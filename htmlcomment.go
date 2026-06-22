package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// htmlComment is a note anchored to a span of text in an HTML document. The
// anchor is a text quote — the selected Quote plus a little surrounding
// context (Prefix/Suffix) — so the browser can re-find the passage even when
// the same words appear more than once. The terminal reader anchors comments
// to word indices instead (see comment.go); HTML has no such index, so the
// quote itself is the anchor.
type htmlComment struct {
	ID      string    `json:"id"`
	Quote   string    `json:"quote"`
	Prefix  string    `json:"prefix,omitempty"`
	Suffix  string    `json:"suffix,omitempty"`
	Body    string    `json:"body"`
	Created time.Time `json:"created"`
	Updated time.Time `json:"updated"`
}

// htmlCommentStore persists every HTML document's comments in
// ~/.config/monocle/html-comments.json (honoring $XDG_CONFIG_HOME), keyed by
// the same docKey state.go uses. It is kept separate from comments.json
// because the two anchor schemes don't interchange.
type htmlCommentStore struct {
	Docs map[string][]htmlComment `json:"docs"`
}

func htmlCommentsPath() (string, error) {
	return configPath("html-comments.json")
}

// loadHTMLComments returns the saved store, or an empty one if none exists yet.
func loadHTMLComments() *htmlCommentStore {
	cs := &htmlCommentStore{Docs: map[string][]htmlComment{}}
	path, err := htmlCommentsPath()
	if err != nil {
		return cs
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return cs
	}
	if json.Unmarshal(data, cs) != nil || cs.Docs == nil {
		cs.Docs = map[string][]htmlComment{}
	}
	return cs
}

func (cs *htmlCommentStore) save() error {
	path, err := htmlCommentsPath()
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

func (cs *htmlCommentStore) forDoc(key string) []htmlComment {
	return cs.Docs[key]
}

// setForDoc stores (or clears) a document's comments, dropping the key
// entirely when there are none so the file stays tidy.
func (cs *htmlCommentStore) setForDoc(key string, comments []htmlComment) {
	if len(comments) == 0 {
		delete(cs.Docs, key)
		return
	}
	cs.Docs[key] = comments
}

// exportHTMLComments renders every comment as Markdown an LLM can map back to
// the source: each block quotes the passage and carries the note. It mirrors
// exportComments so both readers produce the same shape.
func exportHTMLComments(name string, comments []htmlComment) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Comments on %s\n", name)
	for i, c := range comments {
		fmt.Fprintf(&b, "\n## %d.\n", i+1)
		for _, line := range strings.Split(strings.TrimRight(c.Quote, "\n"), "\n") {
			fmt.Fprintf(&b, "> %s\n", line)
		}
		fmt.Fprintf(&b, "\n%s\n", strings.TrimRight(c.Body, "\n"))
	}
	return b.String()
}
