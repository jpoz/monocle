package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
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

// do sends a request with a JSON body and returns the status and body.
func do(t *testing.T, srv *htmlServer, method, path, body string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	rec := httptest.NewRecorder()
	srv.handler().ServeHTTP(rec, req)
	out, err := io.ReadAll(rec.Result().Body)
	if err != nil {
		t.Fatalf("reading %s %s response: %v", method, path, err)
	}
	return rec.Code, string(out)
}

func TestSourceAPIRoundTrip(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	dir := t.TempDir()
	file := filepath.Join(dir, "doc.md")
	if err := os.WriteFile(file, []byte("# Before\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	srv, err := newHTMLServer(file)
	if err != nil {
		t.Fatal(err)
	}

	code, body := get(t, srv, "/__monocle/api/source")
	if code != 200 {
		t.Fatalf("GET source = %d, want 200", code)
	}
	var loaded struct {
		Text     string `json:"text"`
		Modified int64  `json:"modified"`
	}
	if err := json.Unmarshal([]byte(body), &loaded); err != nil {
		t.Fatalf("decoding source: %v", err)
	}
	if loaded.Text != "# Before\n" {
		t.Errorf("GET source text = %q, want the file contents", loaded.Text)
	}
	if loaded.Modified == 0 {
		t.Error("GET source should report the file's modtime")
	}

	// A write carrying the loaded modtime lands on disk, and the rendered
	// document picks it up on the next request.
	put := fmt.Sprintf(`{"text":"# After\n","modified":%d}`, loaded.Modified)
	code, body = do(t, srv, "PUT", "/__monocle/api/source", put)
	if code != 200 {
		t.Fatalf("PUT source = %d %s, want 200", code, body)
	}
	on, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(on) != "# After\n" {
		t.Errorf("file on disk = %q, want the saved text", on)
	}
	if _, rendered := get(t, srv, "/doc/doc.md"); !strings.Contains(rendered, "After") {
		t.Error("rendered document should show the saved edit")
	}

	// The file's permissions survive the write.
	info, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Errorf("file mode = %v, want 0644", info.Mode().Perm())
	}
}

func TestSourceAPIStaleWriteRefused(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	dir := t.TempDir()
	file := filepath.Join(dir, "doc.md")
	if err := os.WriteFile(file, []byte("# One\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv, err := newHTMLServer(file)
	if err != nil {
		t.Fatal(err)
	}

	// Someone else edited the file since the editor loaded it: the save is
	// refused rather than clobbering their work.
	code, body := do(t, srv, "PUT", "/__monocle/api/source", `{"text":"# Mine\n","modified":1}`)
	if code != http.StatusConflict {
		t.Fatalf("stale PUT = %d %s, want 409", code, body)
	}
	on, _ := os.ReadFile(file)
	if string(on) != "# One\n" {
		t.Errorf("refused write still changed the file: %q", on)
	}

	// Sending 0 means "overwrite anyway", which the editor does after warning.
	code, body = do(t, srv, "PUT", "/__monocle/api/source", `{"text":"# Mine\n","modified":0}`)
	if code != 200 {
		t.Fatalf("forced PUT = %d %s, want 200", code, body)
	}
	on, _ = os.ReadFile(file)
	if string(on) != "# Mine\n" {
		t.Errorf("file on disk = %q, want the overwritten text", on)
	}
}

func TestRemoteDocumentIsReadOnly(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	u, err := url.Parse("https://example.com/doc.md")
	if err != nil {
		t.Fatal(err)
	}
	srv := newRemoteHTMLServer(&remoteFetch{url: u, body: []byte("# Remote\n")}, kindMarkdown)
	if srv.editable {
		t.Error("a remote document has no file to write back to")
	}

	if code, _ := get(t, srv, "/"); code != 200 {
		t.Fatalf("GET / = %d, want 200", code)
	}
	if _, body := get(t, srv, "/"); strings.Contains(body, "monocle-editor") {
		t.Error("remote shell should not include the editor pane")
	}
	if code, _ := do(t, srv, "PUT", "/__monocle/api/source", `{"text":"x"}`); code != http.StatusForbidden {
		t.Errorf("PUT to a remote document = %d, want 403", code)
	}
}
