package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// styleConfig holds the user-toggleable display options, persisted in state.
type styleConfig struct {
	WordHighlight bool `json:"word_highlight"`
	LineHighlight bool `json:"line_highlight"`
	DimOthers     bool `json:"dim_others"`
	Width         int  `json:"width,omitempty"` // text column width; 0 = auto
}

func defaultStyleConfig() styleConfig {
	return styleConfig{WordHighlight: true, LineHighlight: true, DimOthers: true}
}

const pickerItems = 3

type model struct {
	doc       *document
	path      string // source path, for export labels
	idx       int    // focal word
	style     styleConfig
	st        styles // active theme's styles
	picker    bool   // style picker overlay open
	pickerSel int
	width     int
	height    int

	comments    []comment // for the current document, sorted by Start
	selecting   bool      // shift-select in progress
	anchor      int       // selection anchor word
	editing     bool      // comment editor overlay open
	editor      commentEditor
	flash       string // transient status message, cleared on next key
	themeIdx    int    // index into themes
	themePicker bool   // theme picker overlay open
}

func newModel(doc *document) model {
	return model{doc: doc, style: defaultStyleConfig(), st: defaultStyles()}
}

// setTheme switches the active theme (and its derived styles) by index,
// clamped to the available themes.
func (m *model) setTheme(i int) {
	m.themeIdx = min(max(i, 0), len(themes)-1)
	m.st = newStyles(themes[m.themeIdx])
}

// selRange returns the inclusive word span of the active selection.
func (m model) selRange() (lo, hi int, ok bool) {
	if !m.selecting {
		return 0, 0, false
	}
	return min(m.anchor, m.idx), max(m.anchor, m.idx), true
}

// commentAt returns the index of a comment covering word i, or -1.
func (m model) commentAt(i int) int {
	for k, c := range m.comments {
		if i >= c.Start && i <= c.End {
			return k
		}
	}
	return -1
}

// lineCommented reports whether any comment overlaps the visual line l.
func (m model) lineCommented(l line) bool {
	for _, c := range m.comments {
		if c.Start < l.to && c.End >= l.from {
			return true
		}
	}
	return false
}

// quoteRange returns the plain text of words [start,end], for the comment's
// stored quote.
func (m model) quoteRange(start, end int) string {
	var parts []string
	for i := start; i <= end && i < len(m.doc.words); i++ {
		parts = append(parts, m.doc.words[i].text)
	}
	return strings.Join(parts, " ")
}

// startSelection begins a selection at the focal word if one isn't already
// in progress.
func (m *model) startSelection() {
	if !m.selecting {
		m.selecting = true
		m.anchor = m.idx
	}
}

// openComment opens the editor for the active selection, an existing comment
// under the cursor, or — failing both — the current visual line.
func (m *model) openComment() {
	if lo, hi, ok := m.selRange(); ok {
		m.editor = newCommentEditor(lo, hi, m.quoteRange(lo, hi), m.sectionTitle(lo), "", -1)
	} else if ci := m.commentAt(m.idx); ci >= 0 {
		c := m.comments[ci]
		m.editor = newCommentEditor(c.Start, c.End, c.Quote, c.Section, c.Body, ci)
	} else {
		lines := layoutLines(m.doc, m.ctxW())
		l := lines[lineIndex(lines, m.idx)]
		m.editor = newCommentEditor(l.from, l.to-1, m.quoteRange(l.from, l.to-1), m.sectionTitle(l.from), "", -1)
	}
	m.editing = true
	m.selecting = false
}

// sectionTitle returns the heading enclosing word i, or "".
func (m model) sectionTitle(i int) string {
	if i < 0 || i >= len(m.doc.words) {
		return ""
	}
	if sec := m.doc.words[i].section; sec >= 0 {
		return m.doc.sections[sec].title
	}
	return ""
}

// saveComment commits the editor's contents: an empty body deletes the
// comment being edited (or creates nothing), otherwise it updates or appends.
func (m *model) saveComment(now time.Time) {
	body := strings.TrimRight(m.editor.text(), "\n")
	e := m.editor
	switch {
	case strings.TrimSpace(body) == "":
		if e.editIdx >= 0 {
			m.comments = append(m.comments[:e.editIdx], m.comments[e.editIdx+1:]...)
		}
	case e.editIdx >= 0:
		m.comments[e.editIdx].Body = body
		m.comments[e.editIdx].Updated = now
	default:
		m.comments = append(m.comments, comment{
			Start: e.start, End: e.end, Quote: e.quote, Section: e.section,
			Body: body, Created: now, Updated: now,
		})
		sortComments(m.comments)
	}
}

