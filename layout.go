package main

import "unicode/utf8"

// line is one wrapped visual line: words [from,to) of a single paragraph.
type line struct {
	from, to int
	para     int
}

// layoutLines wraps the whole document at the given width. Both movement
// and rendering use this, so cursor motion always matches what's on screen.
// Preformatted paragraphs (code, tables) keep their stored rows instead of
// wrapping.
func layoutLines(d *document, width int) []line {
	width = max(width, 1)
	var lines []line
	for p := range d.paraStarts {
		start, end := d.paraStarts[p], d.paraEnd(p)
		info := d.paras[p]

		if info.kind.pre() {
			from := start
			for i := start + 1; i <= end; i++ {
				if i == end || d.words[i].row != d.words[from].row {
					lines = append(lines, line{from, i, p})
					from = i
				}
			}
			continue
		}

		wrapW := max(width-info.indent(), 1)
		from, w := start, 0
		for i := start; i < end; i++ {
			wl := utf8.RuneCountInString(d.words[i].text)
			if w > 0 && w+1+wl > wrapW {
				lines = append(lines, line{from, i, p})
				from, w = i, 0
			}
			if w > 0 {
				w++
			}
			w += wl
		}
		lines = append(lines, line{from, end, p})
	}
	return lines
}

// lineIndex returns the index of the line containing word idx.
func lineIndex(lines []line, idx int) int {
	lo, hi := 0, len(lines)-1
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if lines[mid].from <= idx {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return lo
}

// columnOf returns the display column at which word idx starts within its
// line: the stored column for preformatted paragraphs, the running width
// (plus indent) otherwise.
func columnOf(d *document, l line, idx int) int {
	info := d.paras[l.para]
	if info.kind.pre() {
		return d.words[idx].col
	}
	col := info.indent()
	for i := l.from; i < idx; i++ {
		col += utf8.RuneCountInString(d.words[i].text) + 1
	}
	return col
}

// wordAtColumn returns the word on line l whose span covers column col,
// or the last word starting before it — how an editor lands the cursor
// when moving between lines of different lengths.
func wordAtColumn(d *document, l line, col int) int {
	info := d.paras[l.para]
	pre := info.kind.pre()
	best, start := l.from, info.indent()
	for i := l.from; i < l.to; i++ {
		if pre {
			start = d.words[i].col
		}
		if start > col {
			break
		}
		best = i
		if !pre {
			start += utf8.RuneCountInString(d.words[i].text) + 1
		}
	}
	return best
}
