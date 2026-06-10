package main

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

var (
	styleRead    = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	styleHead    = lipgloss.NewStyle().Foreground(lipgloss.Color("231")).Bold(true)
	styleFocus   = lipgloss.NewStyle().Reverse(true).Bold(true)
	styleFar     = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	styleFarHead = lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Bold(true)
	styleCode    = lipgloss.NewStyle().Foreground(lipgloss.Color("179"))
	styleLink    = lipgloss.NewStyle().Foreground(lipgloss.Color("75")).Underline(true)
	styleMarker  = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	styleGrid    = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	styleStatus  = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	stylePicker  = lipgloss.NewStyle().Foreground(lipgloss.Color("250")).
			Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("242")).
			Padding(0, 2)
	stylePickerSel = lipgloss.NewStyle().Foreground(lipgloss.Color("231")).Bold(true)

	styleCommentMark = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))

	cursorLineBG = lipgloss.Color("236")
	selectionBG  = lipgloss.Color("24")
)

// View renders the document around the focal word: the current line is
// vertically centered, the current paragraph is bright with the focal word
// highlighted, and everything else is dimmed (subject to style toggles).
func (m model) View() string {
	if m.width < 10 || m.height < 4 {
		return ""
	}

	ctxW := m.ctxW()
	panelW := m.commentPanelW()
	avail := m.width - panelW
	pad := strings.Repeat(" ", max((avail-ctxW)/2, 0))
	lines := layoutLines(m.doc, ctxW)
	li := lineIndex(lines, m.idx)
	curPara := m.doc.words[m.idx].para

	// prefix is the left margin, with a gutter marker on commented lines.
	prefix := func(l line) string {
		if !m.lineCommented(l) {
			return pad
		}
		if len(pad) >= 2 {
			return pad[:len(pad)-2] + styleCommentMark.Render("▌") + " "
		}
		return styleCommentMark.Render("▌") + " "
	}

	rows := make([]string, m.height-1)
	centerRow := (m.height - 1) / 2
	// lineRow maps a visible line index to the screen row it landed on, so
	// the comment panel can align each note to its anchor.
	lineRow := map[int]int{}

	// Current line and everything below.
	r := centerRow
	for j := li; j < len(lines) && r < len(rows); j++ {
		if j > li && lines[j].para != lines[j-1].para {
			r++ // blank row between paragraphs
			if r >= len(rows) {
				break
			}
		}
		rows[r] = prefix(lines[j]) + m.renderLine(lines[j], curPara, j == li, ctxW)
		lineRow[j] = r
		r++
	}
	// Everything above.
	r = centerRow - 1
	for j := li - 1; j >= 0 && r >= 0; j-- {
		if lines[j].para != lines[j+1].para {
			r-- // blank row between paragraphs
			if r < 0 {
				break
			}
		}
		rows[r] = prefix(lines[j]) + m.renderLine(lines[j], curPara, false, ctxW)
		lineRow[j] = r
		r--
	}

	if panelW > 0 {
		// Keep wide lines (code, tables) from bleeding into the panel.
		for i := range rows {
			rows[i] = ansi.Truncate(rows[i], avail-1, "")
		}
		m.renderCommentPanel(rows, lines, lineRow, avail, panelW)
	}

	if m.picker {
		m.overlayPicker(rows)
	}
	if m.editing && panelW == 0 {
		m.overlayEditor(rows)
	}

	// Preformatted lines (code, tables) can be wider than the view.
	for i, row := range rows {
		rows[i] = ansi.Truncate(row, m.width, "")
	}

	rows = append(rows, m.statusBar())
	return strings.Join(rows, "\n")
}

