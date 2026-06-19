package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func writeDoc(t *testing.T, name, src string) *document {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	doc, err := loadDocument(path)
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func allText(doc *document) string {
	texts := make([]string, len(doc.words))
	for i, w := range doc.words {
		texts[i] = w.text
	}
	return strings.Join(texts, " ")
}

// findWord returns the first word with the given text, or nil.
func findWord(doc *document, text string) *word {
	for i := range doc.words {
		if doc.words[i].text == text {
			return &doc.words[i]
		}
	}
	return nil
}

func TestMarkdownStructure(t *testing.T) {
	doc := writeDoc(t, "sample.md", `# Title

Intro paragraph with **bold** and a [link](http://x).

`+"```go\nfunc body() {}\n```"+`

| kept | table |
|------|-------|
| alpha | beta |

## Second Section

Closing words here.
`)

	if len(doc.sections) != 2 {
		t.Fatalf("sections = %d, want 2", len(doc.sections))
	}
	if doc.sections[1].title != "Second Section" {
		t.Errorf("section title = %q", doc.sections[1].title)
	}
	text := allText(doc)
	for _, gone := range []string{"```", "**", "#", "http://x", "|", "---"} {
		if strings.Contains(text, gone) {
			t.Errorf("%q should have been stripped, text: %q", gone, text)
		}
	}
	for _, kept := range []string{"bold", "link.", "body()", "kept", "alpha", "Closing"} {
		if findWord(doc, kept) == nil {
			t.Errorf("word %q missing from text: %q", kept, text)
		}
	}
	wantKinds := []paraKind{paraHeading, paraText, paraCode, paraTable, paraHeading, paraText}
	if len(doc.paras) != len(wantKinds) {
		t.Fatalf("paras = %d, want %d", len(doc.paras), len(wantKinds))
	}
	for i, want := range wantKinds {
		if doc.paras[i].kind != want {
			t.Errorf("para %d kind = %d, want %d", i, doc.paras[i].kind, want)
		}
	}
	last := doc.words[len(doc.words)-1]
	if last.section != 1 {
		t.Errorf("last word section = %d, want 1", last.section)
	}
}

func TestInlineStyles(t *testing.T) {
	doc := writeDoc(t, "i.md", "plain **bold** *ital* `code` [label](http://e) ~~struck~~ snake_case")
	for _, tc := range []struct {
		text string
		want wordStyle
	}{
		{"plain", wordStyle{}},
		{"bold", wordStyle{bold: true}},
		{"ital", wordStyle{italic: true}},
		{"code", wordStyle{code: true}},
		{"label", wordStyle{link: true}},
		{"struck", wordStyle{strike: true}},
		{"snake_case", wordStyle{}},
	} {
		w := findWord(doc, tc.text)
		if w == nil {
			t.Errorf("word %q missing", tc.text)
			continue
		}
		if w.style != tc.want {
			t.Errorf("%q style = %+v, want %+v", tc.text, w.style, tc.want)
		}
	}
}

func TestTableLayout(t *testing.T) {
	doc := writeDoc(t, "t.md", `| Name | Description |
|------|-------------|
| ab | longer text here |
`)
	info := doc.paras[0]
	if info.kind != paraTable {
		t.Fatalf("kind = %d, want table", info.kind)
	}
	if !info.header {
		t.Error("table should have a header row")
	}
	// Column 0 is 4 wide ("Name"), column 1 is 16 ("longer text here"),
	// separated by " │ ": second column starts at 7, separator at 5.
	if len(info.sepCols) != 1 || info.sepCols[0] != 5 {
		t.Errorf("sepCols = %v, want [5]", info.sepCols)
	}
	if info.width != 23 {
		t.Errorf("width = %d, want 23", info.width)
	}
	for _, tc := range []struct {
		text     string
		col, row int
	}{
		{"Name", 0, 0},
		{"Description", 7, 0},
		{"ab", 0, 1},
		{"longer", 7, 1},
		{"text", 14, 1},
		{"here", 19, 1},
	} {
		w := findWord(doc, tc.text)
		if w == nil {
			t.Fatalf("word %q missing", tc.text)
		}
		if w.col != tc.col || w.row != tc.row {
			t.Errorf("%q at col %d row %d, want col %d row %d", tc.text, w.col, w.row, tc.col, tc.row)
		}
	}

	// One visual line per table row, never wrapped, even at tiny widths.
	lines := layoutLines(doc, 10)
	if len(lines) != 2 {
		t.Fatalf("lines = %+v, want 2 rows", lines)
	}

	// Vertical movement lands on the cell below, by display column.
	m := newModel(doc)
	m.width, m.height = 80, 24
	m.idx = 1 // "Description", col 7
	m.moveLine(1)
	if doc.words[m.idx].text != "longer" {
		t.Errorf("down from Description landed on %q, want longer", doc.words[m.idx].text)
	}
}

func TestTableWideRunes(t *testing.T) {
	doc := writeDoc(t, "t.md", `| Name | Note |
|------|------|
| ✅ done | ok |
| 漢字 | x |
`)
	info := doc.paras[0]
	// Column widths follow display width, not rune count: ✅ occupies two
	// cells and 漢字 four, so "✅ done" is 7 wide. The separator then sits
	// at 8 and the second column starts at 10.
	if len(info.sepCols) != 1 || info.sepCols[0] != 8 {
		t.Errorf("sepCols = %v, want [8]", info.sepCols)
	}
	if info.width != 14 {
		t.Errorf("width = %d, want 14", info.width)
	}
	for _, tc := range []struct {
		text string
		col  int
	}{
		{"✅", 0},
		{"done", 3},
		{"漢字", 0},
		{"Note", 10},
		{"ok", 10},
		{"x", 10},
	} {
		w := findWord(doc, tc.text)
		if w == nil {
			t.Fatalf("word %q missing", tc.text)
		}
		if w.col != tc.col {
			t.Errorf("%q at col %d, want %d", tc.text, w.col, tc.col)
		}
	}
}

func TestTableWrapsToWidth(t *testing.T) {
	doc := writeDoc(t, "t.md", `| # | Finding | Severity |
|---|---------|----------|
| 1 | the first finding has a great many words that cannot possibly fit on one screen row | Critical |
| 2 | short | High |
`)
	const w = 40
	g := layoutTable(doc, 0, w)
	if g.width > w {
		t.Errorf("table width = %d, want <= %d", g.width, w)
	}
	if g.heights[0] != 1 || g.heights[1] < 2 || g.heights[2] != 1 {
		t.Errorf("heights = %v, want [1, >=2, 1]", g.heights)
	}
	// No word may cross a column separator or the table edge.
	for i, word := range doc.words {
		lo, hi := g.col[i], g.col[i]+dispWidth(word.text)
		for _, sc := range g.sepCols {
			if sc >= lo && sc < hi {
				t.Errorf("%q spans [%d,%d) across separator at %d", word.text, lo, hi, sc)
			}
		}
		if hi > g.width {
			t.Errorf("%q ends at %d, past table width %d", word.text, hi, g.width)
		}
	}

	// Vertical movement crosses wrapped rows one table row at a time.
	m := newModel(doc)
	m.width, m.height = 60, 24
	for i := range doc.words {
		if doc.words[i].text == "Finding" {
			m.idx = i
		}
	}
	m.moveLine(1)
	if got := doc.words[m.idx].text; got != "the" {
		t.Errorf("down from Finding landed on %q, want the", got)
	}
	m.moveLine(1)
	if got := doc.words[m.idx].text; got != "short" {
		t.Errorf("down again landed on %q, want short", got)
	}

	// Every rendered screen row draws the grid at the same display columns.
	m.style = styleConfig{}
	ctxW := m.ctxW()
	gr := layoutTable(doc, 0, ctxW)
	for _, l := range layoutLines(doc, ctxW) {
		for _, seg := range m.renderTableRows(l, 0, false, ctxW) {
			var got []int
			col := 0
			for _, r := range ansi.Strip(seg) {
				if r == '│' {
					got = append(got, col)
				}
				col += dispWidth(string(r))
			}
			if !slices.Equal(got, gr.sepCols) {
				t.Errorf("separators at %v, want %v in %q", got, gr.sepCols, ansi.Strip(seg))
			}
		}
	}
}

func TestCodeWideRunes(t *testing.T) {
	doc := writeDoc(t, "c.md", "```\n漢字 x\n```\n")
	// 漢字 displays 4 wide, so "x" starts at column 5 and the row is 6 wide.
	if w := findWord(doc, "x"); w == nil || w.col != 5 {
		t.Fatalf("x at %+v, want col 5", w)
	}
	if got := doc.paras[0].width; got != 6 {
		t.Errorf("width = %d, want 6", got)
	}
}

func TestCodeBlockPreservesLayout(t *testing.T) {
	doc := writeDoc(t, "c.md", "```go\nfunc main() {\n\tx := 1\n}\n\nvar y int\n```\n")
	if len(doc.paras) != 2 {
		t.Fatalf("paras = %d, want 2 (blank line splits the block)", len(doc.paras))
	}
	for i, info := range doc.paras {
		if info.kind != paraCode {
			t.Errorf("para %d kind = %d, want code", i, info.kind)
		}
	}
	x := findWord(doc, "x")
	if x == nil || x.col != 4 || x.row != 1 {
		t.Errorf("x = %+v, want col 4 row 1 (tab expanded)", x)
	}
	// Code lines never wrap.
	lines := layoutLines(doc, 5)
	var codeLines int
	for _, l := range lines {
		if l.para == 0 {
			codeLines++
		}
	}
	if codeLines != 3 {
		t.Errorf("first block lines = %d, want 3", codeLines)
	}
}

func TestListsAndQuotes(t *testing.T) {
	doc := writeDoc(t, "l.md", `- first item
- second item
3. numbered

> quoted text
> more quote
`)
	wantKinds := []paraKind{paraList, paraList, paraList, paraQuote}
	if len(doc.paras) != len(wantKinds) {
		t.Fatalf("paras = %d, want %d", len(doc.paras), len(wantKinds))
	}
	for i, want := range wantKinds {
		if doc.paras[i].kind != want {
			t.Errorf("para %d kind = %d, want %d", i, doc.paras[i].kind, want)
		}
	}
	if doc.paras[0].marker != "•" {
		t.Errorf("bullet marker = %q, want •", doc.paras[0].marker)
	}
	if doc.paras[2].marker != "3." {
		t.Errorf("numbered marker = %q, want 3.", doc.paras[2].marker)
	}
	// Wrapped lines account for the marker indent in column math.
	lines := layoutLines(doc, 72)
	if got := columnOf(doc, lines[0], 0, 72); got != 2 {
		t.Errorf("first list word column = %d, want 2", got)
	}
}

func TestLayoutLines(t *testing.T) {
	doc := writeDoc(t, "p.txt", "alpha beta gamma delta\n\nepsilon zeta")
	lines := layoutLines(doc, 11) // fits "alpha beta", then "gamma delta", then "epsilon"...
	want := []line{
		{0, 2, 0}, // alpha beta
		{2, 4, 0}, // gamma delta
		{4, 5, 1}, // epsilon
		{5, 6, 1}, // zeta
	}
	if len(lines) != len(want) {
		t.Fatalf("lines = %+v, want %+v", lines, want)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Errorf("line %d = %+v, want %+v", i, lines[i], want[i])
		}
	}
	for i := range doc.words {
		li := lineIndex(lines, i)
		if i < lines[li].from || i >= lines[li].to {
			t.Errorf("lineIndex(%d) = %d, range [%d,%d)", i, li, lines[li].from, lines[li].to)
		}
	}
}

