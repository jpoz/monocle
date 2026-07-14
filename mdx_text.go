package main

import (
	"strconv"
	"strings"
)

// mdx_text.go flattens MDX into plain Markdown for the terminal reader, which
// has no way to render HTML components. Prose passes through; each component
// becomes a readable Markdown approximation (a table stays a table, code stays
// a fenced block, a diagram becomes its caption) so the TUI never shows raw
// JSX the way a plain Markdown parse would.

// mdxToMarkdown converts MDX source to Markdown suitable for parseMarkdown.
func mdxToMarkdown(src string) string {
	_, rest := stripFrontmatter(src)
	var b strings.Builder
	flattenNodes(&b, rest)
	return b.String()
}

func flattenNodes(b *strings.Builder, src string) {
	for _, nd := range scanMDX(dedent(src)) {
		if !nd.isElem {
			writeBlock(b, strings.TrimSpace(nd.md))
			continue
		}
		flattenElem(b, nd)
	}
}

// writeBlock appends a block of text followed by a blank line separator.
func writeBlock(b *strings.Builder, s string) {
	if strings.TrimSpace(s) == "" {
		return
	}
	b.WriteString(s)
	b.WriteString("\n\n")
}

func flattenChildren(nd mdxNode) string {
	var b strings.Builder
	flattenNodes(&b, nd.children)
	return strings.TrimRight(b.String(), "\n")
}

func flattenElem(b *strings.Builder, nd mdxNode) {
	switch nd.name {
	case "RichText":
		flattenNodes(b, nd.children)
	case "Callout":
		body := flattenChildren(nd)
		if tone := attrStr(nd.attrs, "tone"); tone != "" {
			body = "**[" + tone + "]** " + strings.TrimLeft(body, " ")
		}
		writeBlock(b, blockquote(body))
	case "Columns":
		flattenNodes(b, nd.children)
	case "Column":
		if label := attrStr(nd.attrs, "label"); label != "" {
			writeBlock(b, "**"+label+"**")
		}
		flattenNodes(b, nd.children)
	case "Table":
		writeBlock(b, flattenTable(nd.attrs))
	case "Diagram", "HtmlBlock":
		writeBlock(b, diagramText(nd))
	case "Code":
		writeBlock(b, fencedCode(attrStr(nd.attrs, "language"), attrStr(nd.attrs, "filename"), attrStr(nd.attrs, "code")))
		if cap := attrStr(nd.attrs, "caption"); cap != "" {
			writeBlock(b, "_"+cap+"_")
		}
	case "AnnotatedCode":
		writeBlock(b, fencedCode(attrStr(nd.attrs, "language"), attrStr(nd.attrs, "filename"), attrStr(nd.attrs, "code")))
		writeBlock(b, annotationList(asArr(nd.attrs["annotations"])))
	case "Diff":
		writeBlock(b, flattenDiff(nd.attrs))
	case "DataModel":
		writeBlock(b, flattenDataModel(nd.attrs))
	case "FileTree":
		writeBlock(b, flattenFileTree(nd.attrs))
	case "Checklist":
		writeBlock(b, flattenChecklist(nd.attrs))
	case "Endpoint":
		flattenEndpoint(b, nd)
	case "TabsBlock", "Tabs", "CodeTabs":
		flattenTabs(b, nd)
	case "QuestionForm", "VisualQuestions":
		flattenQuestionForm(b, nd.attrs)
	case "WireframeBlock":
		writeBlock(b, wireframeText(nd))
	case "Mermaid":
		writeBlock(b, fencedCode("mermaid", "", attrStr(nd.attrs, "source")))
	default:
		if body := flattenChildren(nd); body != "" {
			flattenNodes(b, nd.children)
		}
	}
}

func blockquote(s string) string {
	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		lines[i] = strings.TrimRight("> "+ln, " ")
	}
	return strings.Join(lines, "\n")
}

func fencedCode(lang, filename, code string) string {
	var b strings.Builder
	if filename != "" {
		b.WriteString("`" + filename + "`\n\n")
	}
	b.WriteString("```" + lang + "\n")
	b.WriteString(strings.TrimRight(code, "\n"))
	b.WriteString("\n```")
	return b.String()
}

