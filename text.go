package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// wordStyle carries the inline markdown styling of a single word.
type wordStyle struct {
	bold   bool
	italic bool
	code   bool
	link   bool
	strike bool
}

type word struct {
	text    string
	para    int
	section int // index into document.sections, -1 before any heading
	style   wordStyle
	col     int // pre paragraphs: display column where the word starts
	row     int // pre paragraphs: visual row within the paragraph
}

type section struct {
	title string
	start int // word index
}

type paraKind int

const (
	paraText paraKind = iota
	paraHeading
	paraList
	paraQuote
	paraCode
	paraTable
)

// pre reports whether the paragraph is preformatted: its words carry fixed
// row/column positions instead of wrapping to the view width.
func (k paraKind) pre() bool { return k == paraCode || k == paraTable }

type paraInfo struct {
	kind    paraKind
	marker  string // paraList: bullet or number shown before the first line
	width   int    // pre paragraphs: full row width
	sepCols []int  // paraTable: columns where │ separators are drawn
	header  bool   // paraTable: the first row is a header
}

// indent is the left margin wrapped lines of this paragraph render under.
func (pi paraInfo) indent() int {
	switch pi.kind {
	case paraList:
		return utf8.RuneCountInString(pi.marker) + 1
	case paraQuote:
		return 2
	}
	return 0
}

type document struct {
	words      []word
	paraStarts []int      // word index of the first word of each paragraph
	paras      []paraInfo // parallel to paraStarts
	sections   []section
}

type rawPara struct {
	words []word // text/style/col/row set; para and section filled later
	info  paraInfo
}

var (
	linkRe        = regexp.MustCompile(`!?\[([^\]]*)\]\(([^)]*)\)`)
	headingLineRe = regexp.MustCompile(`^#{1,6}\s+(.*)$`)
	listMarkerRe  = regexp.MustCompile(`^([-*+]|\d+[.)])\s+`)
	paraSplitRe   = regexp.MustCompile(`\n\s*\n`)
	tableDivRe    = regexp.MustCompile(`^:?-+:?$`)
)

func loadDocument(path string) (*document, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var paras []rawPara
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".markdown", ".mdx":
		paras = parseMarkdown(string(data))
	default:
		paras = parsePlain(string(data))
	}

	doc, err := buildDocument(paras)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return doc, nil
}

// parseMarkdown splits markdown into typed paragraphs: headings become
// section markers, code blocks and tables keep their layout, list items and
// blockquotes keep their structure, and inline emphasis becomes word styles.
func parseMarkdown(text string) []rawPara {
	p := &mdParser{}
	for line := range strings.SplitSeq(text, "\n") {
		p.feed(line)
	}
	p.flushAll()
	return p.paras
}

type mdParser struct {
	paras []rawPara

	cur     []word
	curInfo paraInfo

	tableRows []string

	inFence   bool
	codeWords []word
	codeRow   int
}

func isFence(trimmed string) bool {
	return strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~")
}

func (p *mdParser) feed(line string) {
	trimmed := strings.TrimSpace(line)

	if p.inFence {
		switch {
		case isFence(trimmed):
			p.inFence = false
			p.flushCode()
		case trimmed == "":
			// Blank rows split the block into paragraphs, so the gap
			// still reads as a gap on screen.
			p.flushCode()
		default:
			p.addCodeLine(line)
		}
		return
	}

	if isFence(trimmed) {
		p.flushAll()
		p.inFence = true
		return
	}
	if strings.HasPrefix(trimmed, "|") {
		p.flushPara()
		p.tableRows = append(p.tableRows, trimmed)
		return
	}
	p.flushTable()

	switch {
	case trimmed == "":
		p.flushPara()

	case headingLineRe.MatchString(trimmed):
		p.flushPara()
		m := headingLineRe.FindStringSubmatch(trimmed)
		if words := parseInline(m[1]); len(words) > 0 {
			p.paras = append(p.paras, rawPara{words: words, info: paraInfo{kind: paraHeading}})
		}

	case strings.HasPrefix(trimmed, ">"):
		if p.curInfo.kind != paraQuote {
			p.flushPara()
			p.curInfo = paraInfo{kind: paraQuote}
		}
		for strings.HasPrefix(trimmed, ">") {
			trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, ">"))
		}
		trimmed = listMarkerRe.ReplaceAllString(trimmed, "")
		p.cur = append(p.cur, parseInline(trimmed)...)

	case listMarkerRe.MatchString(trimmed):
		p.flushPara()
		marker := listMarkerRe.FindStringSubmatch(trimmed)[1]
		if marker == "-" || marker == "*" || marker == "+" {
			marker = "•"
		}
		p.curInfo = paraInfo{kind: paraList, marker: marker}
		p.cur = append(p.cur, parseInline(listMarkerRe.ReplaceAllString(trimmed, ""))...)

	default:
		p.cur = append(p.cur, parseInline(trimmed)...)
	}
}

