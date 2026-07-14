package main

import (
	"encoding/base64"
	"html"
	"strconv"
	"strings"
	"sync/atomic"
)

// mdx.go renders MDX (Markdown + JSX components) to HTML. MDX files pair
// CommonMark prose with capitalized JSX component tags whose props are JS
// object/array literals — goldmark alone dumps those tags as literal text. We
// parse the JSX structure ourselves, evaluate the literal props, and render
// each known component to the same .arc-mdx-* HTML the visual-plan renderer
// produces; prose regions still flow through goldmark. Nothing here executes
// JavaScript: props are pure data literals and author HTML lands in a scoped
// shadow root (see web/mdx.js), so it can run no scripts and reach nothing.

// isMDXPath reports whether path names an MDX file specifically (a subset of
// isMarkdownPath, which also covers .md/.markdown).
func isMDXPath(path string) bool {
	return strings.EqualFold(pathExt(path), ".mdx")
}

func pathExt(path string) string {
	if i := strings.LastIndexByte(path, '.'); i >= 0 {
		return path[i:]
	}
	return ""
}

// ---- document ------------------------------------------------------------

// mdxDocument is a parsed MDX file ready to render: the body HTML plus the
// title/description lifted from YAML frontmatter (if any).
type mdxDocument struct {
	Title string
	Desc  string
	Body  string
}

// renderMDXDocument parses frontmatter and renders the body to HTML.
func renderMDXDocument(src []byte) mdxDocument {
	fm, rest := stripFrontmatter(string(src))
	desc := fm["brief"]
	if desc == "" {
		desc = fm["description"]
	}
	return mdxDocument{
		Title: fm["title"],
		Desc:  desc,
		Body:  renderMDXFragment(rest),
	}
}

// stripFrontmatter removes a leading `---`-delimited YAML block and returns a
// shallow map of its top-level scalar keys plus the remaining source. Only the
// handful of keys we surface (title/brief/description) need parsing, so this is
// deliberately a line scanner, not a YAML implementation.
func stripFrontmatter(s string) (map[string]string, string) {
	fm := map[string]string{}
	t := strings.TrimPrefix(s, "\ufeff")
	if !strings.HasPrefix(t, "---\n") && !strings.HasPrefix(t, "---\r\n") {
		return fm, s
	}
	_, rest, _ := strings.Cut(t, "\n")
	end := -1
	scan := rest
	off := len(t) - len(rest)
	for {
		i := strings.IndexByte(scan, '\n')
		var line string
		if i < 0 {
			line = scan
		} else {
			line = scan[:i]
		}
		if strings.TrimRight(line, "\r") == "---" {
			end = off + len(line)
			if i >= 0 {
				end++ // consume the trailing newline
			}
			break
		}
		if k, v, ok := splitKeyValue(line); ok {
			fm[k] = v
		}
		if i < 0 {
			break
		}
		off += i + 1
		scan = scan[i+1:]
	}
	if end < 0 {
		return fm, s
	}
	return fm, t[end:]
}

func splitKeyValue(line string) (string, string, bool) {
	line = strings.TrimSpace(strings.TrimRight(line, "\r"))
	i := strings.IndexByte(line, ':')
	if i <= 0 {
		return "", "", false
	}
	k := strings.TrimSpace(line[:i])
	v := strings.TrimSpace(line[i+1:])
	v = strings.Trim(v, `"'`)
	if k == "" {
		return "", "", false
	}
	return k, v, true
}

// ---- node scanner --------------------------------------------------------

// mdxNode is one top-level item in a fragment: either a run of markdown or a
// JSX element (with parsed attributes and, unless self-closing, raw child
// source for recursive rendering).
type mdxNode struct {
	isElem   bool
	md       string
	name     string
	attrs    map[string]any
	children string
	self     bool
}

