package main

import (
	"bytes"
	"strconv"
	"strings"
)

// mdx_render.go turns the parsed MDX node tree into HTML. Each renderer mirrors
// the visual-plan component of the same name, emitting the same .arc-mdx-*
// class vocabulary that web/mdx.css styles. Prose nodes flow through goldmark;
// component children recurse through renderMDXFragment so nested components
// (a Diagram inside a Column, say) render rather than leak as text.

// renderMDXFragment renders a run of MDX source (markdown + components).
func renderMDXFragment(src string) string {
	var b strings.Builder
	for _, nd := range scanMDX(dedent(src)) {
		if nd.isElem {
			b.WriteString(renderComponent(nd))
		} else {
			b.WriteString(renderMarkdown(nd.md))
		}
	}
	return b.String()
}

// renderMarkdown runs a prose chunk through the shared goldmark renderer.
func renderMarkdown(s string) string {
	var buf bytes.Buffer
	if err := mdRenderer.Convert([]byte(s), &buf); err != nil {
		return "<p>" + esc(s) + "</p>"
	}
	return buf.String()
}

func renderComponent(nd mdxNode) string {
	switch nd.name {
	case "RichText":
		return renderMDXFragment(nd.children)
	case "Callout":
		return renderCallout(attrStr(nd.attrs, "tone"), attrStr(nd.attrs, "title"), renderMDXFragment(nd.children))
	case "Columns":
		return `<div class="arc-mdx-columns">` + renderMDXFragment(nd.children) + `</div>`
	case "Column":
		return renderColumn(nd)
	case "Table":
		return renderTable(nd.attrs)
	case "Diagram":
		return renderShadowFigure("diagram", asObj(nd.attrs["data"]), diagramBaseCSS)
	case "HtmlBlock":
		return renderShadowFigure("htmlblock", objFromAttrs(nd.attrs), htmlBlockBaseCSS)
	case "DataModel":
		return renderDataModel(nd.attrs)
	case "Code":
		return renderCode(nd.attrs)
	case "AnnotatedCode":
		return renderAnnotatedCode(nd.attrs)
	case "Diff":
		return renderDiff(nd.attrs)
	case "FileTree":
		return renderFileTree(nd.attrs)
	case "Json":
		return renderJSON(nd.attrs)
	case "Checklist":
		return renderChecklist(nd.attrs)
	case "Endpoint":
		return renderEndpoint(nd)
	case "TabsBlock", "Tabs", "CodeTabs":
		return renderTabs(nd)
	case "QuestionForm", "VisualQuestions":
		return renderQuestionForm(nd.attrs)
	case "WireframeBlock":
		return renderWireframe(nd)
	case "Mermaid":
		return renderMermaid(nd.attrs)
	default:
		return renderFallback(nd)
	}
}

// objFromAttrs adapts an attribute map into a jsObject so a shadow figure can
// read html/css/caption uniformly whether they arrive as a `data={{…}}` prop
// (Diagram) or as top-level props (HtmlBlock).
func objFromAttrs(attrs map[string]any) *jsObject {
	o := &jsObject{m: map[string]any{}}
	for _, k := range []string{"html", "css", "caption"} {
		if v, ok := attrs[k]; ok {
			o.set(k, v)
		}
	}
	return o
}

// ---- Callout / Columns ---------------------------------------------------

var calloutIcons = map[string]string{
	"info":     `<svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><path d="M12 16v-4M12 8h.01"/></svg>`,
	"warning":  `<svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M10.29 3.86 1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"/><path d="M12 9v4M12 17h.01"/></svg>`,
	"risk":     `<svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M7.86 2h8.28L22 7.86v8.28L16.14 22H7.86L2 16.14V7.86L7.86 2z"/><path d="M12 8v4M12 16h.01"/></svg>`,
	"success":  `<svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><path d="m9 12 2 2 4-4"/></svg>`,
	"decision": `<svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="6" y1="3" x2="6" y2="15"/><circle cx="18" cy="6" r="3"/><circle cx="6" cy="18" r="3"/><path d="M18 9a9 9 0 0 1-9 9"/></svg>`,
}

func renderCallout(tone, title, body string) string {
	if _, ok := calloutIcons[tone]; !ok {
		tone = "info"
	}
	var b strings.Builder
	b.WriteString(`<div class="arc-mdx-callout arc-mdx-callout--` + tone + `">`)
	b.WriteString(`<span class="arc-mdx-callout-icon">` + calloutIcons[tone] + `</span>`)
	b.WriteString(`<div class="arc-mdx-callout-body">`)
	if title != "" {
		b.WriteString(`<div class="arc-mdx-callout-title">` + esc(title) + `</div>`)
	}
	b.WriteString(body + `</div></div>`)
	return b.String()
}

func renderColumn(nd mdxNode) string {
	var b strings.Builder
	b.WriteString(`<div class="arc-mdx-column">`)
	if label := attrStr(nd.attrs, "label"); label != "" {
		b.WriteString(`<div class="arc-mdx-column-label">` + esc(label) + `</div>`)
	}
	b.WriteString(`<div class="arc-mdx-column-body">` + renderMDXFragment(nd.children) + `</div></div>`)
	return b.String()
}

// ---- Table ---------------------------------------------------------------

