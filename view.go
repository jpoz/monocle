package main

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
)

var (
	styleWord   = lipgloss.NewStyle().Foreground(lipgloss.Color("231")).Bold(true)
	styleORP    = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)
	styleNear   = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	styleFar    = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	styleGuide  = lipgloss.NewStyle().Foreground(lipgloss.Color("238"))
	styleTick   = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	styleStatus = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
)

func (m model) View() string {
	if m.width < 10 || m.height < 7 {
		return ""
	}

	cur := m.doc.words[m.idx]
	runes := []rune(cur.text)
	orp := orpIndex(cur.text)
	pivot := m.width / 2
	wordStart := pivot - orp

	paraStart := m.doc.paraStarts[cur.para]
	paraEnd := len(m.doc.words)
	if cur.para+1 < len(m.doc.paraStarts) {
		paraEnd = m.doc.paraStarts[cur.para+1]
	}

	// Center line: the focal word with as many neighbors as fit beside it.
	leftWords, leftLen, leftRest := takeBackward(m.doc.words[paraStart:m.idx], wordStart-1)
	rightBudget := m.width - (wordStart + len(runes)) - 1
	rightWords, _, rightRest := takeForward(m.doc.words[m.idx+1:paraEnd], rightBudget)

	var center strings.Builder
	center.WriteString(strings.Repeat(" ", max(wordStart-1-leftLen, 0)))
	if len(leftWords) > 0 {
		center.WriteString(styleNear.Render(strings.Join(leftWords, " ")))
	}
	if wordStart > 0 {
		center.WriteString(" ")
	}
	center.WriteString(styleWord.Render(string(runes[:orp])))
	center.WriteString(styleORP.Render(string(runes[orp])))
	if orp+1 < len(runes) {
		center.WriteString(styleWord.Render(string(runes[orp+1:])))
	}
	if len(rightWords) > 0 {
		center.WriteString(" " + styleNear.Render(strings.Join(rightWords, " ")))
	}

	// Guide rails with the pivot tick the eye rests on.
	guideW := min(m.width-4, 64)
	guidePad := strings.Repeat(" ", max(pivot-guideW/2, 0))
	tickAt := pivot - max(pivot-guideW/2, 0)
	rail := func(tick string) string {
		return guidePad +
			styleGuide.Render(strings.Repeat("─", tickAt)) +
			styleTick.Render(tick) +
			styleGuide.Render(strings.Repeat("─", max(guideW-tickAt-1, 0)))
	}

	// Greyed paragraph context above and below the focal line.
	ctxW := min(m.width-8, 72)
	ctxPad := strings.Repeat(" ", max((m.width-ctxW)/2, 0))
	maxCtx := max((m.height-7)/2, 0)

	above := wrapWords(leftRest, ctxW)
	if len(above) > maxCtx {
		above = above[len(above)-maxCtx:]
	}
	below := wrapWords(rightRest, ctxW)
	if len(below) > maxCtx {
		below = below[:maxCtx]
	}

	rows := make([]string, m.height-1)
	centerRow := (m.height - 1) / 2
	rows[centerRow] = center.String()
	rows[centerRow-1] = rail("┬")
	rows[centerRow+1] = rail("┴")
	for i, line := range above {
		if r := centerRow - 2 - (len(above) - 1 - i); r >= 0 {
			// Right-align so the context reads as flowing into the pivot.
			rows[r] = ctxPad + strings.Repeat(" ", ctxW-utf8.RuneCountInString(line)) + styleFar.Render(line)
		}
	}
	for i, line := range below {
		if r := centerRow + 2 + i; r < len(rows) {
			rows[r] = ctxPad + styleFar.Render(line)
		}
	}

	state := "▶"
	if m.paused {
		state = "⏸"
	}
	status := fmt.Sprintf("%s %d wpm · %d/%d (%d%%) · space play/pause · ←/→ word · ↑/↓ paragraph · +/- speed · q quit",
		state, m.wpm, m.idx+1, len(m.doc.words), (m.idx+1)*100/len(m.doc.words))
	if pad := (m.width - utf8.RuneCountInString(status)) / 2; pad > 0 {
		status = strings.Repeat(" ", pad) + status
	}
	rows = append(rows, styleStatus.Render(status))

	return strings.Join(rows, "\n")
}

// takeBackward takes words from the end of ws that fit in width when joined
// by spaces, returning them in order plus the leftover prefix.
func takeBackward(ws []word, width int) (fit []string, used int, rest []string) {
	i := len(ws)
	for i > 0 {
		wl := utf8.RuneCountInString(ws[i-1].text)
		need := wl
		if used > 0 {
			need++
		}
		if used+need > width {
			break
		}
		used += need
		i--
	}
	for _, w := range ws[i:] {
		fit = append(fit, w.text)
	}
	for _, w := range ws[:i] {
		rest = append(rest, w.text)
	}
	return fit, used, rest
}

func takeForward(ws []word, width int) (fit []string, used int, rest []string) {
	i := 0
	for i < len(ws) {
		wl := utf8.RuneCountInString(ws[i].text)
		need := wl
		if used > 0 {
			need++
		}
		if used+need > width {
			break
		}
		used += need
		i++
	}
	for _, w := range ws[:i] {
		fit = append(fit, w.text)
	}
	for _, w := range ws[i:] {
		rest = append(rest, w.text)
	}
	return fit, used, rest
}

// wrapWords greedily wraps words into lines no wider than width.
func wrapWords(ws []string, width int) []string {
	var lines []string
	var cur strings.Builder
	curLen := 0
	for _, w := range ws {
		wl := utf8.RuneCountInString(w)
		if curLen > 0 && curLen+1+wl > width {
			lines = append(lines, cur.String())
			cur.Reset()
			curLen = 0
		}
		if curLen > 0 {
			cur.WriteString(" ")
			curLen++
		}
		cur.WriteString(w)
		curLen += wl
	}
	if curLen > 0 {
		lines = append(lines, cur.String())
	}
	return lines
}
