package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestCommentStoreRoundTrip(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	cs := loadComments()
	if len(cs.Docs) != 0 {
		t.Fatalf("fresh store not empty: %+v", cs)
	}

	cs.setForDoc("/doc.md", []comment{{Start: 1, End: 2, Quote: "alpha beta", Body: "a note"}})
	if err := cs.save(); err != nil {
		t.Fatal(err)
	}

	got := loadComments().forDoc("/doc.md")
	if len(got) != 1 || got[0].Body != "a note" || got[0].Start != 1 || got[0].End != 2 {
		t.Fatalf("round-trip mismatch: %+v", got)
	}

	// Clearing a document's comments drops the key entirely.
	cs.setForDoc("/doc.md", nil)
	if _, ok := cs.Docs["/doc.md"]; ok {
		t.Error("setForDoc(nil) should delete the key")
	}
}

func TestExportComments(t *testing.T) {
	comments := []comment{
		{Quote: "alpha beta", Section: "Intro", Body: "first note"},
		{Quote: "line one\nline two", Body: "second note"},
	}
	out := exportComments("doc.md", comments)

	for _, want := range []string{
		"# Comments on doc.md",
		"## 1. Intro",
		"> alpha beta",
		"first note",
		"## 2.",
		"> line one",
		"> line two",
		"second note",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("export missing %q:\n%s", want, out)
		}
	}
}

func TestCommentEditor(t *testing.T) {
	e := newCommentEditor(0, 1, "quote", "Sec", "", -1)
	for _, r := range "hi" {
		e.insert(r)
	}
	e.newline()
	for _, r := range "yo" {
		e.insert(r)
	}
	if got := e.text(); got != "hi\nyo" {
		t.Fatalf("text = %q, want %q", got, "hi\nyo")
	}

	// Three backspaces clear "yo" then merge the empty line onto "hi".
	e.backspace()
	e.backspace()
	e.backspace()
	if got := e.text(); got != "hi" {
		t.Fatalf("after backspaces text = %q, want %q", got, "hi")
	}
	if e.row != 0 || e.col != 2 {
		t.Errorf("cursor = (%d,%d), want (0,2)", e.row, e.col)
	}
}

func TestCommentEditorSeededBody(t *testing.T) {
	e := newCommentEditor(0, 1, "q", "", "one\ntwo", 3)
	if got := e.text(); got != "one\ntwo" {
		t.Fatalf("seeded text = %q", got)
	}
	if e.editIdx != 3 {
		t.Errorf("editIdx = %d, want 3", e.editIdx)
	}
	// Cursor lands at the end of the last line.
	if e.row != 1 || e.col != 3 {
		t.Errorf("cursor = (%d,%d), want (1,3)", e.row, e.col)
	}
}

// driveComment runs the comment lifecycle through Update the way a user would.
func TestCommentLifecycleThroughUpdate(t *testing.T) {
	doc := writeDoc(t, "c.md", "# Title\n\nalpha beta gamma delta\n")
	m := newModel(doc)
	m.path = "c.md"
	m.width, m.height = 80, 24
	m.idx = 1 // "alpha"

	press := func(msg tea.KeyMsg) {
		next, _ := m.Update(msg)
		m = next.(model)
	}

	// Shift-select "alpha beta", then comment on it.
	press(keyMsg("L"))
	if lo, hi, ok := m.selRange(); !ok || lo != 1 || hi != 2 {
		t.Fatalf("selRange = (%d,%d,%v), want (1,2,true)", lo, hi, ok)
	}
	press(keyMsg("c"))
	if !m.editing {
		t.Fatal("c should open the editor")
	}
	if m.selecting {
		t.Error("opening the editor should clear the selection")
	}
	press(keyMsg("note"))
	press(tea.KeyMsg{Type: tea.KeyCtrlS})

	if m.editing {
		t.Fatal("ctrl+s should close the editor")
	}
	if len(m.comments) != 1 {
		t.Fatalf("want 1 comment, got %d", len(m.comments))
	}
	c := m.comments[0]
	if c.Start != 1 || c.End != 2 || c.Body != "note" || c.Quote != "alpha beta" || c.Section != "Title" {
		t.Fatalf("unexpected comment: %+v", c)
	}
	if m.commentAt(1) != 0 || m.commentAt(3) != -1 {
		t.Errorf("commentAt: covered=%d uncovered=%d", m.commentAt(1), m.commentAt(3))
	}

	// Pressing c on a commented word (no selection) edits it; clearing the
	// body and saving deletes the comment.
	press(keyMsg("c"))
	if !m.editing || m.editor.editIdx != 0 {
		t.Fatalf("c on a comment should edit it (editing=%v editIdx=%d)", m.editing, m.editor.editIdx)
	}
	for range "note" {
		press(tea.KeyMsg{Type: tea.KeyBackspace})
	}
	press(tea.KeyMsg{Type: tea.KeyCtrlS})
	if len(m.comments) != 0 {
		t.Fatalf("clearing the body should delete the comment, have %d", len(m.comments))
	}
}

func TestCommentCurrentLineFallback(t *testing.T) {
	doc := writeDoc(t, "c.md", "# Title\n\nalpha beta gamma delta\n")
	m := newModel(doc)
	m.width, m.height = 80, 24
	m.idx = 2 // "beta", no selection, no existing comment

	m.openComment()
	if m.editor.start != 1 || m.editor.end != 4 {
		t.Errorf("current-line target = (%d,%d), want (1,4)", m.editor.start, m.editor.end)
	}
}

