package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

// comment is a note anchored to a span of words [Start,End] in a document.
// Quote stores the text that was selected so exports stay meaningful even if
// the document later changes and the indices drift.
type comment struct {
	Start   int       `json:"start"`
	End     int       `json:"end"`
	Quote   string    `json:"quote"`
	Section string    `json:"section,omitempty"` // enclosing heading, for export context
	Body    string    `json:"body"`
	Created time.Time `json:"created"`
	Updated time.Time `json:"updated"`
}

// commentStore persists every document's comments in
// ~/.config/monocle/comments.json (honoring $XDG_CONFIG_HOME), keyed by the
// same docKey state.go uses.
type commentStore struct {
	Docs map[string][]comment `json:"docs"`
}

func commentsPath() (string, error) {
	return configPath("comments.json")
}

// loadComments returns the saved comment store, or an empty one if none
// exists yet.
func loadComments() *commentStore {
	cs := &commentStore{Docs: map[string][]comment{}}
	path, err := commentsPath()
	if err != nil {
		return cs
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return cs
	}
	if json.Unmarshal(data, cs) != nil || cs.Docs == nil {
		cs.Docs = map[string][]comment{}
	}
	return cs
}

func (cs *commentStore) save() error {
	path, err := commentsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cs, "", "  ")
	if err != nil {
		return err
	}
	// Write via a temp file so a crash mid-write can't corrupt the store.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (cs *commentStore) forDoc(key string) []comment {
	return cs.Docs[key]
}

// setForDoc stores (or clears) a document's comments, dropping the key
// entirely when there are none so the file stays tidy.
func (cs *commentStore) setForDoc(key string, comments []comment) {
	if len(comments) == 0 {
		delete(cs.Docs, key)
		return
	}
	cs.Docs[key] = comments
}

// exportComments renders every comment as Markdown an LLM can readily map
// back to the source: each block names its section, quotes the passage, and
// carries the note.
func exportComments(name string, comments []comment) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Comments on %s\n", name)
	for i, c := range comments {
		fmt.Fprintf(&b, "\n## %d.", i+1)
		if c.Section != "" {
			fmt.Fprintf(&b, " %s", c.Section)
		}
		b.WriteByte('\n')
		for _, line := range strings.Split(strings.TrimRight(c.Quote, "\n"), "\n") {
			fmt.Fprintf(&b, "> %s\n", line)
		}
		fmt.Fprintf(&b, "\n%s\n", strings.TrimRight(c.Body, "\n"))
	}
	return b.String()
}

// copyToClipboard writes s to the system clipboard using the platform's
// native utility. It returns an error if no clipboard tool is available.
func copyToClipboard(s string) error {
	cmd, err := clipboardCmd()
	if err != nil {
		return err
	}
	cmd.Stdin = strings.NewReader(s)
	return cmd.Run()
}

// repoRelPath returns path relative to the enclosing git repository's root,
// or the cleaned absolute path when the file isn't inside a repo. The repo is
// found by walking up for a .git entry, so git need not be installed. A remote
// document has no path to shorten, so its URL comes back unchanged.
func repoRelPath(path string) string {
	if isRemoteRef(path) {
		return path
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	for dir := filepath.Dir(abs); ; {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			if rel, err := filepath.Rel(dir, abs); err == nil {
				return rel
			}
			return abs
		}
		parent := filepath.Dir(dir)
		if parent == dir { // reached the filesystem root
			return abs
		}
		dir = parent
	}
}

func clipboardCmd() (*exec.Cmd, error) {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("pbcopy"), nil
	case "windows":
		return exec.Command("clip"), nil
	default:
		for _, c := range [][]string{
			{"wl-copy"},
			{"xclip", "-selection", "clipboard"},
			{"xsel", "--clipboard", "--input"},
		} {
			if path, err := exec.LookPath(c[0]); err == nil {
				return exec.Command(path, c[1:]...), nil
			}
		}
		return nil, fmt.Errorf("no clipboard tool found (install wl-copy, xclip, or xsel)")
	}
}

// commentEditor is a minimal multi-line text editor used to compose a
// comment. It owns the text being typed plus a cursor; the surrounding model
// owns where the comment will be anchored.
type commentEditor struct {
	lines   [][]rune
	row     int // cursor line
	col     int // cursor column, in runes
	start   int // anchored word range, inclusive
	end     int
	quote   string
	section string
	editIdx int // index into model.comments when editing, else -1
}

func newCommentEditor(start, end int, quote, section, body string, editIdx int) commentEditor {
	var lines [][]rune
	for _, l := range strings.Split(body, "\n") {
		lines = append(lines, []rune(l))
	}
	if len(lines) == 0 {
		lines = [][]rune{{}}
	}
	return commentEditor{
		lines:   lines,
		row:     len(lines) - 1,
		col:     len(lines[len(lines)-1]),
		start:   start,
		end:     end,
		quote:   quote,
		section: section,
		editIdx: editIdx,
	}
}

// text joins the editor lines back into a single string body.
func (e *commentEditor) text() string {
	parts := make([]string, len(e.lines))
	for i, l := range e.lines {
		parts[i] = string(l)
	}
	return strings.Join(parts, "\n")
}

func (e *commentEditor) insert(r rune) {
	l := e.lines[e.row]
	l = append(l, 0)
	copy(l[e.col+1:], l[e.col:])
	l[e.col] = r
	e.lines[e.row] = l
	e.col++
}

func (e *commentEditor) newline() {
	l := e.lines[e.row]
	rest := append([]rune{}, l[e.col:]...)
	e.lines[e.row] = l[:e.col]
	e.lines = append(e.lines, nil)
	copy(e.lines[e.row+2:], e.lines[e.row+1:])
	e.lines[e.row+1] = rest
	e.row++
	e.col = 0
}

func (e *commentEditor) backspace() {
	if e.col > 0 {
		l := e.lines[e.row]
		e.lines[e.row] = append(l[:e.col-1], l[e.col:]...)
		e.col--
		return
	}
	if e.row == 0 {
		return
	}
	// Merge this line onto the end of the previous one.
	prev := e.lines[e.row-1]
	e.col = len(prev)
	e.lines[e.row-1] = append(prev, e.lines[e.row]...)
	e.lines = append(e.lines[:e.row], e.lines[e.row+1:]...)
	e.row--
}

func (e *commentEditor) moveLeft() {
	switch {
	case e.col > 0:
		e.col--
	case e.row > 0:
		e.row--
		e.col = len(e.lines[e.row])
	}
}

func (e *commentEditor) moveRight() {
	switch {
	case e.col < len(e.lines[e.row]):
		e.col++
	case e.row < len(e.lines)-1:
		e.row++
		e.col = 0
	}
}

func (e *commentEditor) moveUp() {
	if e.row > 0 {
		e.row--
		e.col = min(e.col, len(e.lines[e.row]))
	}
}

func (e *commentEditor) moveDown() {
	if e.row < len(e.lines)-1 {
		e.row++
		e.col = min(e.col, len(e.lines[e.row]))
	}
}

// sortComments orders comments by their starting word so the gutter, status
// bar, and export all read top-to-bottom.
func sortComments(comments []comment) {
	sort.SliceStable(comments, func(i, j int) bool {
		if comments[i].Start != comments[j].Start {
			return comments[i].Start < comments[j].Start
		}
		return comments[i].End < comments[j].End
	})
}