func (m model) renderLine(l line, curPara int, current bool, ctxW int) string {
	info := m.doc.paras[l.para]
	cursorline := current && m.style.LineHighlight
	bright := !m.style.DimOthers || l.para == curPara

	// On the cursor line every segment, including the gaps between words,
	// carries the background so it reads as one continuous bar.
	apply := func(s lipgloss.Style) lipgloss.Style {
		if cursorline {
			return s.Background(cursorLineBG)
		}
		return s
	}

	lo, hi, selOk := m.selRange()

	var b strings.Builder
	used := 0
	write := func(s lipgloss.Style, text string) {
		b.WriteString(apply(s).Render(text))
		used += utf8.RuneCountInString(text)
	}
	// writeSel renders selected text with the selection background, which
	// wins over the cursor-line background.
	writeSel := func(s lipgloss.Style, text string) {
		b.WriteString(s.Background(selectionBG).Render(text))
		used += utf8.RuneCountInString(text)
	}

	marker := styleMarker
	if !bright {
		marker = styleFar
	}
	switch info.kind {
	case paraList:
		if l.from == m.doc.paraStarts[l.para] {
			write(marker, info.marker+" ")
		} else {
			write(lipgloss.NewStyle(), strings.Repeat(" ", info.indent()))
		}
	case paraQuote:
		write(marker, "▎ ")
	}

	if info.kind.pre() {
		// Words sit at fixed columns; gaps carry the table grid, and the
		// whole header row is underlined to double as the header divider.
		header := info.kind == paraTable && info.header && m.doc.words[l.from].row == 0
		gap, grid := lipgloss.NewStyle(), styleGrid
		if header {
			gap, grid = gap.Underline(true), grid.Underline(true)
		}
		gapTo := func(to int) {
			for _, sc := range info.sepCols {
				if sc >= used && sc < to {
					write(gap, strings.Repeat(" ", sc-used))
					write(grid, "│")
				}
			}
			if to > used {
				write(gap, strings.Repeat(" ", to-used))
			}
		}
		for i := l.from; i < l.to; i++ {
			w := m.doc.words[i]
			gapTo(w.col)
			st := m.wordStyle(w, info, bright, i == m.idx, header)
			if selOk && i >= lo && i <= hi {
				writeSel(st, w.text)
			} else {
				write(st, w.text)
			}
		}
		gapTo(info.width)
	} else {
		for i := l.from; i < l.to; i++ {
			sel := selOk && i >= lo && i <= hi
			if i > l.from {
				// The joining space is selected only when it sits between
				// two selected words, so the highlight reads continuous.
				if selOk && i-1 >= lo && i <= hi {
					writeSel(lipgloss.NewStyle(), " ")
				} else {
					write(lipgloss.NewStyle(), " ")
				}
			}
			w := m.doc.words[i]
			st := m.wordStyle(w, info, bright, i == m.idx, false)
			if sel {
				writeSel(st, w.text)
			} else {
				write(st, w.text)
			}
		}
	}

	if cursorline && used < ctxW {
		write(lipgloss.NewStyle(), strings.Repeat(" ", ctxW-used))
	}
	return b.String()
}

// wordStyle picks the lipgloss style for one word from its paragraph kind,
// inline markdown styling, and focus/brightness.
func (m model) wordStyle(w word, info paraInfo, bright, focus, underline bool) lipgloss.Style {
	var s lipgloss.Style
	switch {
	case focus && m.style.WordHighlight:
		s = styleFocus
	case info.kind == paraHeading && bright:
		s = styleHead
	case info.kind == paraHeading:
		s = styleFarHead
	case !bright:
		s = styleFar
	case info.kind == paraCode || w.style.code:
		s = styleCode
	case w.style.link:
		s = styleLink
	default:
		s = styleRead
	}
	if w.style.bold {
		s = s.Bold(true)
	}
	if w.style.italic {
		s = s.Italic(true)
	}
	if w.style.strike {
		s = s.Strikethrough(true)
	}
	if underline {
		s = s.Underline(true)
	}
	return s
}

// overlayPicker draws the style toggle panel over the center of the view.
func (m model) overlayPicker(rows []string) {
	items := []struct {
		label string
		on    bool
	}{
		{"Word highlight", m.style.WordHighlight},
		{"Line highlight", m.style.LineHighlight},
		{"Dim other paragraphs", m.style.DimOthers},
	}

	body := []string{stylePickerSel.Render("Style"), ""}
	for i, it := range items {
		box := "[ ]"
		if it.on {
			box = "[x]"
		}
		text := fmt.Sprintf("%s %s", box, it.label)
		if i == m.pickerSel {
			body = append(body, stylePickerSel.Render("▸ "+text))
		} else {
			body = append(body, "  "+text)
		}
	}
	body = append(body, "", styleStatus.Render("space toggle · esc close"))

	panel := stylePicker.Render(strings.Join(body, "\n"))
	plines := strings.Split(panel, "\n")
	top := max((len(rows)-len(plines))/2, 0)
	for i, pl := range plines {
		r := top + i
		if r >= len(rows) {
			break
		}
		pad := max((m.width-lipgloss.Width(pl))/2, 0)
		rows[r] = strings.Repeat(" ", pad) + pl
	}
}