func flattenTable(attrs map[string]any) string {
	cols := asArr(attrs["columns"])
	var rows [][]any
	for _, r := range asArr(attrs["rows"]) {
		if a, ok := r.([]any); ok {
			rows = append(rows, a)
		}
	}
	colCount := len(cols)
	for _, r := range rows {
		if len(r) > colCount {
			colCount = len(r)
		}
	}
	if colCount == 0 {
		return ""
	}
	cell := func(v any) string {
		return strings.ReplaceAll(cellText(v), "|", "\\|")
	}
	var b strings.Builder
	b.WriteString("|")
	for c := 0; c < colCount; c++ {
		h := ""
		if c < len(cols) {
			h = cell(cols[c])
		}
		b.WriteString(" " + h + " |")
	}
	b.WriteString("\n|")
	for c := 0; c < colCount; c++ {
		b.WriteString(" --- |")
	}
	for _, r := range rows {
		b.WriteString("\n|")
		for c := 0; c < colCount; c++ {
			v := ""
			if c < len(r) {
				v = cell(r[c])
			}
			b.WriteString(" " + v + " |")
		}
	}
	return b.String()
}

func diagramText(nd mdxNode) string {
	data := asObj(nd.attrs["data"])
	if data == nil {
		data = objFromAttrs(nd.attrs)
	}
	if cap := asText(data.get("caption")); cap != "" {
		return "_[diagram] " + cap + "_"
	}
	return "_[diagram]_"
}

func wireframeText(nd mdxNode) string {
	for _, child := range scanMDX(dedent(nd.children)) {
		if child.isElem && child.name == "Screen" {
			if cap := attrStr(child.attrs, "caption"); cap != "" {
				return "_[wireframe] " + cap + "_"
			}
		}
	}
	return "_[wireframe]_"
}