// scanMDX splits a fragment into ordered markdown/element nodes. Component
// detection fires only on a capitalized tag at the start of a line and outside
// fenced code, so prose, inline `<` and lowercase HTML pass through untouched.
func scanMDX(src string) []mdxNode {
	var nodes []mdxNode
	n := len(src)
	mdStart := 0
	inFence := false
	flush := func(end int) {
		if end > mdStart && strings.TrimSpace(src[mdStart:end]) != "" {
			nodes = append(nodes, mdxNode{md: src[mdStart:end]})
		}
	}
	i := 0
	for i < n {
		if i == 0 || src[i-1] == '\n' {
			t := strings.TrimSpace(lineAt(src, i))
			if strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
				inFence = !inFence
			}
		}
		if !inFence && src[i] == '<' && i+1 < n && isUpperAlpha(src[i+1]) && atLineStart(src, i) {
			if node, next, ok := parseElement(src, i); ok {
				flush(i)
				nodes = append(nodes, node)
				i = next
				mdStart = i
				continue
			}
		}
		i++
	}
	flush(n)
	return nodes
}

func lineAt(src string, i int) string {
	if j := strings.IndexByte(src[i:], '\n'); j >= 0 {
		return src[i : i+j]
	}
	return src[i:]
}

func atLineStart(src string, i int) bool {
	for j := i - 1; j >= 0; j-- {
		switch src[j] {
		case '\n':
			return true
		case ' ', '\t', '\r':
			continue
		default:
			return false
		}
	}
	return true
}

// parseElement reads the element beginning at src[start] ('<'). It returns the
// node and the index just past it (past `/>` or the matching close tag).
func parseElement(src string, start int) (mdxNode, int, bool) {
	tagEnd, self, ok := findTagEnd(src, start)
	if !ok {
		return mdxNode{}, 0, false
	}
	j := start + 1
	for j < len(src) && isAlnum(src[j]) {
		j++
	}
	name := src[start+1 : j]
	if name == "" {
		return mdxNode{}, 0, false
	}
	attrsText := src[j : tagEnd-1] // drop the closing '>'
	if self {
		attrsText = strings.TrimSuffix(strings.TrimSpace(attrsText), "/")
	}
	node := mdxNode{isElem: true, name: name, attrs: parseAttrs(attrsText), self: self}
	if self {
		return node, tagEnd, true
	}
	closeStart, closeEnd, ok := findClose(src, tagEnd, name)
	if !ok {
		node.children = src[tagEnd:]
		return node, len(src), true
	}
	node.children = src[tagEnd:closeStart]
	return node, closeEnd, true
}

// findTagEnd scans an opening tag from src[i]=='<' to its terminating '>',
// skipping string literals and balancing `{…}` expression braces. It reports
// the index past '>' and whether the tag self-closes (`… />`).
func findTagEnd(src string, i int) (int, bool, bool) {
	n := len(src)
	j := i + 1
	for j < n && isAlnum(src[j]) {
		j++
	}
	depth := 0
	var quote byte
	for j < n {
		c := src[j]
		if quote != 0 {
			if c == '\\' {
				j += 2
				continue
			}
			if c == quote {
				quote = 0
			}
			j++
			continue
		}
		switch c {
		case '"', '\'', '`':
			quote = c
		case '{':
			depth++
		case '}':
			if depth > 0 {
				depth--
			}
		case '>':
			if depth == 0 {
				return j + 1, j > i && src[j-1] == '/', true
			}
		}
		j++
	}
	return n, false, false
}

// findClose locates the tag closing an element of the given name, honoring
// same-name nesting. Returns the index of the closing tag's '<' and the index
// just past its '>'.
func findClose(src string, from int, name string) (int, int, bool) {
	depth := 1
	n := len(src)
	i := from
	for i < n {
		if src[i] == '<' {
			if i+1 < n && src[i+1] == '/' && matchName(src, i+2, name) {
				k := i + 2 + len(name)
				for k < n && src[k] != '>' {
					k++
				}
				depth--
				if depth == 0 {
					if k < n {
						k++
					}
					return i, k, true
				}
				i = k
				continue
			}
			if i+1 < n && isUpperAlpha(src[i+1]) && matchName(src, i+1, name) {
				if e, self, ok := findTagEnd(src, i); ok {
					if !self {
						depth++
					}
					i = e
					continue
				}
			}
		}
		i++
	}
	return 0, 0, false
}

func matchName(src string, pos int, name string) bool {
	if pos+len(name) > len(src) || src[pos:pos+len(name)] != name {
		return false
	}
	if next := pos + len(name); next < len(src) && isAlnum(src[next]) {
		return false
	}
	return true
}