func (m model) statusBar() string {
	if m.flash != "" {
		return m.centerStatus(m.flash)
	}

	parts := []string{
		fmt.Sprintf("%d%%", (m.idx+1)*100/len(m.doc.words)),
	}
	if sec := m.doc.words[m.idx].section; sec >= 0 {
		parts = append(parts, "§ "+m.doc.sections[sec].title)
	}

	switch {
	case m.editing:
		parts = append(parts, "editing comment · ctrl+s save · esc cancel")
	case m.selecting:
		parts = append(parts, "selecting · c comment · esc cancel")
	case m.commentAt(m.idx) >= 0 && m.commentPanelW() == 0:
		// Only echo the note when the panel isn't there to show it.
		parts = append(parts, "💬 "+oneLine(m.comments[m.commentAt(m.idx)].Body))
	default:
		parts = append(parts, "hjkl move · {} para · n/p section · ⇧ select · c comment · x export · s style · q quit")
	}
	return m.centerStatus(strings.Join(parts, " · "))
}

// centerStatus truncates a status string to the view width and centers it.
func (m model) centerStatus(status string) string {
	if r := []rune(status); len(r) > m.width {
		status = string(r[:m.width])
	}
	if pad := (m.width - utf8.RuneCountInString(status)) / 2; pad > 0 {
		status = strings.Repeat(" ", pad) + status
	}
	return styleStatus.Render(status)
}

// overlayEditor draws the comment composer over the center of the view: the
// quoted passage, the editable body with a cursor, and the key hints.
func (m model) overlayEditor(rows []string) {
	e := m.editor
	w := max(min(m.width-8, 60), 20)

	title := "Comment"
	hint := "ctrl+s save · esc cancel"
	if e.editIdx >= 0 {
		title, hint = "Edit comment", "ctrl+s save (empty deletes) · esc cancel"
	}

	body := []string{stylePickerSel.Render(title), ""}
	body = append(body, styleFar.Render("> "+truncate(strings.ReplaceAll(e.quote, "\n", " "), w)))
	body = append(body, "")
	for r, line := range e.lines {
		body = append(body, renderEditorLine(line, r == e.row, e.col))
	}
	body = append(body, "", styleStatus.Render(hint))

	panel := stylePicker.Width(w).Render(strings.Join(body, "\n"))
	plines := strings.Split(panel, "\n")
	top := max((len(rows)-len(plines))/2, 0)
	for i, pl := range plines {
		r := top + i
		if r >= len(rows) {
			break
		}
		pad := max((m.width-lipgloss.Width(pl))/2, 0)
		rows[r] = strings.Repeat(" ", pad) + pl
	}
}

// renderEditorLine renders one body line, drawing a reverse-video block at the
// cursor when this is the cursor line.
func renderEditorLine(line []rune, cursor bool, col int) string {
	if !cursor {
		return string(line)
	}
	c := min(col, len(line))
	if c == len(line) {
		return string(line) + styleFocus.Render(" ")
	}
	return string(line[:c]) + styleFocus.Render(string(line[c])) + string(line[c+1:])
}

// truncate shortens s to at most w runes, marking elision with an ellipsis.
func truncate(s string, w int) string {
	r := []rune(s)
	if len(r) <= w {
		return s
	}
	if w <= 1 {
		return string(r[:max(w, 0)])
	}
	return string(r[:w-1]) + "…"
}

// commentCard is one panel entry: a stack of pre-rendered lines anchored to a
// screen row.
type commentCard struct {
	anchor int
	lines  []string
}

// renderCommentPanel draws comment cards — and the editor when it's open —
// down the right margin, each aligned to the line it annotates and stacked so
// they never overlap.
func (m model) renderCommentPanel(rows []string, lines []line, lineRow map[int]int, left, width int) {
	textW := max(width-2, 4) // room for the "▌ " / "  " prefix

	// anchorRow is the screen row of the first visible line a span touches,
	// or -1 when none of it is on screen.
	anchorRow := func(start, end int) int {
		for li := lineIndex(lines, start); li <= lineIndex(lines, end) && li < len(lines); li++ {
			if r, ok := lineRow[li]; ok {
				return r
			}
		}
		return -1
	}

	var cards []commentCard
	for i, c := range m.comments {
		if m.editing && i == m.editor.editIdx {
			continue // drawn as the editor card instead
		}
		if a := anchorRow(c.Start, c.End); a >= 0 {
			cards = append(cards, commentCard{a, m.commentCardLines(c, textW, m.commentAt(m.idx) == i)})
		}
	}
	if m.editing {
		a := len(rows) / 2
		if r := anchorRow(m.editor.start, m.editor.end); r >= 0 {
			a = r
		}
		cards = append(cards, commentCard{a, m.editorCardLines(textW)})
	}

	sort.SliceStable(cards, func(i, j int) bool { return cards[i].anchor < cards[j].anchor })

	next := 0
	for _, card := range cards {
		start := max(card.anchor, next)
		for i, ln := range card.lines {
			rr := start + i
			if rr < 0 || rr >= len(rows) {
				break
			}
			rows[rr] = placeAt(rows[rr], ln, left)
		}
		next = start + len(card.lines) + 1 // blank line between cards
	}
}