func renderTable(attrs map[string]any) string {
	cols := asArr(attrs["columns"])
	aligns := asArr(attrs["align"])
	var rows [][]any
	for _, r := range asArr(attrs["rows"]) {
		if a, ok := r.([]any); ok {
			rows = append(rows, a)
		}
	}
	if len(cols) == 0 && len(rows) == 0 {
		return `<div class="arc-mdx-table-empty">(no table data)</div>`
	}
	colCount := len(cols)
	for _, r := range rows {
		if len(r) > colCount {
			colCount = len(r)
		}
	}
	alignOf := func(c int) string {
		if c < len(aligns) {
			switch a := asText(aligns[c]); a {
			case "left", "right", "center":
				return a
			}
		}
		if len(rows) > 0 {
			all, any := true, false
			for _, r := range rows {
				if c >= len(r) || r[c] == nil {
					continue
				}
				if looksNumeric(r[c]) {
					any = true
				} else {
					all = false
					break
				}
			}
			if all && any {
				return "right"
			}
		}
		return "left"
	}
	compact := ""
	if attrStr(attrs, "density") == "compact" {
		compact = " arc-mdx-table--compact"
	}
	var b strings.Builder
	b.WriteString(`<div class="arc-mdx-table-wrap"><table class="arc-mdx-table` + compact + `">`)
	if len(cols) > 0 {
		b.WriteString("<thead><tr>")
		for c := 0; c < colCount; c++ {
			h := ""
			if c < len(cols) {
				h = asText(cols[c])
			}
			b.WriteString(`<th style="text-align:` + alignOf(c) + `">` + esc(h) + `</th>`)
		}
		b.WriteString("</tr></thead>")
	}
	b.WriteString("<tbody>")
	for _, r := range rows {
		b.WriteString("<tr>")
		for c := 0; c < colCount; c++ {
			v := ""
			if c < len(r) {
				v = cellText(r[c])
			}
			b.WriteString(`<td style="text-align:` + alignOf(c) + `">` + esc(v) + `</td>`)
		}
		b.WriteString("</tr>")
	}
	b.WriteString("</tbody></table></div>")
	return b.String()
}

func looksNumeric(v any) bool {
	switch x := v.(type) {
	case float64:
		return true
	case string:
		if strings.TrimSpace(x) == "" {
			return false
		}
		_, err := strconv.ParseFloat(strings.TrimSpace(x), 64)
		return err == nil
	}
	return false
}

func cellText(v any) string {
	if s := asText(v); s != "" || v == nil {
		return s
	}
	return "" // objects/arrays in table cells are not expected; render empty
}

// ---- Shadow figures (Diagram / HtmlBlock) --------------------------------

// renderShadowFigure emits a framed block whose author HTML+CSS is handed to
// web/mdx.js to render inside a shadow root (scoped, script-free). base is the
// renderer-owned stylesheet injected ahead of author CSS.
func renderShadowFigure(kind string, data *jsObject, base string) string {
	if data == nil {
		return ""
	}
	htmlText := asText(data.get("html"))
	cssText := asText(data.get("css"))
	caption := asText(data.get("caption"))
	if htmlText == "" && cssText == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString(`<div class="arc-mdx-` + kind + `">`)
	b.WriteString(`<div class="arc-mdx-` + kind + `-frame" data-shadow-html="` + b64(htmlText) +
		`" data-shadow-css="` + b64(base+cssText) + `"></div>`)
	if caption != "" {
		b.WriteString(`<div class="arc-mdx-` + kind + `-caption">` + esc(caption) + `</div>`)
	}
	b.WriteString(`</div>`)
	return b.String()
}

// ---- Code ----------------------------------------------------------------

func renderCode(attrs map[string]any) string {
	var b strings.Builder
	b.WriteString(`<figure class="arc-mdx-code">`)
	if fn := attrStr(attrs, "filename"); fn != "" {
		b.WriteString(`<div class="arc-mdx-code-head">` + esc(fn) + `</div>`)
	}
	b.WriteString(`<div class="arc-mdx-code-body"><pre><code>` + esc(attrStr(attrs, "code")) + `</code></pre></div>`)
	if cap := attrStr(attrs, "caption"); cap != "" {
		b.WriteString(`<figcaption class="arc-mdx-code-cap">` + esc(cap) + `</figcaption>`)
	}
	b.WriteString(`</figure>`)
	return b.String()
}

// ---- AnnotatedCode -------------------------------------------------------