func TestMoveLineKeepsColumn(t *testing.T) {
	// Two lines of three words each at width 17:
	//   "one two three"     (cols 0, 4, 8)
	//   "four five six"     (cols 0, 5, 10)
	doc := writeDoc(t, "m.txt", "one two three four five six")
	m := newModel(doc)
	m.width, m.height = 25, 10 // ctxW = 17

	m.idx = 1 // "two", column 4
	m.moveLine(1)
	if doc.words[m.idx].text != "four" {
		t.Errorf("down from %q landed on %q, want %q", "two", doc.words[m.idx].text, "four")
	}

	m.idx = 2 // "three", column 8
	m.moveLine(1)
	if doc.words[m.idx].text != "five" {
		t.Errorf("down from %q landed on %q, want %q", "three", doc.words[m.idx].text, "five")
	}
	m.moveLine(-1)
	if doc.words[m.idx].text != "two" {
		t.Errorf("up from %q landed on %q, want %q", "five", doc.words[m.idx].text, "two")
	}

	// Clamped at the edges.
	m.idx = 0
	m.moveLine(-1)
	if m.idx != 0 {
		t.Errorf("up from first line moved to %d", m.idx)
	}
	m.idx = len(doc.words) - 1
	m.moveLine(1)
	if m.idx != len(doc.words)-1 {
		t.Errorf("down from last line moved to %d", m.idx)
	}
}