func TestExportToClipboardNoComments(t *testing.T) {
	doc := writeDoc(t, "c.md", "alpha beta")
	m := newModel(doc)
	if got := m.exportToClipboard(); got != "No comments to export" {
		t.Errorf("empty export = %q", got)
	}
}

func TestSaveCommentTimestamps(t *testing.T) {
	doc := writeDoc(t, "c.md", "alpha beta gamma")
	m := newModel(doc)
	m.editor = newCommentEditor(0, 1, "alpha beta", "", "hi", -1)
	now := time.Unix(1000, 0)
	m.saveComment(now)
	if len(m.comments) != 1 || !m.comments[0].Created.Equal(now) {
		t.Fatalf("saveComment did not stamp Created: %+v", m.comments)
	}
}

func TestCommentPanelWidth(t *testing.T) {
	cases := []struct {
		w        int
		editing  bool
		comments int
		want     int
	}{
		{120, false, 0, 0},  // nothing to show
		{120, false, 2, 40}, // capped at 40
		{120, true, 0, 40},  // editing reserves room too
		{90, false, 1, 30},  // a third of the width
		{50, true, 1, 0},    // too narrow: fall back to the overlay
	}
	for _, c := range cases {
		m := model{width: c.w, editing: c.editing}
		m.comments = make([]comment, c.comments)
		if got := m.commentPanelW(); got != c.want {
			t.Errorf("commentPanelW(w=%d editing=%v n=%d) = %d, want %d", c.w, c.editing, c.comments, got, c.want)
		}
	}
}

func TestWrapText(t *testing.T) {
	got := wrapText("the quick brown fox", 9)
	want := []string{"the quick", "brown fox"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("wrapText = %q, want %q", got, want)
	}
	// Existing newlines are preserved as hard breaks.
	if got := wrapText("a\nb", 80); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("wrapText newline = %q", got)
	}
}

func TestWrapEditorLineCursor(t *testing.T) {
	m := model{st: defaultStyles()}
	// A line longer than the width wraps, and the cursor sits inside it.
	segs := m.wrapEditorLine([]rune("abcdefgh"), true, 3, 4)
	if len(segs) != 2 {
		t.Fatalf("want 2 segments, got %d: %q", len(segs), segs)
	}
	if ansi.Strip(strings.Join(segs, "")) != "abcdefgh" {
		t.Errorf("wrapped text changed: %q", ansi.Strip(strings.Join(segs, "")))
	}
	// Cursor just past the end of a full-width line wraps to a fresh segment.
	segs = m.wrapEditorLine([]rune("abcd"), true, 4, 4)
	if len(segs) != 2 || ansi.Strip(segs[1]) != " " {
		t.Errorf("trailing cursor should wrap to a new line: %q", segs)
	}
	// An empty cursor line still shows a cursor block.
	if segs := m.wrapEditorLine(nil, true, 0, 4); len(segs) != 1 || ansi.Strip(segs[0]) != " " {
		t.Errorf("empty cursor line = %q", segs)
	}
}

func TestPlaceAt(t *testing.T) {
	if got := placeAt("ab", "X", 5); got != "ab   X" {
		t.Errorf("placeAt = %q, want %q", got, "ab   X")
	}
	// When the row already reaches the column, content appends directly.
	if got := placeAt("abcde", "X", 3); got != "abcdeX" {
		t.Errorf("placeAt past col = %q, want %q", got, "abcdeX")
	}
}

func TestRepoRelPath(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "docs", "readme.md")
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, want := repoRelPath(file), filepath.Join("docs", "readme.md"); got != want {
		t.Errorf("repoRelPath in repo = %q, want %q", got, want)
	}

	// Outside any repo, the absolute path is returned.
	outside := filepath.Join(t.TempDir(), "plain.txt")
	if err := os.WriteFile(outside, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := repoRelPath(outside); !filepath.IsAbs(got) {
		t.Errorf("repoRelPath outside repo = %q, want an absolute path", got)
	}
}

func TestBackSectionKey(t *testing.T) {
	doc := writeDoc(t, "c.md", "# One\n\nalpha beta\n\n# Two\n\ngamma delta\n")
	m := newModel(doc)
	m.width, m.height = 80, 24
	m.idx = 5 // "delta", in the second section

	press := func(k string) {
		next, _ := m.Update(keyMsg(k))
		m = next.(model)
	}
	// b rewinds to the section start, then to the previous section.
	press("b")
	if m.idx != 3 {
		t.Fatalf("first b -> %d, want 3 (start of section Two)", m.idx)
	}
	press("b")
	if m.idx != 0 {
		t.Fatalf("second b -> %d, want 0 (section One)", m.idx)
	}
}

func TestCommentPanelInView(t *testing.T) {
	doc := writeDoc(t, "v.md", "alpha beta gamma delta epsilon zeta eta theta")
	m := newModel(doc)
	m.width, m.height = 100, 16
	m.idx = 2
	m.comments = []comment{{Start: 1, End: 2, Quote: "alpha beta", Body: "a side note"}}

	out := ansi.Strip(m.View())
	if !strings.Contains(out, "a side note") {
		t.Errorf("rendered view should show the comment in the panel:\n%s", out)
	}
	// The note must sit in the right-hand panel, at or past the text column's
	// right edge — never inside the text itself.
	ctxW, panelW := m.ctxW(), m.commentPanelW()
	textRight := max((m.width-(ctxW+commentGap+panelW))/2, 0) + ctxW
	for _, ln := range strings.Split(out, "\n") {
		if i := strings.Index(ln, "a side note"); i >= 0 && i < textRight {
			t.Errorf("comment rendered inside the text column at %d (text ends at %d):\n%q", i, textRight, ln)
		}
	}
}