// exportToClipboard copies every comment to the clipboard, returning a status
// message describing the outcome.
func (m model) exportToClipboard() string {
	if len(m.comments) == 0 {
		return "No comments to export"
	}
	name := filepath.Base(m.path)
	if err := copyToClipboard(exportComments(name, m.comments)); err != nil {
		return "Clipboard error: " + err.Error()
	}
	n := len(m.comments)
	if n == 1 {
		return "Copied 1 comment to clipboard"
	}
	return fmt.Sprintf("Copied %d comments to clipboard", n)
}

// copyPath copies the document's path (relative to the git repo root when
// there is one) to the clipboard, returning a status message.
func (m model) copyPath() string {
	p := repoRelPath(m.path)
	if err := copyToClipboard(p); err != nil {
		return "Clipboard error: " + err.Error()
	}
	return "Copied path: " + p
}

func (m model) Init() tea.Cmd {
	return nil
}

// ctxW is the text column width; layout and rendering must agree on it.
// A user-set width (the +/- keys) wins; otherwise it tracks the terminal.
// When the comment panel is open it claims room on the right, so the text
// column wraps within what's left.
func (m model) ctxW() int {
	if m.width <= 0 {
		if m.style.Width > 0 {
			return m.style.Width
		}
		return 72
	}
	panel := m.commentPanelW()
	avail := m.width - panel
	if panel > 0 {
		avail -= commentGap // leave breathing room between text and panel
	}
	if m.style.Width > 0 {
		return max(min(m.style.Width, avail-2), 10)
	}
	return max(min(avail-8, 72), 10)
}

// commentGap is the blank columns between the text column and the comment
// panel, so the notes sit close to their context without crowding it.
const commentGap = 4

// commentPanelW is the width of the right-hand comment margin, or 0 when it
// isn't shown — there are no comments and nothing is being edited, or the
// terminal is too narrow to spare the room (the editor falls back to a
// centered overlay there).
func (m model) commentPanelW() int {
	if !m.editing && len(m.comments) == 0 {
		return 0
	}
	w := min(max(m.width/3, 24), 40)
	if m.width-w < 30 {
		return 0
	}
	return w
}

const widthStep = 4

// adjustWidth steps the text column width from its current effective value,
// so the first press in auto mode nudges from what's on screen.
func (m *model) adjustWidth(delta int) {
	w := m.ctxW() + delta
	if m.width > 0 {
		w = min(w, m.width-2)
	}
	m.style.Width = max(w, 10)
}

func (m *model) seek(idx int) {
	m.idx = min(max(idx, 0), len(m.doc.words)-1)
}

// moveLine moves the focus to the adjacent visual line, landing on the
// word nearest the current column, like a text editor cursor.
func (m *model) moveLine(delta int) {
	w := m.ctxW()
	lines := layoutLines(m.doc, w)
	li := lineIndex(lines, m.idx)
	ti := min(max(li+delta, 0), len(lines)-1)
	if ti == li {
		return
	}
	col := columnOf(m.doc, lines[li], m.idx, w)
	m.idx = wordAtColumn(m.doc, lines[ti], col, w)
}