func TestMoveLineCrossesParagraphs(t *testing.T) {
	doc := writeDoc(t, "p.txt", "first paragraph here\n\nsecond paragraph there")
	m := newModel(doc)
	m.width, m.height = 80, 24 // both paragraphs fit on one line each

	m.idx = 0
	m.moveLine(1)
	if got := doc.words[m.idx].para; got != 1 {
		t.Errorf("down should cross into paragraph 1, in %d (word %q)", got, doc.words[m.idx].text)
	}
	m.moveLine(-1)
	if got := doc.words[m.idx].para; got != 0 {
		t.Errorf("up should cross back into paragraph 0, in %d", got)
	}
}

func TestViewRendersAtAnySizeAndPosition(t *testing.T) {
	doc := writeDoc(t, "v.md", `# Heading One

One small paragraph. And a considerably-longer second sentence right here.

| a | wide table column here |
|---|------------------------|
| 1 | value                  |

`+"```\nfunc main() {\n\tprintln(\"hi\")\n}\n```"+`

- a list item with **bold** and a [link](http://x)

> a quoted line

## Heading Two

The quick brown fox jumps over the lazy dog near the river bank today.
`)

	styles := []styleConfig{
		defaultStyleConfig(),
		{}, // everything off
		{WordHighlight: true},
		{LineHighlight: true},
		{DimOthers: true},
	}
	for _, size := range [][2]int{{80, 24}, {20, 8}, {5, 3}, {120, 40}} {
		for _, st := range styles {
			for _, picker := range []bool{false, true} {
				m := newModel(doc)
				m.width, m.height = size[0], size[1]
				m.style, m.picker = st, picker
				// A selection spanning a few words, a comment with a marker,
				// and the editor overlay all need to render at every size.
				m.selecting, m.anchor = true, 1
				m.comments = []comment{{Start: 2, End: 5, Quote: "q", Body: "a\nnote"}}
				for i := range doc.words {
					m.idx = i
					_ = m.View() // must not panic at any combination
				}
				m.editing = true
				m.editor = newCommentEditor(2, 5, "a long quoted passage", "Heading One", "line\ntwo", -1)
				_ = m.View()
			}
		}
	}
}