func renderAnnotatedCode(attrs map[string]any) string {
	code := attrStr(attrs, "code")
	anns := asArr(attrs["annotations"])
	var specs []string
	for _, a := range anns {
		if o := asObj(a); o != nil {
			specs = append(specs, asText(o.get("lines")))
		}
	}
	var b strings.Builder
	b.WriteString(`<figure class="arc-mdx-acode">`)
	if fn := attrStr(attrs, "filename"); fn != "" {
		b.WriteString(`<div class="arc-mdx-acode-head">` + esc(fn) + `</div>`)
	}
	b.WriteString(`<div class="arc-mdx-acode-body">`)
	for i, line := range strings.Split(code, "\n") {
		lineNo := i + 1
		cls := "arc-mdx-acode-line"
		if lineInAnySpec(lineNo, specs) {
			cls += " arc-mdx-acode-line--hl"
		}
		content := esc(line)
		if content == "" {
			content = " "
		}
		b.WriteString(`<div class="` + cls + `"><span class="arc-mdx-acode-gutter">` +
			strconv.Itoa(lineNo) + `</span><span class="arc-mdx-acode-code">` + content + `</span></div>`)
	}
	b.WriteString(`</div>`)

	var notes strings.Builder
	for _, a := range anns {
		o := asObj(a)
		if o == nil {
			continue
		}
		note, label := asText(o.get("note")), asText(o.get("label"))
		if note == "" && label == "" {
			continue
		}
		notes.WriteString(`<div class="arc-mdx-acode-note">`)
		if rl := rangeLabel(asText(o.get("lines"))); rl != "" {
			notes.WriteString(`<span class="arc-mdx-acode-note-range">` + esc(rl) + `</span>`)
		}
		notes.WriteString(`<span class="arc-mdx-acode-note-text">`)
		if label != "" {
			notes.WriteString(`<span class="arc-mdx-acode-note-label">` + esc(label) + `</span>`)
		}
		notes.WriteString(`<span class="arc-mdx-acode-note-body">` + esc(note) + `</span></span></div>`)
	}
	if notes.Len() > 0 {
		b.WriteString(`<div class="arc-mdx-acode-notes">` + notes.String() + `</div>`)
	}
	b.WriteString(`</figure>`)
	return b.String()
}

func parseLineRange(spec string) (int, int, bool) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return 0, 0, false
	}
	if i := strings.IndexByte(spec, '-'); i > 0 {
		a, e1 := strconv.Atoi(strings.TrimSpace(spec[:i]))
		b, e2 := strconv.Atoi(strings.TrimSpace(spec[i+1:]))
		if e1 != nil || e2 != nil {
			return 0, 0, false
		}
		if b < a {
			a, b = b, a
		}
		return a, b, true
	}
	a, err := strconv.Atoi(spec)
	if err != nil {
		return 0, 0, false
	}
	return a, a, true
}

func lineInAnySpec(n int, specs []string) bool {
	for _, s := range specs {
		if a, b, ok := parseLineRange(s); ok && n >= a && n <= b {
			return true
		}
	}
	return false
}

func rangeLabel(spec string) string {
	a, b, ok := parseLineRange(spec)
	if !ok {
		return ""
	}
	if a == b {
		return "L" + strconv.Itoa(a)
	}
	return "L" + strconv.Itoa(a) + "–" + strconv.Itoa(b)
}

// ---- Diff ----------------------------------------------------------------

func renderDiff(attrs map[string]any) string {
	before := strings.Split(attrStr(attrs, "before"), "\n")
	after := strings.Split(attrStr(attrs, "after"), "\n")
	split := attrStr(attrs, "mode") == "split"
	rows := diffRows(before, after)

	var b strings.Builder
	b.WriteString(`<figure class="arc-mdx-diff">`)
	if fn := attrStr(attrs, "filename"); fn != "" {
		b.WriteString(`<div class="arc-mdx-diff-head">` + esc(fn) + `</div>`)
	}
	b.WriteString(`<div class="arc-mdx-diff-body">`)
	if split {
		b.WriteString(diffSplit(rows))
	} else {
		for _, r := range rows {
			b.WriteString(diffRowHTML(r, r.num()))
		}
	}
	b.WriteString(`</div>`)

	var notes strings.Builder
	for _, a := range asArr(attrs["annotations"]) {
		o := asObj(a)
		if o == nil {
			continue
		}
		note, label := asText(o.get("note")), asText(o.get("label"))
		if note == "" && label == "" {
			continue
		}
		side, lines := asText(o.get("side")), asText(o.get("lines"))
		tag := strings.TrimSpace(side)
		if lines != "" {
			tag = strings.TrimSpace(tag + " L" + lines)
		}
		notes.WriteString(`<div class="arc-mdx-diff-note">`)
		if tag != "" {
			notes.WriteString(`<span class="arc-mdx-diff-note-tag">` + esc(tag) + `</span>`)
		}
		notes.WriteString(`<span>`)
		if label != "" {
			notes.WriteString(`<span class="arc-mdx-diff-note-label">` + esc(label) + `</span>`)
		}
		notes.WriteString(`<span class="arc-mdx-diff-note-body">` + esc(note) + `</span></span></div>`)
	}
	if notes.Len() > 0 {
		b.WriteString(`<div class="arc-mdx-diff-notes">` + notes.String() + `</div>`)
	}
	b.WriteString(`</figure>`)
	return b.String()
}

type diffRow struct {
	op                string // "context" | "add" | "del"
	beforeNo, afterNo int    // 0 = absent
	text              string
}

func (r diffRow) num() int {
	if r.op == "del" {
		return r.beforeNo
	}
	return r.afterNo
}

