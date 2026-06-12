package main

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Colors live in theme.go; the active set is m.st, built from the chosen
// theme. Reference styles as m.st.read, m.st.focus, and so on.

// View renders the document around the focal word: the current line is
// vertically centered, the current paragraph is bright with the focal word
// highlighted, and everything else is dimmed (subject to style toggles).
func (m model) View() string {
	if m.width < 10 || m.height < 4 {
		return ""
	}

	ctxW := m.ctxW()
	panelW := m.commentPanelW()

	// Center the text alone, or — when the panel is open — center the whole
	// text+gap+panel block so the notes hug their context instead of being
	// flung to the screen edge.
	leftPad := max((m.width-ctxW)/2, 0)
	panelLeft := 0
	if panelW > 0 {
		leftPad = max((m.width-(ctxW+commentGap+panelW))/2, 0)
		panelLeft = leftPad + ctxW + commentGap
	}
	pad := strings.Repeat(" ", leftPad)
	lines := layoutLines(m.doc, ctxW)
	li := lineIndex(lines, m.idx)
	curPara := m.doc.words[m.idx].para

	// prefix is the left margin, with a gutter marker on commented lines.
	prefix := func(l line) string {
		if !m.lineCommented(l) {
			return pad
		}
		if len(pad) >= 2 {
			return pad[:len(pad)-2] + m.st.commentMark.Render("▌") + " "
		}
		return m.st.commentMark.Render("▌") + " "
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
		// Keep wide lines (code, tables) from bleeding into the gap or panel.
		textRight := leftPad + ctxW
		for i := range rows {
			rows[i] = ansi.Truncate(rows[i], textRight, "")
		}
		m.renderCommentPanel(rows, lines, lineRow, panelLeft, panelW)
	}

	if m.picker {
		m.overlayPicker(rows)
	}
	if m.themePicker {
		m.overlayThemePicker(rows)
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
			return s.Background(m.st.cursorLine)
		}
		return s
	}

	lo, hi, selOk := m.selRange()

	var b strings.Builder
	used := 0
	write := func(s lipgloss.Style, text string) {
		b.WriteString(apply(s).Render(text))
		used += dispWidth(text)
	}
	// writeSel renders selected text with the selection background, which
	// wins over the cursor-line background.
	writeSel := func(s lipgloss.Style, text string) {
		b.WriteString(s.Background(m.st.selection).Render(text))
		used += dispWidth(text)
	}
	// writeFocus renders the focal word with its own highlight intact: the
	// focus style already carries a background, so it must bypass apply (which
	// would paint the cursor-line bar over it) and any selection beneath it.
	writeFocus := func(s lipgloss.Style, text string) {
		b.WriteString(s.Render(text))
		used += dispWidth(text)
	}

	marker := m.st.marker
	if !bright {
		marker = m.st.far
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
		gap, grid := lipgloss.NewStyle(), m.st.grid
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
			switch {
			case i == m.idx && m.style.WordHighlight:
				writeFocus(st, w.text)
			case selOk && i >= lo && i <= hi:
				writeSel(st, w.text)
			default:
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
			switch {
			case i == m.idx && m.style.WordHighlight:
				writeFocus(st, w.text)
			case sel:
				writeSel(st, w.text)
			default:
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
		s = m.st.focus
	case info.kind == paraHeading && bright:
		s = m.st.head
	case info.kind == paraHeading:
		s = m.st.farHead
	case !bright:
		s = m.st.far
	case info.kind == paraCode || w.style.code:
		s = m.st.code
	case w.style.link:
		s = m.st.link
	default:
		s = m.st.read
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

	body := []string{m.st.pickerSel.Render("Style"), ""}
	for i, it := range items {
		box := "[ ]"
		if it.on {
			box = "[x]"
		}
		text := fmt.Sprintf("%s %s", box, it.label)
		if i == m.pickerSel {
			body = append(body, m.st.pickerSel.Render("▸ "+text))
		} else {
			body = append(body, "  "+text)
		}
	}
	body = append(body, "", m.st.status.Render("space toggle · esc close"))

	panel := m.st.picker.Render(strings.Join(body, "\n"))
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

// overlayThemePicker lists the themes, each drawn in its own colors as a live
// preview, with the active one marked. Moving the cursor switches the theme.
func (m model) overlayThemePicker(rows []string) {
	nameW := 0
	for _, t := range themes {
		nameW = max(nameW, len(t.name))
	}

	body := []string{m.st.pickerSel.Render("Theme"), ""}
	for i, t := range themes {
		ts := newStyles(t)
		name := ts.read.Render(t.name + strings.Repeat(" ", nameW-len(t.name)))
		swatch := ts.commentMark.Render("●") + ts.link.Render("●") + ts.code.Render("●") + " " + ts.focus.Render(" A ")
		row := name + "  " + swatch
		if i == m.themeIdx {
			body = append(body, m.st.pickerSel.Render("▸ ")+row)
		} else {
			body = append(body, "  "+row)
		}
	}
	body = append(body, "", m.st.status.Render("j/k preview · esc close"))

	panel := m.st.picker.Render(strings.Join(body, "\n"))
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
		parts = append(parts, "hjkl move · {} para · n/b section · ⇧ select · c comment · p path · x export · s style · t theme · q quit")
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
	return m.st.status.Render(status)
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

	body := []string{m.st.pickerSel.Render(title), ""}
	body = append(body, m.st.far.Render("> "+truncate(strings.ReplaceAll(e.quote, "\n", " "), w)))
	body = append(body, "")
	for r, line := range e.lines {
		body = append(body, m.renderEditorLine(line, r == e.row, e.col))
	}
	body = append(body, "", m.st.status.Render(hint))

	panel := m.st.picker.Width(w).Render(strings.Join(body, "\n"))
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

// renderEditorLine renders one body line, drawing the cursor block at the
// cursor column when this is the cursor line.
func (m model) renderEditorLine(line []rune, cursor bool, col int) string {
	if !cursor {
		return string(line)
	}
	c := min(col, len(line))
	if c == len(line) {
		return string(line) + m.st.focus.Render(" ")
	}
	return string(line[:c]) + m.st.focus.Render(string(line[c])) + string(line[c+1:])
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
	body := m.st.far
	if focused {
		body = m.st.read
	}
	out := []string{m.st.commentMark.Render("▌") + " " + m.st.far.Italic(true).Render(truncate(oneLine(c.Quote), w))}
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
	out := []string{m.st.pickerSel.Render("▌") + " " + m.st.pickerSel.Render(title)}
	out = append(out, "  "+m.st.far.Italic(true).Render(truncate(oneLine(e.quote), w)))
	for r, line := range e.lines {
		for _, seg := range m.wrapEditorLine(line, r == e.row, e.col, w) {
			out = append(out, "  "+seg)
		}
	}
	for _, ln := range wrapText(hint, w) {
		out = append(out, "  "+m.st.status.Render(ln))
	}
	return out
}

// wrapEditorLine character-wraps one logical editor line to width w, drawing
// the cursor block at cursorCol when this is the cursor line. Character
// wrapping (rather than word) keeps the cursor's position unambiguous so it is
// always on screen, even in the narrow panel.
func (m model) wrapEditorLine(line []rune, cursor bool, cursorCol, w int) []string {
	w = max(w, 1)
	if len(line) == 0 {
		if cursor {
			return []string{m.st.focus.Render(" ")}
		}
		return []string{""}
	}

	var out []string
	for start := 0; start < len(line); start += w {
		end := min(start+w, len(line))
		seg := line[start:end]
		if cursor && cursorCol >= start && cursorCol < end {
			c := cursorCol - start
			out = append(out, string(seg[:c])+m.st.focus.Render(string(seg[c]))+string(seg[c+1:]))
		} else {
			out = append(out, string(seg))
		}
	}
	// A cursor sitting just past the last character either tacks onto the
	// final segment or, if that segment is full, wraps to a fresh line.
	if cursor && cursorCol >= len(line) {
		if len(line)%w == 0 {
			out = append(out, m.st.focus.Render(" "))
		} else {
			out[len(out)-1] += m.st.focus.Render(" ")
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