func (m *model) togglePickerItem() {
	switch m.pickerSel {
	case 0:
		m.style.WordHighlight = !m.style.WordHighlight
	case 1:
		m.style.LineHighlight = !m.style.LineHighlight
	case 2:
		m.style.DimOthers = !m.style.DimOthers
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case tea.KeyMsg:
		m.flash = ""
		if m.editing {
			return m.updateEditor(msg)
		}
		if m.picker {
			switch msg.String() {
			case "ctrl+c":
				return m, tea.Quit
			case "q", "esc", "s":
				m.picker = false
			case "up", "k":
				m.pickerSel = max(m.pickerSel-1, 0)
			case "down", "j":
				m.pickerSel = min(m.pickerSel+1, pickerItems-1)
			case " ", "enter":
				m.togglePickerItem()
			}
			return m, nil
		}
		if m.themePicker {
			// Moving the cursor previews the theme live; it sticks on close.
			switch msg.String() {
			case "ctrl+c":
				return m, tea.Quit
			case "q", "esc", "t", "enter", " ":
				m.themePicker = false
			case "up", "k":
				m.setTheme(m.themeIdx - 1)
			case "down", "j":
				m.setTheme(m.themeIdx + 1)
			}
			return m, nil
		}

		// Shift+motion (or capital H/J/K/L) extends a selection from the
		// anchor; everything else falls through and collapses it.
		switch msg.String() {
		case "shift+right", "L":
			m.startSelection()
			m.seek(m.idx + 1)
			return m, nil
		case "shift+left", "H":
			m.startSelection()
			m.seek(m.idx - 1)
			return m, nil
		case "shift+down", "J":
			m.startSelection()
			m.moveLine(1)
			return m, nil
		case "shift+up", "K":
			m.startSelection()
			m.moveLine(-1)
			return m, nil
		}

		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit

		case "esc":
			if m.selecting {
				m.selecting = false
				return m, nil
			}
			return m, tea.Quit

		case "c":
			m.openComment()
			return m, nil

		case "x":
			m.flash = m.exportToClipboard()
			return m, nil

		case "p":
			m.flash = m.copyPath()
			return m, nil

		case "s":
			m.picker = true
			return m, nil

		case "t":
			m.themePicker = true
			return m, nil

		case "+", "=":
			m.adjustWidth(widthStep)
			return m, nil

		case "-", "_":
			m.adjustWidth(-widthStep)
			return m, nil
		}

		// Plain motion: collapse any selection, then move.
		m.selecting = false
		switch msg.String() {
		case "right", "l":
			m.seek(m.idx + 1)

		case "left", "h":
			m.seek(m.idx - 1)

		case "down", "j":
			m.moveLine(1)

		case "up", "k":
			m.moveLine(-1)

		case "}":
			p := m.doc.words[m.idx].para
			if p+1 < len(m.doc.paraStarts) {
				m.seek(m.doc.paraStarts[p+1])
			} else {
				m.seek(len(m.doc.words) - 1)
			}

		case "{":
			p := m.doc.words[m.idx].para
			// First press rewinds to the paragraph start; pressing again
			// from the start jumps to the previous paragraph.
			if start := m.doc.paraStarts[p]; m.idx > start {
				m.seek(start)
			} else if p > 0 {
				m.seek(m.doc.paraStarts[p-1])
			}

		case "n":
			if sec := m.doc.words[m.idx].section; sec+1 < len(m.doc.sections) {
				m.seek(m.doc.sections[sec+1].start)
			}

		case "b":
			sec := m.doc.words[m.idx].section
			if sec < 0 {
				break
			}
			// Like {: section start first, then the previous section.
			if start := m.doc.sections[sec].start; m.idx > start {
				m.seek(start)
			} else if sec > 0 {
				m.seek(m.doc.sections[sec-1].start)
			}

		case "g":
			m.seek(0)

		case "G":
			m.seek(len(m.doc.words) - 1)
		}
	}
	return m, nil
}

// updateEditor handles a key while the comment editor overlay is open.
// ctrl+s saves, esc discards, and enter inserts a newline.
func (m model) updateEditor(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeyEsc:
		m.editing = false
	case tea.KeyCtrlS:
		m.saveComment(time.Now())
		m.editing = false
	case tea.KeyEnter:
		m.editor.newline()
	case tea.KeyBackspace:
		m.editor.backspace()
	case tea.KeyLeft:
		m.editor.moveLeft()
	case tea.KeyRight:
		m.editor.moveRight()
	case tea.KeyUp:
		m.editor.moveUp()
	case tea.KeyDown:
		m.editor.moveDown()
	case tea.KeyHome:
		m.editor.col = 0
	case tea.KeyEnd:
		m.editor.col = len(m.editor.lines[m.editor.row])
	case tea.KeySpace:
		m.editor.insert(' ')
	case tea.KeyTab:
		m.editor.insert(' ')
		m.editor.insert(' ')
	case tea.KeyRunes:
		for _, r := range msg.Runes {
			m.editor.insert(r)
		}
	}
	return m, nil
}