// diffRows computes a line-level diff via a longest-common-subsequence walk.
func diffRows(before, after []string) []diffRow {
	n, m := len(before), len(after)
	lcs := make([][]int, n+1)
	for i := range lcs {
		lcs[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if before[i] == after[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else if lcs[i+1][j] >= lcs[i][j+1] {
				lcs[i][j] = lcs[i+1][j]
			} else {
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}
	var rows []diffRow
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case before[i] == after[j]:
			rows = append(rows, diffRow{"context", i + 1, j + 1, before[i]})
			i, j = i+1, j+1
		case lcs[i+1][j] >= lcs[i][j+1]:
			rows = append(rows, diffRow{"del", i + 1, 0, before[i]})
			i++
		default:
			rows = append(rows, diffRow{"add", 0, j + 1, after[j]})
			j++
		}
	}
	for ; i < n; i++ {
		rows = append(rows, diffRow{"del", i + 1, 0, before[i]})
	}
	for ; j < m; j++ {
		rows = append(rows, diffRow{"add", 0, j + 1, after[j]})
	}
	return rows
}

func diffRowHTML(r diffRow, no int) string {
	sign, cls := " ", ""
	switch r.op {
	case "add":
		sign, cls = "+", " arc-mdx-diff-row--add"
	case "del":
		sign, cls = "-", " arc-mdx-diff-row--del"
	}
	num := ""
	if no > 0 {
		num = strconv.Itoa(no)
	}
	code := esc(r.text)
	if code == "" {
		code = " "
	}
	return `<div class="arc-mdx-diff-row` + cls + `"><span class="arc-mdx-diff-gutter">` + num +
		`</span><span class="arc-mdx-diff-sign">` + sign + `</span><span class="arc-mdx-diff-code">` + code + `</span></div>`
}

func diffSplit(rows []diffRow) string {
	type pair struct{ left, right *diffRow }
	var pairs []pair
	for k := 0; k < len(rows); {
		r := rows[k]
		switch r.op {
		case "context":
			rr := r
			pairs = append(pairs, pair{&rr, &rr})
			k++
		case "del":
			if k+1 < len(rows) && rows[k+1].op == "add" {
				l, rt := rows[k], rows[k+1]
				pairs = append(pairs, pair{&l, &rt})
				k += 2
			} else {
				l := rows[k]
				pairs = append(pairs, pair{&l, nil})
				k++
			}
		default:
			rt := rows[k]
			pairs = append(pairs, pair{nil, &rt})
			k++
		}
	}
	empty := `<div class="arc-mdx-diff-row arc-mdx-diff-row--empty">&nbsp;</div>`
	var left, right strings.Builder
	for _, p := range pairs {
		if p.left != nil {
			left.WriteString(diffRowHTML(*p.left, p.left.beforeNo))
		} else {
			left.WriteString(empty)
		}
		if p.right != nil {
			right.WriteString(diffRowHTML(*p.right, p.right.afterNo))
		} else {
			right.WriteString(empty)
		}
	}
	return `<div class="arc-mdx-diff-split"><div class="arc-mdx-diff-split-col">` + left.String() +
		`</div><div class="arc-mdx-diff-split-col">` + right.String() + `</div></div>`
}

// ---- DataModel -----------------------------------------------------------

func renderDataModel(attrs map[string]any) string {
	ents := asArr(attrs["entities"])
	rels := asArr(attrs["relations"])
	if len(ents) == 0 && len(rels) == 0 {
		return `<div class="arc-mdx-datamodel"><div class="arc-mdx-datamodel-empty">(no entities)</div></div>`
	}
	var b strings.Builder
	b.WriteString(`<div class="arc-mdx-datamodel"><div class="arc-mdx-datamodel-grid">`)
	for _, e := range ents {
		ent := asObj(e)
		name := "(entity)"
		if ent != nil {
			if s := asText(ent.get("name")); s != "" {
				name = s
			} else if s := asText(ent.get("id")); s != "" {
				name = s
			}
		}
		b.WriteString(`<div class="arc-mdx-datamodel-entity"><div class="arc-mdx-datamodel-entity-name">` + esc(name) + `</div><div class="arc-mdx-datamodel-fields">`)
		fields := asArr(ent.get("fields"))
		if len(fields) == 0 {
			b.WriteString(`<div class="arc-mdx-datamodel-field"><span class="arc-mdx-datamodel-empty">(no fields)</span></div>`)
		}
		for _, f := range fields {
			fo := asObj(f)
			if fo == nil {
				continue
			}
			b.WriteString(`<div class="arc-mdx-datamodel-field">`)
			fName := asText(fo.get("name"))
			if fName == "" {
				fName = "(field)"
			}
			b.WriteString(`<span class="arc-mdx-datamodel-field-name">` + esc(fName) + `</span>`)
			if truthy(fo.get("pk")) {
				b.WriteString(`<span class="arc-mdx-datamodel-badge arc-mdx-datamodel-badge--pk">PK</span>`)
			}
			if fk := asText(fo.get("fk")); fk != "" {
				b.WriteString(`<span class="arc-mdx-datamodel-badge arc-mdx-datamodel-badge--fk">` + esc(fk) + `</span>`)
			}
			if truthy(fo.get("nullable")) {
				b.WriteString(`<span class="arc-mdx-datamodel-nullable">nullable</span>`)
			}
			if ft := asText(fo.get("type")); ft != "" {
				b.WriteString(`<span class="arc-mdx-datamodel-field-type">` + esc(ft) + `</span>`)
			}
			b.WriteString(`</div>`)
		}
		b.WriteString(`</div></div>`)
	}
	b.WriteString(`</div>`)
	if len(rels) > 0 {
		b.WriteString(`<div class="arc-mdx-datamodel-relations"><div class="arc-mdx-datamodel-relations-title">Relations</div>`)
		for _, r := range rels {
			ro := asObj(r)
			if ro == nil {
				continue
			}
			from, to := asText(ro.get("from")), asText(ro.get("to"))
			if from == "" {
				from = "?"
			}
			if to == "" {
				to = "?"
			}
			b.WriteString(`<div class="arc-mdx-datamodel-relation">` + esc(from) +
				` <span class="arc-mdx-datamodel-relation-arrow">→</span> ` + esc(to))
			if kind := asText(ro.get("kind")); kind != "" {
				b.WriteString(`<span class="arc-mdx-datamodel-relation-kind"> (` + esc(kind) + `)</span>`)
			}
			b.WriteString(`</div>`)
		}
		b.WriteString(`</div>`)
	}
	b.WriteString(`</div>`)
	return b.String()
}

// ---- FileTree ------------------------------------------------------------

type fileNode struct {
	name     string
	order    []string
	children map[string]*fileNode
	isFile   bool
	change   string
	note     string
}

func newFileNode(name string) *fileNode {
	return &fileNode{name: name, children: map[string]*fileNode{}}
}

var fileChangeClass = map[string]string{
	"added": "success", "modified": "info", "removed": "risk", "renamed": "warning",
}

func renderFileTree(attrs map[string]any) string {
	root := newFileNode("")
	for _, e := range asArr(attrs["entries"]) {
		eo := asObj(e)
		if eo == nil {
			continue
		}
		path := strings.TrimSpace(asText(eo.get("path")))
		if path == "" {
			continue
		}
		cur := root
		segs := strings.Split(path, "/")
		for idx, seg := range segs {
			if seg == "" {
				continue
			}
			child := cur.children[seg]
			if child == nil {
				child = newFileNode(seg)
				cur.children[seg] = child
				cur.order = append(cur.order, seg)
			}
			if idx == len(segs)-1 {
				child.isFile = true
				if _, ok := fileChangeClass[asText(eo.get("change"))]; ok {
					child.change = asText(eo.get("change"))
				}
				child.note = asText(eo.get("note"))
			}
			cur = child
		}
	}
	var rows strings.Builder
	var walk func(*fileNode, int)
	walk = func(node *fileNode, depth int) {
		var dirs, files []string
		for _, k := range node.order {
			if len(node.children[k].children) > 0 {
				dirs = append(dirs, k)
			} else {
				files = append(files, k)
			}
		}
		for _, k := range append(dirs, files...) {
			c := node.children[k]
			isFolder := len(c.children) > 0
			cls := "arc-mdx-filetree-row"
			if isFolder {
				cls += " arc-mdx-filetree-row--folder"
			}
			rows.WriteString(`<div class="` + cls + `" style="padding-left:` + strconv.Itoa(12+depth*16) + `px">`)
			rows.WriteString(`<span class="arc-mdx-filetree-icon">` + fileGlyph(isFolder) + `</span>`)
			rows.WriteString(`<span class="arc-mdx-filetree-name">` + esc(c.name) + `</span>`)
			if c.change != "" {
				rows.WriteString(`<span class="arc-mdx-filetree-tag arc-mdx-filetree-tag--` + fileChangeClass[c.change] + `">` + esc(c.change) + `</span>`)
			}
			if c.note != "" {
				rows.WriteString(`<span class="arc-mdx-filetree-note">— ` + esc(c.note) + `</span>`)
			}
			rows.WriteString(`</div>`)
			if isFolder {
				walk(c, depth+1)
			}
		}
	}
	walk(root, 0)

	var b strings.Builder
	b.WriteString(`<div class="arc-mdx-filetree">`)
	if title := attrStr(attrs, "title"); title != "" {
		b.WriteString(`<div class="arc-mdx-filetree-title">` + esc(title) + `</div>`)
	}
	if rows.Len() > 0 {
		b.WriteString(`<div class="arc-mdx-filetree-rows">` + rows.String() + `</div>`)
	} else {
		b.WriteString(`<div class="arc-mdx-filetree-empty">(no files)</div>`)
	}
	b.WriteString(`</div>`)
	return b.String()
}

func fileGlyph(folder bool) string {
	if folder {
		return `<svg viewBox="0 0 24 24" width="13" height="13" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M4 20h16a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.93a2 2 0 0 1-1.66-.9l-.82-1.2A2 2 0 0 0 7.93 3H4a2 2 0 0 0-2 2v13c0 1.1.9 2 2 2z"/></svg>`
	}
	return `<svg viewBox="0 0 24 24" width="13" height="13" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><path d="M14 2v6h6"/></svg>`
}

// ---- Checklist -----------------------------------------------------------

func renderChecklist(attrs map[string]any) string {
	items := asArr(attrs["items"])
	if len(items) == 0 {
		return `<ul class="arc-mdx-checklist"><li class="arc-mdx-checklist-empty">(no checklist items)</li></ul>`
	}
	var b strings.Builder
	b.WriteString(`<ul class="arc-mdx-checklist">`)
	for _, it := range items {
		o := asObj(it)
		if o == nil {
			continue
		}
		checked := truthy(o.get("checked"))
		box, labelCls := "arc-mdx-checklist-box", "arc-mdx-checklist-label"
		mark := ""
		if checked {
			box += " arc-mdx-checklist-box--checked"
			labelCls += " arc-mdx-checklist-label--checked"
			mark = `<svg viewBox="0 0 24 24" width="12" height="12" fill="none" stroke="currentColor" stroke-width="3" stroke-linecap="round" stroke-linejoin="round"><path d="M20 6 9 17l-5-5"/></svg>`
		}
		label := asText(o.get("label"))
		if label == "" {
			label = "(untitled)"
		}
		b.WriteString(`<li class="arc-mdx-checklist-item"><span class="` + box + `">` + mark + `</span><div class="arc-mdx-checklist-body"><div class="` + labelCls + `">` + esc(label) + `</div>`)
		if note := asText(o.get("note")); note != "" {
			b.WriteString(`<div class="arc-mdx-checklist-note">` + esc(note) + `</div>`)
		}
		b.WriteString(`</div></li>`)
	}
	b.WriteString(`</ul>`)
	return b.String()
}

// ---- Endpoint ------------------------------------------------------------

func renderEndpoint(nd mdxNode) string {
	attrs := nd.attrs
	method := strings.ToUpper(attrStr(attrs, "method"))
	if method == "" {
		method = "GET"
	}
	path := attrStr(attrs, "path")
	if path == "" {
		path = "(no path)"
	}
	deprecated := truthy(attrs["deprecated"])
	children := strings.TrimSpace(nd.children)

	var body strings.Builder
	if desc := attrStr(attrs, "description"); desc != "" {
		body.WriteString(`<p class="arc-mdx-endpoint-desc">` + esc(desc) + `</p>`)
	}
	if children != "" {
		body.WriteString(renderMDXFragment(children))
	}
	if auth := attrStr(attrs, "auth"); auth != "" {
		body.WriteString(`<div class="arc-mdx-endpoint-auth">Auth: <code>` + esc(auth) + `</code></div>`)
	}
	body.WriteString(endpointParams(asArr(attrs["params"])))
	if req := asObj(attrs["request"]); req != nil {
		if ex := prettyJSON(req.get("example")); ex != "" {
			title := "Request"
			if ct := asText(req.get("contentType")); ct != "" {
				title += " (" + ct + ")"
			}
			body.WriteString(`<div class="arc-mdx-endpoint-section-title">` + esc(title) + `</div><pre class="arc-mdx-endpoint-pre">` + esc(ex) + `</pre>`)
		}
	}
	body.WriteString(endpointResponses(asArr(attrs["responses"])))

	var b strings.Builder
	b.WriteString(`<div class="arc-mdx-endpoint"><div class="arc-mdx-endpoint-head">`)
	b.WriteString(`<span class="arc-mdx-endpoint-method arc-mdx-method--` + methodClass(method) + `">` + esc(method) + `</span>`)
	pcls := "arc-mdx-endpoint-path"
	if deprecated {
		pcls += " arc-mdx-endpoint-path--deprecated"
	}
	b.WriteString(`<span class="` + pcls + `">` + esc(path) + `</span>`)
	if s := attrStr(attrs, "summary"); s != "" {
		b.WriteString(`<span class="arc-mdx-endpoint-summary">` + esc(s) + `</span>`)
	}
	if deprecated {
		b.WriteString(`<span class="arc-mdx-endpoint-deprecated-tag">deprecated</span>`)
	}
	b.WriteString(`</div>`)
	if body.Len() > 0 {
		b.WriteString(`<div class="arc-mdx-endpoint-body">` + body.String() + `</div>`)
	}
	b.WriteString(`</div>`)
	return b.String()
}

func methodClass(m string) string {
	switch m {
	case "GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS":
		return strings.ToLower(m)
	}
	return "other"
}

func endpointParams(params []any) string {
	if len(params) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(`<div class="arc-mdx-endpoint-section-title">Parameters</div><table class="arc-mdx-endpoint-table"><thead><tr><th>Name</th><th>In</th><th>Type</th><th>Description</th></tr></thead><tbody>`)
	for _, p := range params {
		o := asObj(p)
		if o == nil {
			continue
		}
		name := asText(o.get("name"))
		if name == "" {
			name = "(param)"
		}
		req := ""
		if truthy(o.get("required")) {
			req = `<span class="arc-mdx-endpoint-param-req"> *</span>`
		}
		b.WriteString(`<tr><td><span class="arc-mdx-endpoint-param-name">` + esc(name) + `</span>` + req +
			`</td><td>` + esc(asText(o.get("in"))) + `</td><td>` + esc(asText(o.get("type"))) +
			`</td><td>` + esc(asText(o.get("description"))) + `</td></tr>`)
	}
	b.WriteString(`</tbody></table>`)
	return b.String()
}

func endpointResponses(responses []any) string {
	if len(responses) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(`<div class="arc-mdx-endpoint-section-title">Responses</div>`)
	for _, r := range responses {
		o := asObj(r)
		if o == nil {
			continue
		}
		status := asText(o.get("status"))
		if status == "" {
			status = "—"
		}
		b.WriteString(`<div class="arc-mdx-endpoint-response"><div class="arc-mdx-endpoint-response-head"><span class="arc-mdx-endpoint-status arc-mdx-endpoint-status--` +
			statusClass(status) + `">` + esc(status) + `</span>`)
		if desc := asText(o.get("description")); desc != "" {
			b.WriteString(`<span class="arc-mdx-endpoint-response-desc">` + esc(desc) + `</span>`)
		}
		b.WriteString(`</div>`)
		if ex := prettyJSON(o.get("example")); ex != "" {
			b.WriteString(`<pre class="arc-mdx-endpoint-pre">` + esc(ex) + `</pre>`)
		}
		b.WriteString(`</div>`)
	}
	return b.String()
}

func statusClass(status string) string {
	if status == "" {
		return "x"
	}
	switch status[0] {
	case '2', '3', '4', '5':
		return string(status[0])
	}
	return "x"
}

// prettyJSON pretty-prints a JSON string example (or a structured value),
// falling back to the raw text if it does not parse.
func prettyJSON(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		s := strings.TrimSpace(x)
		if s == "" {
			return ""
		}
		var parsed any
		if err := jsonUnmarshal(s, &parsed); err == nil {
			return jsonIndent(parsed)
		}
		return x
	default:
		return jsonIndent(jsValueToGo(v))
	}
}

