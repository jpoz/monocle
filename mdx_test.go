package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseJSValue(t *testing.T) {
	v, _ := parseJSValue(`{ html: 'a', n: 3, ok: true, arr: [1, "two", false], nested: { k: 'v' } }`)
	o := asObj(v)
	if o == nil {
		t.Fatalf("expected object, got %T", v)
	}
	if got := asText(o.get("html")); got != "a" {
		t.Errorf("html = %q", got)
	}
	if got := asText(o.get("n")); got != "3" {
		t.Errorf("n = %q", got)
	}
	if b, ok := o.get("ok").(bool); !ok || !b {
		t.Errorf("ok = %v", o.get("ok"))
	}
	arr := asArr(o.get("arr"))
	if len(arr) != 3 {
		t.Fatalf("arr len = %d", len(arr))
	}
	if asText(arr[1]) != "two" {
		t.Errorf("arr[1] = %q", asText(arr[1]))
	}
	if asText(asObj(o.get("nested")).get("k")) != "v" {
		t.Errorf("nested.k wrong")
	}
}

func TestJSObjectKeepsOrder(t *testing.T) {
	v, _ := parseJSValue(`{ z: 1, a: 2, m: 3 }`)
	o := asObj(v)
	want := []string{"z", "a", "m"}
	if strings.Join(o.keys, ",") != strings.Join(want, ",") {
		t.Errorf("key order = %v, want %v", o.keys, want)
	}
}

func TestStringEscapes(t *testing.T) {
	v, _ := parseJSValue(`"line1\nline2\t\"q\""`)
	if v != "line1\nline2\t\"q\"" {
		t.Errorf("escapes = %q", v)
	}
}

func TestScanSelfClosingAndChildren(t *testing.T) {
	nodes := scanMDX("intro text\n\n<Table columns={[\"A\"]} rows={[[\"1\"]]} />\n\n<Callout tone=\"info\">\n\nbody\n\n</Callout>\n")
	if len(nodes) != 3 {
		t.Fatalf("nodes = %d: %+v", len(nodes), nodes)
	}
	if nodes[0].isElem {
		t.Errorf("node0 should be markdown")
	}
	if !nodes[1].isElem || nodes[1].name != "Table" || !nodes[1].self {
		t.Errorf("node1 wrong: %+v", nodes[1])
	}
	if !nodes[2].isElem || nodes[2].name != "Callout" || nodes[2].self {
		t.Errorf("node2 wrong: %+v", nodes[2])
	}
	if strings.TrimSpace(nodes[2].children) != "body" {
		t.Errorf("callout children = %q", nodes[2].children)
	}
}

func TestNestedSameNameChildren(t *testing.T) {
	src := `<Columns>
<Column label="Before">

text A

</Column>
<Column label="After">

text B

</Column>
</Columns>`
	nodes := scanMDX(src)
	if len(nodes) != 1 || nodes[0].name != "Columns" {
		t.Fatalf("expected one Columns node, got %+v", nodes)
	}
	inner := scanMDX(nodes[0].children)
	cols := 0
	for _, n := range inner {
		if n.isElem && n.name == "Column" {
			cols++
		}
	}
	if cols != 2 {
		t.Errorf("expected 2 Column children, got %d", cols)
	}
}

func TestRenderComponentsProduceClasses(t *testing.T) {
	src := `<Callout tone="risk">

**Danger**

</Callout>

<Table columns={["A","B"]} rows={[["1","2"]]} />

<Diagram data={{ html: '<div class="diagram-node">X</div>', caption: 'cap' }} />

<DataModel entities={[{ id: "t", name: "t", fields: [{ name: "id", type: "uuid", pk: true }] }]} />
`
	out := renderMDXFragment(src)
	for _, want := range []string{
		"arc-mdx-callout--risk",
		"arc-mdx-table",
		"arc-mdx-diagram-frame",
		"data-shadow-html=",
		"arc-mdx-datamodel-entity",
		"arc-mdx-datamodel-badge--pk",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q", want)
		}
	}
	if strings.Contains(out, "<Callout") || strings.Contains(out, "<Diagram") {
		t.Errorf("raw JSX leaked into output")
	}
}