// parseAttrs reads `name="str"`, `name={expr}`, and bare boolean attributes
// from an opening tag's attribute text. `{expr}` values are evaluated as JS
// literals; the outer JSX braces are stripped first.
func parseAttrs(s string) map[string]any {
	attrs := map[string]any{}
	n := len(s)
	i := 0
	for i < n {
		for i < n && isSpace(s[i]) {
			i++
		}
		if i >= n {
			break
		}
		start := i
		for i < n && (isAlnum(s[i]) || s[i] == '-' || s[i] == '_') {
			i++
		}
		name := s[start:i]
		if name == "" {
			i++
			continue
		}
		for i < n && isSpace(s[i]) {
			i++
		}
		if i >= n || s[i] != '=' {
			attrs[name] = true
			continue
		}
		i++
		for i < n && isSpace(s[i]) {
			i++
		}
		if i >= n {
			break
		}
		switch s[i] {
		case '"', '\'':
			q := s[i]
			i++
			vs := i
			for i < n && s[i] != q {
				if s[i] == '\\' {
					i++
				}
				i++
			}
			attrs[name] = s[vs:i]
			if i < n {
				i++
			}
		case '{':
			depth := 0
			var quote byte
			vs := i
			for i < n {
				c := s[i]
				if quote != 0 {
					if c == '\\' {
						i += 2
						continue
					}
					if c == quote {
						quote = 0
					}
					i++
					continue
				}
				switch c {
				case '"', '\'', '`':
					quote = c
				case '{':
					depth++
				case '}':
					depth--
				}
				i++
				if depth == 0 {
					break
				}
			}
			val, _ := parseJSValue(s[vs+1 : i-1])
			attrs[name] = val
		}
	}
	return attrs
}

// ---- JS literal parser ---------------------------------------------------

// jsObject is an insertion-ordered object literal. Order matters for rendering
// (entity fields, endpoint params, JSON trees), which a Go map would lose.
type jsObject struct {
	keys []string
	m    map[string]any
}

func (o *jsObject) get(k string) any {
	if o == nil {
		return nil
	}
	return o.m[k]
}

func (o *jsObject) set(k string, v any) {
	if o.m == nil {
		o.m = map[string]any{}
	}
	if _, ok := o.m[k]; !ok {
		o.keys = append(o.keys, k)
	}
	o.m[k] = v
}

type jsParser struct {
	s string
	i int
}

// parseJSValue evaluates a JS data literal (object, array, string, number,
// boolean, null) into Go values: *jsObject, []any, string, float64, bool, nil.
func parseJSValue(s string) (any, error) {
	p := &jsParser{s: s}
	return p.value(), nil
}

func (p *jsParser) ws() {
	for p.i < len(p.s) {
		c := p.s[p.i]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			p.i++
			continue
		}
		if c == '/' && p.i+1 < len(p.s) {
			if p.s[p.i+1] == '/' {
				for p.i < len(p.s) && p.s[p.i] != '\n' {
					p.i++
				}
				continue
			}
			if p.s[p.i+1] == '*' {
				p.i += 2
				for p.i+1 < len(p.s) && !(p.s[p.i] == '*' && p.s[p.i+1] == '/') {
					p.i++
				}
				p.i += 2
				continue
			}
		}
		break
	}
}

func (p *jsParser) value() any {
	p.ws()
	if p.i >= len(p.s) {
		return nil
	}
	switch c := p.s[p.i]; {
	case c == '{':
		return p.object()
	case c == '[':
		return p.array()
	case c == '"' || c == '\'' || c == '`':
		return p.string()
	case c == '-' || c == '+' || c == '.' || (c >= '0' && c <= '9'):
		return p.number()
	default:
		return p.ident()
	}
}

func (p *jsParser) object() any {
	o := &jsObject{m: map[string]any{}}
	p.i++ // '{'
	for {
		p.ws()
		if p.i >= len(p.s) || p.s[p.i] == '}' {
			p.i++
			break
		}
		if p.s[p.i] == ',' {
			p.i++
			continue
		}
		var key string
		if c := p.s[p.i]; c == '"' || c == '\'' || c == '`' {
			key, _ = p.string().(string)
		} else {
			start := p.i
			for p.i < len(p.s) {
				ch := p.s[p.i]
				if isAlnum(ch) || ch == '_' || ch == '$' {
					p.i++
				} else {
					break
				}
			}
			key = p.s[start:p.i]
		}
		p.ws()
		if p.i < len(p.s) && p.s[p.i] == ':' {
			p.i++
		}
		o.set(key, p.value())
	}
	return o
}