// ---- Json ----------------------------------------------------------------

func renderJSON(attrs map[string]any) string {
	title := attrStr(attrs, "title")
	raw := attrStr(attrs, "json")
	var b strings.Builder
	b.WriteString(`<div class="arc-mdx-json">`)
	if title != "" {
		b.WriteString(`<div class="arc-mdx-json-title">` + esc(title) + `</div>`)
	}
	var parsed any
	if err := jsonUnmarshal(strings.TrimSpace(raw), &parsed); err == nil {
		b.WriteString(`<pre class="arc-mdx-json-raw">` + esc(jsonIndent(parsed)) + `</pre>`)
	} else if raw != "" {
		b.WriteString(`<pre class="arc-mdx-json-raw">` + esc(raw) + `</pre>`)
	} else {
		b.WriteString(`<pre class="arc-mdx-json-raw">(no JSON)</pre>`)
	}
	b.WriteString(`</div>`)
	return b.String()
}

// ---- Tabs ----------------------------------------------------------------

func renderTabs(nd mdxNode) string {
	side := false
	switch attrStr(nd.attrs, "orientation") {
	case "vertical", "side":
		side = true
	}

	type tab struct {
		label string
		body  string
	}
	var tabs []tab

	if raw := asArr(nd.attrs["tabs"]); len(raw) > 0 {
		for i, t := range raw {
			o := asObj(t)
			label := ""
			var body strings.Builder
			if o != nil {
				label = asText(o.get("label"))
				for _, blk := range asArr(o.get("blocks")) {
					body.WriteString(renderBlock(asObj(blk)))
				}
			}
			if label == "" {
				label = "Tab " + strconv.Itoa(i+1)
			}
			tabs = append(tabs, tab{label, body.String()})
		}
	} else {
		// <Tabs><Tab label="…">…</Tab></Tabs> form.
		for _, child := range scanMDX(dedent(nd.children)) {
			if child.isElem && child.name == "Tab" {
				label := attrStr(child.attrs, "label")
				if label == "" {
					label = "Tab " + strconv.Itoa(len(tabs)+1)
				}
				tabs = append(tabs, tab{label, renderMDXFragment(child.children)})
			}
		}
	}

	if len(tabs) == 0 {
		return `<div class="arc-mdx-tabs"><div class="arc-mdx-tabs-empty">(no tabs)</div></div>`
	}

	group := nextID("tab")
	cls := "arc-mdx-tabs"
	if side {
		cls += " arc-mdx-tabs--side"
	}
	var radios, strip, panels, style strings.Builder
	for i, t := range tabs {
		rid := group + "-" + strconv.Itoa(i)
		checked := ""
		if i == 0 {
			checked = " checked"
		}
		radios.WriteString(`<input type="radio" name="` + group + `" id="` + rid + `" class="arc-mdx-tabs-radio"` + checked + `>`)
		strip.WriteString(`<label class="arc-mdx-tabs-tab" for="` + rid + `">` + esc(t.label) + `</label>`)
		panels.WriteString(`<div class="arc-mdx-tabs-panel" id="` + rid + `-p">` + t.body + `</div>`)
		style.WriteString(`#` + rid + `:checked~.arc-mdx-tabs-panels>#` + rid + `-p{display:block}`)
		style.WriteString(`#` + rid + `:checked~.arc-mdx-tabs-strip>label[for="` + rid + `"]{color:var(--mdx-accent);font-weight:600;border-color:var(--mdx-accent)}`)
	}
	return `<div class="` + cls + `"><style>` + style.String() + `</style>` + radios.String() +
		`<div class="arc-mdx-tabs-strip" role="tablist">` + strip.String() +
		`</div><div class="arc-mdx-tabs-panels">` + panels.String() + `</div></div>`
}