func (p *mdParser) flushPara() {
	if len(p.cur) > 0 {
		p.paras = append(p.paras, rawPara{words: p.cur, info: p.curInfo})
		p.cur = nil
	}
	p.curInfo = paraInfo{}
}

func (p *mdParser) flushCode() {
	if len(p.codeWords) > 0 {
		width := 0
		for _, w := range p.codeWords {
			width = max(width, w.col+utf8.RuneCountInString(w.text))
		}
		p.paras = append(p.paras, rawPara{
			words: p.codeWords,
			info:  paraInfo{kind: paraCode, width: width},
		})
		p.codeWords = nil
	}
	p.codeRow = 0
}

func (p *mdParser) flushTable() {
	rows := p.tableRows
	p.tableRows = nil
	if len(rows) == 0 {
		return
	}
	if rp := buildTable(rows); len(rp.words) > 0 {
		p.paras = append(p.paras, rp)
	}
}

func (p *mdParser) flushAll() {
	p.flushPara()
	p.flushTable()
	p.flushCode()
}

// addCodeLine records one code line verbatim: each word keeps the column it
// starts at, so indentation survives rendering.
func (p *mdParser) addCodeLine(line string) {
	line = strings.ReplaceAll(line, "\t", "    ")
	var cur []rune
	col, startCol := 0, 0
	flush := func() {
		if len(cur) > 0 {
			p.codeWords = append(p.codeWords, word{text: string(cur), col: startCol, row: p.codeRow})
			cur = nil
		}
	}
	for _, r := range line {
		if unicode.IsSpace(r) {
			flush()
		} else {
			if len(cur) == 0 {
				startCol = col
			}
			cur = append(cur, r)
		}
		col++
	}
	flush()
	p.codeRow++
}

// buildTable lays out table rows on a shared column grid: every cell starts
// at its column's offset, so rows line up under each other.
func buildTable(lines []string) rawPara {
	var rows [][][]word // row -> cell -> words
	header := false
	for _, l := range lines {
		cells := splitTableRow(l)
		if isTableDivider(cells) {
			if len(rows) == 1 {
				header = true
			}
			continue
		}
		row := make([][]word, len(cells))
		for c, cell := range cells {
			row[c] = parseInline(cell)
		}
		rows = append(rows, row)
	}

	var widths []int
	for _, row := range rows {
		for c, cell := range row {
			for c >= len(widths) {
				widths = append(widths, 1)
			}
			widths[c] = max(widths[c], cellWidth(cell))
		}
	}

	info := paraInfo{kind: paraTable, header: header}
	starts := make([]int, len(widths))
	x := 0
	for c := range widths {
		starts[c] = x
		x += widths[c]
		if c < len(widths)-1 {
			info.sepCols = append(info.sepCols, x+1)
			x += 3 // " │ "
		}
	}
	info.width = x

	var words []word
	for r, row := range rows {
		for c, cell := range row {
			col := starts[c]
			for _, w := range cell {
				w.col = col
				w.row = r
				words = append(words, w)
				col += utf8.RuneCountInString(w.text) + 1
			}
		}
	}
	return rawPara{words: words, info: info}
}

func splitTableRow(line string) []string {
	line = strings.TrimPrefix(line, "|")
	line = strings.TrimSuffix(line, "|")
	cells := strings.Split(line, "|")
	for i := range cells {
		cells[i] = strings.TrimSpace(cells[i])
	}
	return cells
}