// commentCardLines renders a saved comment: a dimmed quote header over the
// wrapped body, brighter when the focal word sits inside it.
func (m model) commentCardLines(c comment, w int, focused bool) []string {
	body := styleFar
	if focused {
		body = styleRead
	}
	out := []string{styleCommentMark.Render("▌") + " " + styleFar.Italic(true).Render(truncate(oneLine(c.Quote), w))}
	for _, ln := range wrapText(c.Body, w) {
		out = append(out, "  "+body.Render(ln))
	}
	return out
}

// editorCardLines renders the live editor as a panel card: title, the quoted
// passage, the editable body with a cursor, and the key hints.
func (m model) editorCardLines(w int) []string {
	e := m.editor
	title, hint := "Comment", "ctrl+s save · esc cancel"
	if e.editIdx >= 0 {
		title, hint = "Edit comment", "ctrl+s save · esc cancel · empty deletes"
	}
	out := []string{stylePickerSel.Render("▌") + " " + stylePickerSel.Render(title)}
	out = append(out, "  "+styleFar.Italic(true).Render(truncate(oneLine(e.quote), w)))
	for r, line := range e.lines {
		for _, seg := range wrapEditorLine(line, r == e.row, e.col, w) {
			out = append(out, "  "+seg)
		}
	}
	for _, ln := range wrapText(hint, w) {
		out = append(out, "  "+styleStatus.Render(ln))
	}
	return out
}

// wrapEditorLine character-wraps one logical editor line to width w, drawing
// the reverse-video cursor at cursorCol when this is the cursor line. Character
// wrapping (rather than word) keeps the cursor's position unambiguous so it is
// always on screen, even in the narrow panel.
func wrapEditorLine(line []rune, cursor bool, cursorCol, w int) []string {
	w = max(w, 1)
	if len(line) == 0 {
		if cursor {
			return []string{styleFocus.Render(" ")}
		}
		return []string{""}
	}

	var out []string
	for start := 0; start < len(line); start += w {
		end := min(start+w, len(line))
		seg := line[start:end]
		if cursor && cursorCol >= start && cursorCol < end {
			c := cursorCol - start
			out = append(out, string(seg[:c])+styleFocus.Render(string(seg[c]))+string(seg[c+1:]))
		} else {
			out = append(out, string(seg))
		}
	}
	// A cursor sitting just past the last character either tacks onto the
	// final segment or, if that segment is full, wraps to a fresh line.
	if cursor && cursorCol >= len(line) {
		if len(line)%w == 0 {
			out = append(out, styleFocus.Render(" "))
		} else {
			out[len(out)-1] += styleFocus.Render(" ")
		}
	}
	return out
}

// placeAt pads row out to column col (counting visible width) and appends s,
// so panel content lands in a fixed right-hand column.
func placeAt(row, s string, col int) string {
	if w := lipgloss.Width(row); w < col {
		row += strings.Repeat(" ", col-w)
	}
	return row + s
}

// wrapText word-wraps s to width w, preserving its existing line breaks.
func wrapText(s string, w int) []string {
	w = max(w, 1)
	var out []string
	for _, para := range strings.Split(s, "\n") {
		words := strings.Fields(para)
		if len(words) == 0 {
			out = append(out, "")
			continue
		}
		line := words[0]
		for _, word := range words[1:] {
			if utf8.RuneCountInString(line)+1+utf8.RuneCountInString(word) <= w {
				line += " " + word
			} else {
				out = append(out, line)
				line = word
			}
		}
		out = append(out, line)
	}
	for i := range out {
		out[i] = truncate(out[i], w)
	}
	return out
}

// oneLine flattens a multi-line string to a single line for compact display.
func oneLine(s string) string {
	return strings.ReplaceAll(s, "\n", " ")
}