// renderBlock renders one block from a tab's `blocks` array (a {type,data}
// object) by delegating to the matching component renderer.
func renderBlock(o *jsObject) string {
	if o == nil {
		return ""
	}
	data := asObj(o.get("data"))
	dm := jsObjToAttrs(data)
	switch asText(o.get("type")) {
	case "rich-text":
		return renderMarkdown(asText(data.get("markdown")))
	case "annotated-code":
		return renderAnnotatedCode(dm)
	case "code":
		return renderCode(dm)
	case "diagram":
		return renderShadowFigure("diagram", data, diagramBaseCSS)
	case "custom-html":
		return renderShadowFigure("htmlblock", data, htmlBlockBaseCSS)
	case "table":
		return renderTable(dm)
	case "diff":
		return renderDiff(dm)
	case "file-tree":
		return renderFileTree(dm)
	case "data-model":
		return renderDataModel(dm)
	case "checklist":
		return renderChecklist(dm)
	case "json-explorer":
		return renderJSON(dm)
	case "callout":
		return renderCallout(asText(data.get("tone")), asText(data.get("title")), renderMarkdown(asText(data.get("body"))))
	default:
		return `<div class="arc-mdx-fallback"><span class="arc-mdx-fallback-tag">` + esc(asText(o.get("type"))) + `</span></div>`
	}
}