func isTableDivider(cells []string) bool {
	for _, c := range cells {
		if !tableDivRe.MatchString(c) {
			return false
		}
	}
	return len(cells) > 0
}

func cellWidth(cell []word) int {
	w := 0
	for i, wd := range cell {
		if i > 0 {
			w++
		}
		w += utf8.RuneCountInString(wd.text)
	}
	return w
}

func parsePlain(text string) []rawPara {
	var paras []rawPara
	for _, p := range paraSplitRe.Split(text, -1) {
		var words []word
		for _, f := range strings.Fields(p) {
			words = append(words, word{text: f})
		}
		if len(words) > 0 {
			paras = append(paras, rawPara{words: words})
		}
	}
	return paras
}

// parseInline splits markdown text into styled words, interpreting emphasis
// (**bold**, *italic*), inline `code`, ~~strikethrough~~, and [links](url).
func parseInline(s string) []word {
	p := &inlineParser{}
	last := 0
	for _, m := range linkRe.FindAllStringSubmatchIndex(s, -1) {
		p.feed(s[last:m[0]], false)
		p.feed(s[m[2]:m[3]], true) // label text; the URL is dropped
		last = m[1]
	}
	p.feed(s[last:], false)
	p.flushWord()
	return p.words
}

type inlineParser struct {
	words    []word
	cur      []rune
	curStyle wordStyle // style at the first rune of the current word
	st       wordStyle
}

func (p *inlineParser) flushWord() {
	if len(p.cur) > 0 {
		p.words = append(p.words, word{text: string(p.cur), style: p.curStyle})
		p.cur = nil
	}
}

func (p *inlineParser) add(r rune, link bool) {
	if len(p.cur) == 0 {
		p.curStyle = p.st
		p.curStyle.link = link
	}
	p.cur = append(p.cur, r)
}

func (p *inlineParser) feed(s string, link bool) {
	rs := []rune(s)
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		if unicode.IsSpace(r) {
			p.flushWord()
			continue
		}
		if p.st.code { // inside a code span, only the closing backtick is special
			if r == '`' {
				p.st.code = false
			} else {
				p.add(r, link)
			}
			continue
		}
		switch {
		case r == '`':
			p.st.code = true
		case r == '*' && i+1 < len(rs) && rs[i+1] == '*':
			p.st.bold = !p.st.bold
			i++
		case r == '_' && i+1 < len(rs) && rs[i+1] == '_' && atBoundary(rs, i, i+1):
			p.st.bold = !p.st.bold
			i++
		case r == '~' && i+1 < len(rs) && rs[i+1] == '~':
			p.st.strike = !p.st.strike
			i++
		case r == '*':
			p.st.italic = !p.st.italic
		case r == '_' && atBoundary(rs, i, i):
			p.st.italic = !p.st.italic
		default:
			p.add(r, link)
		}
	}
}

// atBoundary reports whether the delimiter spanning rs[i..j] touches a word
// boundary, so snake_case underscores stay literal.
func atBoundary(rs []rune, i, j int) bool {
	return i == 0 || unicode.IsSpace(rs[i-1]) || j+1 >= len(rs) || unicode.IsSpace(rs[j+1])
}

func buildDocument(paras []rawPara) (*document, error) {
	doc := &document{}
	secIdx := -1
	for _, para := range paras {
		p := len(doc.paraStarts)
		doc.paraStarts = append(doc.paraStarts, len(doc.words))
		doc.paras = append(doc.paras, para.info)
		if para.info.kind == paraHeading {
			secIdx = len(doc.sections)
			titles := make([]string, len(para.words))
			for i, w := range para.words {
				titles[i] = w.text
			}
			doc.sections = append(doc.sections, section{
				title: strings.Join(titles, " "),
				start: len(doc.words),
			})
		}
		for _, w := range para.words {
			w.para = p
			w.section = secIdx
			doc.words = append(doc.words, w)
		}
	}
	if len(doc.words) == 0 {
		return nil, fmt.Errorf("no readable text")
	}
	return doc, nil
}

func (d *document) paraEnd(p int) int {
	if p+1 < len(d.paraStarts) {
		return d.paraStarts[p+1]
	}
	return len(d.words)
}