func TestWidthAdjust(t *testing.T) {
	doc := writeDoc(t, "w.txt", "some words to read here and some more after them")
	m := newModel(doc)
	m.width, m.height = 100, 24

	if got := m.ctxW(); got != 72 {
		t.Fatalf("auto ctxW = %d, want 72", got)
	}

	press := func(key string) {
		next, _ := m.Update(keyMsg(key))
		m = next.(model)
	}

	press("+")
	if got := m.ctxW(); got != 72+widthStep {
		t.Errorf("ctxW after + = %d, want %d", got, 72+widthStep)
	}
	press("-")
	press("-")
	if got := m.ctxW(); got != 72-widthStep {
		t.Errorf("ctxW after +-- = %d, want %d", got, 72-widthStep)
	}

	// Clamped to the terminal on the high end...
	for range 20 {
		press("+")
	}
	if got := m.ctxW(); got != m.width-2 {
		t.Errorf("ctxW maxed = %d, want %d", got, m.width-2)
	}
	// ...and to a readable minimum on the low end.
	for range 40 {
		press("-")
	}
	if got := m.ctxW(); got != 10 {
		t.Errorf("ctxW minned = %d, want 10", got)
	}

	// A user-set width survives a resize, re-clamped to the new terminal.
	m.style.Width = 80
	m.width = 60
	if got := m.ctxW(); got != 58 {
		t.Errorf("ctxW on narrow terminal = %d, want 58", got)
	}
}

func keyMsg(key string) tea.KeyMsg {
	switch key {
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case " ":
		return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	}
}

func TestStylePicker(t *testing.T) {
	doc := writeDoc(t, "s.txt", "some words to read here")
	m := newModel(doc)
	m.width, m.height = 80, 24

	press := func(key string) {
		next, _ := m.Update(keyMsg(key))
		m = next.(model)
	}

	press("s")
	if !m.picker {
		t.Fatal("s should open the picker")
	}
	press("l") // movement keys must not move the document while picker is open
	if m.idx != 0 {
		t.Error("document moved while picker open")
	}
	press(" ")
	if m.style.WordHighlight {
		t.Error("space should toggle word highlight off")
	}
	press("j")
	press(" ")
	if m.style.LineHighlight {
		t.Error("second item should toggle line highlight off")
	}
	press("esc")
	if m.picker {
		t.Error("esc should close the picker")
	}
	press("l")
	if m.idx != 1 {
		t.Error("movement should work again after closing the picker")
	}
}