func jsObjToAttrs(o *jsObject) map[string]any {
	m := map[string]any{}
	if o != nil {
		for _, k := range o.keys {
			m[k] = o.m[k]
		}
	}
	return m
}

// ---- QuestionForm --------------------------------------------------------

func renderQuestionForm(attrs map[string]any) string {
	questions := asArr(attrs["questions"])
	var b strings.Builder
	b.WriteString(`<div class="arc-mdx-qform">`)
	for _, q := range questions {
		o := asObj(q)
		if o == nil {
			continue
		}
		mode := asText(o.get("mode"))
		b.WriteString(`<div class="arc-mdx-qform-q">`)
		if title := asText(o.get("title")); title != "" {
			req := ""
			if truthy(o.get("required")) {
				req = `<span class="arc-mdx-qform-req">*</span>`
			}
			b.WriteString(`<div class="arc-mdx-qform-title">` + esc(title) + req + `</div>`)
		}
		if sub := asText(o.get("subtitle")); sub != "" {
			b.WriteString(`<div class="arc-mdx-qform-sub">` + esc(sub) + `</div>`)
		}
		if mode == "freeform" {
			ph := asText(o.get("placeholder"))
			b.WriteString(`<div class="arc-mdx-qform-freeform">` + esc(ph) + `</div>`)
		} else {
			input := "radio"
			if mode == "multi" {
				input = "checkbox"
			}
			b.WriteString(`<div class="arc-mdx-qform-options">`)
			for _, opt := range asArr(o.get("options")) {
				oo := asObj(opt)
				if oo == nil {
					continue
				}
				b.WriteString(`<label class="arc-mdx-qform-opt"><span class="arc-mdx-qform-mark arc-mdx-qform-mark--` + input + `"></span><span class="arc-mdx-qform-opt-body"><span class="arc-mdx-qform-opt-label">` + esc(asText(oo.get("label"))))
				if truthy(oo.get("recommended")) {
					b.WriteString(`<span class="arc-mdx-qform-rec">Recommended</span>`)
				}
				b.WriteString(`</span>`)
				if detail := asText(oo.get("detail")); detail != "" {
					b.WriteString(`<span class="arc-mdx-qform-opt-detail">` + esc(detail) + `</span>`)
				}
				b.WriteString(`</span></label>`)
			}
			b.WriteString(`</div>`)
		}
		b.WriteString(`</div>`)
	}
	submit := attrStr(attrs, "submitLabel")
	if submit == "" {
		submit = "Submit"
	}
	b.WriteString(`<button class="arc-mdx-qform-submit" disabled>` + esc(submit) + `</button></div>`)
	return b.String()
}

