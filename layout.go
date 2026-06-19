package main

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
			wl := dispWidth(d.words[i].text)
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

// tableGeom is a table paragraph laid out for one view width. Indexing is
// by word offset from the paragraph start.
type tableGeom struct {
	col     []int // display column where each word starts
	sub     []int // visual sub-row of each word within its table row
	heights []int // screen rows each table row occupies
	sepCols []int // columns where │ separators are drawn
	width   int   // full row width
}

// layoutTable fits a table to the view width. Columns get their natural
// width (the longest cell) when the table fits; otherwise the widest columns
// shrink — never below their longest single word — and cell text wraps
// within the column, so a table row can span several screen rows.
func layoutTable(d *document, p, width int) tableGeom {
	start, end := d.paraStarts[p], d.paraEnd(p)
	info := d.paras[p]
	ncols := max(info.ncols, 1)
	nrows := d.words[end-1].row + 1

	// cells[r][c] holds the word indices of one cell, in order.
	cells := make([][][]int, nrows)
	for r := range cells {
		cells[r] = make([][]int, ncols)
	}
	for i := start; i < end; i++ {
		w := d.words[i]
		cells[w.row][w.cell] = append(cells[w.row][w.cell], i)
	}

	natural := make([]int, ncols) // longest cell
	minw := make([]int, ncols)    // longest single word
	for r := range cells {
		for c, ws := range cells[r] {
			cw := 0
			for k, i := range ws {
				if k > 0 {
					cw++
				}
				wl := dispWidth(d.words[i].text)
				cw += wl
				minw[c] = max(minw[c], wl)
			}
			natural[c] = max(natural[c], cw)
		}
	}

	widths := make([]int, ncols)
	total := 0
	for c := range widths {
		natural[c] = max(natural[c], 1)
		minw[c] = max(minw[c], 1)
		widths[c] = natural[c]
		total += natural[c]
	}
	// Shrink the widest shrinkable column one cell at a time until the
	// table fits; wide columns share the squeeze. If every column is at
	// its minimum the table overflows, words being unsplittable.
	avail := max(width-3*(ncols-1), ncols)
	for total > avail {
		widest := -1
		for c := range widths {
			if widths[c] > minw[c] && (widest < 0 || widths[c] > widths[widest]) {
				widest = c
			}
		}
		if widest < 0 {
			break
		}
		widths[widest]--
		total--
	}

	g := tableGeom{
		col:     make([]int, end-start),
		sub:     make([]int, end-start),
		heights: make([]int, nrows),
	}
	starts := make([]int, ncols)
	x := 0
	for c := range widths {
		starts[c] = x
		x += widths[c]
		if c < ncols-1 {
			g.sepCols = append(g.sepCols, x+1)
			x += 3 // " │ "
		}
	}
	g.width = x

	for r := range cells {
		height := 1
		for c, ws := range cells[r] {
			w, sub := 0, 0
			for _, i := range ws {
				wl := dispWidth(d.words[i].text)
				if w > 0 && w+1+wl > widths[c] {
					sub++
					w = 0
				}
				if w > 0 {
					w++
				}
				g.col[i-start] = starts[c] + w
				g.sub[i-start] = sub
				w += wl
			}
			height = max(height, sub+1)
		}
		g.heights[r] = height
	}
	return g
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
// line: the laid-out column for tables, the stored column for code, the
// running width (plus indent) otherwise.
func columnOf(d *document, l line, idx, width int) int {
	info := d.paras[l.para]
	if info.kind == paraTable {
		return layoutTable(d, l.para, width).col[idx-d.paraStarts[l.para]]
	}
	if info.kind.pre() {
		return d.words[idx].col
	}
	col := info.indent()
	for i := l.from; i < idx; i++ {
		col += dispWidth(d.words[i].text) + 1
	}
	return col
}

// wordAtColumn returns the word on line l whose span covers column col,
// or the last word starting before it — how an editor lands the cursor
// when moving between lines of different lengths. For tables — where a
// row can wrap onto several screen rows — it lands on the first sub-row.
func wordAtColumn(d *document, l line, col, width int) int {
	info := d.paras[l.para]
	if info.kind == paraTable {
		g := layoutTable(d, l.para, width)
		ps := d.paraStarts[l.para]
		best := l.from
		for i := l.from; i < l.to; i++ {
			if g.sub[i-ps] != 0 {
				continue
			}
			if g.col[i-ps] > col {
				break
			}
			best = i
		}
		return best
	}
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
			start += dispWidth(d.words[i].text) + 1
		}
	}
	return best
}