func (p *jsParser) array() any {
	arr := []any{}
	p.i++ // '['
	for {
		p.ws()
		if p.i >= len(p.s) || p.s[p.i] == ']' {
			p.i++
			break
		}
		if p.s[p.i] == ',' {
			p.i++
			continue
		}
		arr = append(arr, p.value())
	}
	return arr
}

func (p *jsParser) string() any {
	q := p.s[p.i]
	p.i++
	var b strings.Builder
	for p.i < len(p.s) {
		c := p.s[p.i]
		if c == '\\' && p.i+1 < len(p.s) {
			switch e := p.s[p.i+1]; e {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case 'r':
				b.WriteByte('\r')
			case 'b':
				b.WriteByte('\b')
			case 'f':
				b.WriteByte('\f')
			case 'u':
				if p.i+6 <= len(p.s) {
					if r, err := strconv.ParseInt(p.s[p.i+2:p.i+6], 16, 32); err == nil {
						b.WriteRune(rune(r))
						p.i += 6
						continue
					}
				}
				b.WriteByte(e)
			default:
				b.WriteByte(e)
			}
			p.i += 2
			continue
		}
		if c == q {
			p.i++
			return b.String()
		}
		b.WriteByte(c)
		p.i++
	}
	return b.String()
}

func (p *jsParser) number() any {
	start := p.i
	for p.i < len(p.s) {
		c := p.s[p.i]
		if (c >= '0' && c <= '9') || c == '.' || c == '-' || c == '+' || c == 'e' || c == 'E' {
			p.i++
		} else {
			break
		}
	}
	if f, err := strconv.ParseFloat(p.s[start:p.i], 64); err == nil {
		return f
	}
	return p.s[start:p.i]
}

func (p *jsParser) ident() any {
	start := p.i
	for p.i < len(p.s) {
		c := p.s[p.i]
		if isAlnum(c) || c == '_' || c == '$' {
			p.i++
		} else {
			break
		}
	}
	switch w := p.s[start:p.i]; w {
	case "true":
		return true
	case "false":
		return false
	case "null", "undefined":
		return nil
	case "":
		p.i++
		return nil
	default:
		return w
	}
}

// ---- value helpers -------------------------------------------------------

func asText(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		if x == float64(int64(x)) {
			return strconv.FormatInt(int64(x), 10)
		}
		return strconv.FormatFloat(x, 'g', -1, 64)
	case bool:
		return strconv.FormatBool(x)
	}
	return ""
}

// asArr coerces a value to a slice, wrapping a lone object the way the JSX
// components do (they accept `x` or `[x]` for list-shaped props).
func asArr(v any) []any {
	switch x := v.(type) {
	case []any:
		return x
	case nil:
		return nil
	default:
		return []any{v}
	}
}

func asObj(v any) *jsObject {
	o, _ := v.(*jsObject)
	return o
}

func truthy(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		return x != ""
	case float64:
		return x != 0
	case nil:
		return false
	default:
		return true
	}
}

func attrStr(attrs map[string]any, k string) string { return asText(attrs[k]) }

// ---- low-level char helpers ----------------------------------------------

func isUpperAlpha(c byte) bool { return c >= 'A' && c <= 'Z' }
func isSpace(c byte) bool      { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }
func isAlnum(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

func esc(s string) string { return html.EscapeString(s) }

func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

var tabCounter atomic.Int64

func nextID(prefix string) string {
	return prefix + strconv.FormatInt(tabCounter.Add(1), 36)
}

// dedent removes the longest whitespace prefix common to all non-blank lines,
// so component children authored with indentation still parse as flush-left
// markdown.
func dedent(s string) string {
	lines := strings.Split(s, "\n")
	min := -1
	for _, ln := range lines {
		if strings.TrimSpace(ln) == "" {
			continue
		}
		indent := len(ln) - len(strings.TrimLeft(ln, " \t"))
		if min < 0 || indent < min {
			min = indent
		}
	}
	if min <= 0 {
		return s
	}
	for i, ln := range lines {
		if len(ln) >= min {
			lines[i] = ln[min:]
		} else {
			lines[i] = strings.TrimLeft(ln, " \t")
		}
	}
	return strings.Join(lines, "\n")
}