// ---- Wireframe -----------------------------------------------------------

func renderWireframe(nd mdxNode) string {
	// A WireframeBlock wraps a <Screen> whose author markup is either an `html`
	// prop or kit-primitive children. We render the html form (what plans use)
	// as a scoped shadow surface; primitive-only screens degrade to a note.
	var screen *mdxNode
	for _, child := range scanMDX(dedent(nd.children)) {
		if child.isElem && child.name == "Screen" {
			c := child
			screen = &c
			break
		}
	}
	if screen == nil {
		return renderFallback(nd)
	}
	surface := attrStr(screen.attrs, "surface")
	if surface == "" {
		surface = "browser"
	}
	caption := attrStr(screen.attrs, "caption")
	htmlText := attrStr(screen.attrs, "html")

	var b strings.Builder
	b.WriteString(`<div class="arc-mdx-wireframe arc-mdx-wireframe--` + esc(surface) + `">`)
	b.WriteString(`<div class="arc-mdx-wireframe-frame">`)
	if htmlText != "" {
		b.WriteString(`<div class="arc-mdx-wireframe-surface" data-shadow-html="` + b64(htmlText) +
			`" data-shadow-css="` + b64(wireframeBaseCSS) + `"></div>`)
	} else {
		b.WriteString(`<div class="arc-mdx-wireframe-note">[wireframe: ` + esc(surface) + `]</div>`)
	}
	b.WriteString(`</div>`)
	if caption != "" {
		b.WriteString(`<div class="arc-mdx-wireframe-caption">` + esc(caption) + `</div>`)
	}
	b.WriteString(`</div>`)
	return b.String()
}

// ---- Mermaid / fallback --------------------------------------------------

func renderMermaid(attrs map[string]any) string {
	src := attrStr(attrs, "source")
	var b strings.Builder
	b.WriteString(`<figure class="arc-mdx-code"><div class="arc-mdx-code-head">mermaid</div><div class="arc-mdx-code-body"><pre><code>` + esc(src) + `</code></pre></div>`)
	if cap := attrStr(attrs, "caption"); cap != "" {
		b.WriteString(`<figcaption class="arc-mdx-code-cap">` + esc(cap) + `</figcaption>`)
	}
	b.WriteString(`</figure>`)
	return b.String()
}

func renderFallback(nd mdxNode) string {
	var b strings.Builder
	b.WriteString(`<div class="arc-mdx-fallback"><span class="arc-mdx-fallback-tag">` + esc(nd.name) + `</span>`)
	if body := strings.TrimSpace(nd.children); body != "" {
		b.WriteString(renderMDXFragment(body))
	}
	b.WriteString(`</div>`)
	return b.String()
}