func annotationList(anns []any) string {
	var b strings.Builder
	for _, a := range anns {
		o := asObj(a)
		if o == nil {
			continue
		}
		note, label := asText(o.get("note")), asText(o.get("label"))
		if note == "" && label == "" {
			continue
		}
		b.WriteString("- ")
		if rl := rangeLabel(asText(o.get("lines"))); rl != "" {
			b.WriteString("`" + rl + "` ")
		}
		if label != "" {
			b.WriteString("**" + label + "** ")
		}
		b.WriteString(note + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func flattenDiff(attrs map[string]any) string {
	rows := diffRows(strings.Split(attrStr(attrs, "before"), "\n"), strings.Split(attrStr(attrs, "after"), "\n"))
	var b strings.Builder
	if fn := attrStr(attrs, "filename"); fn != "" {
		b.WriteString("`" + fn + "`\n\n")
	}
	b.WriteString("```diff\n")
	for _, r := range rows {
		switch r.op {
		case "add":
			b.WriteString("+" + r.text + "\n")
		case "del":
			b.WriteString("-" + r.text + "\n")
		default:
			b.WriteString(" " + r.text + "\n")
		}
	}
	b.WriteString("```")
	return b.String()
}

func flattenDataModel(attrs map[string]any) string {
	var b strings.Builder
	for _, e := range asArr(attrs["entities"]) {
		ent := asObj(e)
		if ent == nil {
			continue
		}
		name := asText(ent.get("name"))
		if name == "" {
			name = asText(ent.get("id"))
		}
		b.WriteString("#### " + name + "\n\n")
		for _, f := range asArr(ent.get("fields")) {
			fo := asObj(f)
			if fo == nil {
				continue
			}
			line := "- `" + asText(fo.get("name")) + "`"
			if t := asText(fo.get("type")); t != "" {
				line += " — " + t
			}
			var flags []string
			if truthy(fo.get("pk")) {
				flags = append(flags, "PK")
			}
			if fk := asText(fo.get("fk")); fk != "" {
				flags = append(flags, "FK→"+fk)
			}
			if truthy(fo.get("nullable")) {
				flags = append(flags, "nullable")
			}
			if len(flags) > 0 {
				line += " _(" + strings.Join(flags, ", ") + ")_"
			}
			b.WriteString(line + "\n")
		}
		b.WriteString("\n")
	}
	rels := asArr(attrs["relations"])
	if len(rels) > 0 {
		b.WriteString("**Relations**\n\n")
		for _, r := range rels {
			ro := asObj(r)
			if ro == nil {
				continue
			}
			b.WriteString("- " + asText(ro.get("from")) + " → " + asText(ro.get("to")))
			if k := asText(ro.get("kind")); k != "" {
				b.WriteString(" (" + k + ")")
			}
			b.WriteString("\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func flattenFileTree(attrs map[string]any) string {
	var b strings.Builder
	if title := attrStr(attrs, "title"); title != "" {
		b.WriteString("**" + title + "**\n\n")
	}
	for _, e := range asArr(attrs["entries"]) {
		eo := asObj(e)
		if eo == nil {
			continue
		}
		b.WriteString("- `" + asText(eo.get("path")) + "`")
		if ch := asText(eo.get("change")); ch != "" {
			b.WriteString(" _(" + ch + ")_")
		}
		if note := asText(eo.get("note")); note != "" {
			b.WriteString(" — " + note)
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func flattenChecklist(attrs map[string]any) string {
	var b strings.Builder
	for _, it := range asArr(attrs["items"]) {
		o := asObj(it)
		if o == nil {
			continue
		}
		box := "[ ]"
		if truthy(o.get("checked")) {
			box = "[x]"
		}
		b.WriteString("- " + box + " " + asText(o.get("label")))
		if note := asText(o.get("note")); note != "" {
			b.WriteString(" — " + note)
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func flattenEndpoint(b *strings.Builder, nd mdxNode) {
	method := strings.ToUpper(attrStr(nd.attrs, "method"))
	head := "**" + method + "** `" + attrStr(nd.attrs, "path") + "`"
	if s := attrStr(nd.attrs, "summary"); s != "" {
		head += " — " + s
	}
	writeBlock(b, head)
	if desc := attrStr(nd.attrs, "description"); desc != "" {
		writeBlock(b, desc)
	}
	if children := strings.TrimSpace(nd.children); children != "" {
		flattenNodes(b, children)
	}
}

func flattenTabs(b *strings.Builder, nd mdxNode) {
	if raw := asArr(nd.attrs["tabs"]); len(raw) > 0 {
		for i, t := range raw {
			o := asObj(t)
			label := ""
			if o != nil {
				label = asText(o.get("label"))
			}
			if label == "" {
				label = "Tab " + strconv.Itoa(i+1)
			}
			writeBlock(b, "##### "+label)
			if o != nil {
				for _, blk := range asArr(o.get("blocks")) {
					flattenBlock(b, asObj(blk))
				}
			}
		}
		return
	}
	for _, child := range scanMDX(dedent(nd.children)) {
		if child.isElem && child.name == "Tab" {
			label := attrStr(child.attrs, "label")
			if label != "" {
				writeBlock(b, "##### "+label)
			}
			flattenNodes(b, child.children)
		}
	}
}

func flattenBlock(b *strings.Builder, o *jsObject) {
	if o == nil {
		return
	}
	data := asObj(o.get("data"))
	dm := jsObjToAttrs(data)
	switch asText(o.get("type")) {
	case "rich-text":
		writeBlock(b, asText(data.get("markdown")))
	case "annotated-code":
		writeBlock(b, fencedCode(asText(data.get("language")), asText(data.get("filename")), asText(data.get("code"))))
		writeBlock(b, annotationList(asArr(data.get("annotations"))))
	case "code":
		writeBlock(b, fencedCode(asText(data.get("language")), asText(data.get("filename")), asText(data.get("code"))))
	case "diagram":
		if cap := asText(data.get("caption")); cap != "" {
			writeBlock(b, "_[diagram] "+cap+"_")
		}
	case "table":
		writeBlock(b, flattenTable(dm))
	case "diff":
		writeBlock(b, flattenDiff(dm))
	case "data-model":
		writeBlock(b, flattenDataModel(dm))
	case "file-tree":
		writeBlock(b, flattenFileTree(dm))
	case "checklist":
		writeBlock(b, flattenChecklist(dm))
	case "callout":
		writeBlock(b, blockquote(asText(data.get("body"))))
	}
}

func flattenQuestionForm(b *strings.Builder, attrs map[string]any) {
	for _, q := range asArr(attrs["questions"]) {
		o := asObj(q)
		if o == nil {
			continue
		}
		if title := asText(o.get("title")); title != "" {
			writeBlock(b, "**"+title+"**")
		}
		if sub := asText(o.get("subtitle")); sub != "" {
			writeBlock(b, sub)
		}
		var opts strings.Builder
		for _, opt := range asArr(o.get("options")) {
			oo := asObj(opt)
			if oo == nil {
				continue
			}
			opts.WriteString("- " + asText(oo.get("label")))
			if truthy(oo.get("recommended")) {
				opts.WriteString(" _(recommended)_")
			}
			if detail := asText(oo.get("detail")); detail != "" {
				opts.WriteString(" — " + detail)
			}
			opts.WriteString("\n")
		}
		writeBlock(b, strings.TrimRight(opts.String(), "\n"))
	}
}