func TestFrontmatterStripped(t *testing.T) {
	doc := renderMDXDocument([]byte("---\ntitle: \"Hi\"\nbrief: \"A brief\"\n---\n\n# Body\n"))
	if doc.Title != "Hi" || doc.Desc != "A brief" {
		t.Errorf("frontmatter = %+v", doc)
	}
	if !strings.Contains(doc.Body, "Body") || strings.Contains(doc.Body, "title:") {
		t.Errorf("body wrong: %q", doc.Body)
	}
}

func TestTUIFlattenNoRawJSX(t *testing.T) {
	src := `<RichText>

# Heading

Some prose.

</RichText>

<Table columns={["A"]} rows={[["1"]]} />
`
	md := mdxToMarkdown(src)
	if strings.Contains(md, "<RichText") || strings.Contains(md, "<Table") {
		t.Errorf("raw JSX leaked into TUI markdown: %q", md)
	}
	if !strings.Contains(md, "# Heading") || !strings.Contains(md, "| A |") {
		t.Errorf("flattened markdown missing content: %q", md)
	}
}

// TestServeMDXHTTP exercises the full browser path: newHTMLServer → handler →
// the MDX doc page and its stylesheet/script assets.
func TestServeMDXHTTP(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plan.mdx")
	mdx := "---\ntitle: \"My Plan\"\nbrief: \"what it does\"\n---\n\n<RichText>\n\n# Overview\n\nProse here.\n\n</RichText>\n\n<Diagram data={{ html: '<div class=\"diagram-node\">Node</div>', caption: 'flow' }} />\n"
	if err := os.WriteFile(path, []byte(mdx), 0o644); err != nil {
		t.Fatal(err)
	}
	srv, err := newHTMLServer(path)
	if err != nil {
		t.Fatal(err)
	}
	if !srv.mdx {
		t.Fatal("server should be in mdx mode")
	}
	h := srv.handler()

	// The rendered document page.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/doc/plan.mdx", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("doc status = %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`id="arc-mdx-root"`, "arc-mdx-dochead", "My Plan", "what it does",
		"Overview", "arc-mdx-diagram-frame", "data-shadow-html=",
		`href="/__monocle/mdx.css"`, `src="/__monocle/mdx.js"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("doc page missing %q", want)
		}
	}

	// The MDX assets must be served.
	for _, u := range []string{"/__monocle/mdx.css", "/__monocle/mdx.js"} {
		r := httptest.NewRecorder()
		h.ServeHTTP(r, httptest.NewRequest(http.MethodGet, u, nil))
		if r.Code != http.StatusOK || r.Body.Len() == 0 {
			t.Errorf("%s status=%d len=%d", u, r.Code, r.Body.Len())
		}
	}
}

// TestRealPlanFile renders the real arc plan if it's checked out locally, as a
// smoke test that no component leaks as raw JSX. Skipped when absent.
func TestRealPlanFile(t *testing.T) {
	const path = "/Users/jpoz/Developer/arc-edit-workflows/plans/workflow-revision-adoption/plan.mdx"
	src, err := os.ReadFile(path)
	if err != nil {
		t.Skip("real plan file not present")
	}
	out := renderMDXDocument(src).Body
	for _, tag := range []string{"&lt;Diagram", "&lt;RichText", "&lt;Table", "&lt;Endpoint", "&lt;DataModel", "&lt;TabsBlock", "&lt;QuestionForm", "&lt;WireframeBlock"} {
		if strings.Contains(out, tag) {
			t.Errorf("component leaked as escaped text: %s", tag)
		}
	}
	for _, want := range []string{"arc-mdx-callout", "arc-mdx-table", "arc-mdx-diagram", "arc-mdx-datamodel", "arc-mdx-endpoint", "arc-mdx-tabs", "arc-mdx-qform", "arc-mdx-filetree"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected rendered component %q", want)
		}
	}
}
