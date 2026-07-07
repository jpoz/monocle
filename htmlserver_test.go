package main

import (
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsMarkdownPath(t *testing.T) {
	for path, want := range map[string]bool{
		"notes.md":     true,
		"NOTES.MD":     true,
		"doc.markdown": true,
		"plan.mdx":     true,
		"page.html":    false,
		"plain.txt":    false,
	} {
		if got := isMarkdownPath(path); got != want {
			t.Errorf("isMarkdownPath(%q) = %v, want %v", path, got, want)
		}
	}
}

// get fetches a path from the server and returns the response body.
func get(t *testing.T, srv *htmlServer, path string) (int, string) {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	rec := httptest.NewRecorder()
	srv.handler().ServeHTTP(rec, req)
	body, err := io.ReadAll(rec.Result().Body)
	if err != nil {
		t.Fatalf("reading %s response: %v", path, err)
	}
	return rec.Code, string(body)
}

func TestServeMarkdownDoc(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	dir := t.TempDir()
	md := "# Title\n\nSome **bold** text.\n\n| a | b |\n| - | - |\n| 1 | 2 |\n"
	if err := os.WriteFile(filepath.Join(dir, "doc.md"), []byte(md), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "img.png"), []byte("fake-png"), 0o644); err != nil {
		t.Fatal(err)
	}

	srv, err := newHTMLServer(filepath.Join(dir, "doc.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !srv.markdown {
		t.Error("server for .md file should be in markdown mode")
	}

	code, body := get(t, srv, "/doc/doc.md")
	if code != 200 {
		t.Fatalf("GET /doc/doc.md = %d, want 200", code)
	}
	for _, want := range []string{"<h1", "Title", "<strong>bold</strong>", "<table>"} {
		if !strings.Contains(body, want) {
			t.Errorf("rendered doc missing %q\nbody: %s", want, body)
		}
	}

	// Sibling assets still come straight from the directory.
	code, body = get(t, srv, "/doc/img.png")
	if code != 200 || body != "fake-png" {
		t.Errorf("GET /doc/img.png = %d %q, want 200 \"fake-png\"", code, body)
	}

	// The shell page points its iframe at the document and, for Markdown,
	// includes the content-width picker alongside the theme picker.
	code, body = get(t, srv, "/")
	if code != 200 || !strings.Contains(body, "/doc/doc.md") {
		t.Errorf("GET / = %d, want 200 with iframe src /doc/doc.md", code)
	}
	for _, want := range []string{"monocle-theme", "monocle-width"} {
		if !strings.Contains(body, want) {
			t.Errorf("markdown shell missing %q control", want)
		}
	}
}

func TestServeHTMLDocRaw(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	dir := t.TempDir()
	html := "<html><body><p>as-is</p></body></html>"
	if err := os.WriteFile(filepath.Join(dir, "page.html"), []byte(html), 0o644); err != nil {
		t.Fatal(err)
	}

	srv, err := newHTMLServer(filepath.Join(dir, "page.html"))
	if err != nil {
		t.Fatal(err)
	}
	if srv.markdown {
		t.Error("server for .html file should not be in markdown mode")
	}

	code, body := get(t, srv, "/doc/page.html")
	if code != 200 || body != html {
		t.Errorf("GET /doc/page.html = %d %q, want the file verbatim", code, body)
	}

	// HTML documents style themselves: the shell keeps the theme picker (it
	// themes the chrome) but drops the Markdown content-width picker.
	code, body = get(t, srv, "/")
	if code != 200 {
		t.Fatalf("GET / = %d, want 200", code)
	}
	if !strings.Contains(body, "monocle-theme") {
		t.Error("html shell missing monocle-theme control")
	}
	if strings.Contains(body, "monocle-width") {
		t.Error("html shell should not include the monocle-width control")
	}
}
