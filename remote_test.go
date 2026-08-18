package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestIsRemoteRef(t *testing.T) {
	for ref, want := range map[string]bool{
		"https://example.com/plan.md":  true,
		"http://example.com":           true,
		"HTTPS://example.com/x":        true, // url.Parse lowercases the scheme
		"docs/plan.md":                 false,
		"/abs/plan.md":                 false,
		"file:///tmp/plan.md":          false,
		"https://":                     false,
		"C:\\notes\\plan.md":           false,
		"https://claude.ai/code/a/800": true,
	} {
		if got := isRemoteRef(ref); got != want {
			t.Errorf("isRemoteRef(%q) = %v, want %v", ref, got, want)
		}
	}
}

func TestRemoteKind(t *testing.T) {
	cases := []struct {
		ref, ct string
		body    string
		want    docKind
	}{
		{"https://x.test/plan.md", "text/plain", "", kindMarkdown},
		{"https://x.test/plan.mdx", "", "", kindMDX},
		{"https://x.test/page.html", "", "", kindHTML},
		{"https://x.test/a/800b", "text/html; charset=utf-8", "", kindHTML},
		{"https://x.test/a/800b", "text/markdown", "", kindMarkdown},
		{"https://x.test/a/800b", "", "<!DOCTYPE html>\n<html>", kindHTML},
		{"https://x.test/notes", "text/plain", "just words", kindPlain},
		{"https://x.test/notes", "", "just words", kindPlain},
	}
	for _, c := range cases {
		u, err := url.Parse(c.ref)
		if err != nil {
			t.Fatal(err)
		}
		f := &remoteFetch{url: u, body: []byte(c.body), contentType: c.ct}
		if got := remoteKind(f); got != c.want {
			t.Errorf("remoteKind(%q, %q) = %v, want %v", c.ref, c.ct, got, c.want)
		}
	}
}

func TestWithBaseHref(t *testing.T) {
	u, err := url.Parse("https://x.test/a/b?v=1")
	if err != nil {
		t.Fatal(err)
	}
	tag := `<base href="https://x.test/a/b?v=1">`

	cases := []struct{ in, want string }{
		{"<html><head><title>t</title></head>", "<html><head>" + tag + "<title>t</title></head>"},
		{"<HTML>\n<HEAD lang=en>x", "<HTML>\n<HEAD lang=en>" + tag + "x"},
		{"<html><header>x</header>", "<html>" + tag + "<header>x</header>"},
		{"<p>fragment</p>", tag + "<p>fragment</p>"},
	}
	for _, c := range cases {
		if got := string(withBaseHref([]byte(c.in), u)); got != c.want {
			t.Errorf("withBaseHref(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// remoteSite serves an HTML document with a relative stylesheet plus a
// Markdown document, standing in for a site monocle is pointed at.
func remoteSite() *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/a/page", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(`<html><head><link rel="stylesheet" href="app.css"></head><body><p>Remote body</p></body></html>`))
	})
	mux.HandleFunc("/a/plan.md", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/markdown")
		w.Write([]byte("# Remote plan\n\nSome **bold** text.\n"))
	})
	return httptest.NewServer(mux)
}

func TestServeRemoteHTMLDoc(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	site := remoteSite()
	defer site.Close()

	f, err := fetchRemote(site.URL + "/a/page")
	if err != nil {
		t.Fatal(err)
	}
	kind := remoteKind(f)
	if kind != kindHTML {
		t.Fatalf("kind = %v, want kindHTML", kind)
	}

	srv := newRemoteHTMLServer(f, kind)
	if srv.base != "page" {
		t.Errorf("base = %q, want %q", srv.base, "page")
	}
	if srv.markdown {
		t.Error("HTML document should not be in markdown mode")
	}

	code, body := get(t, srv, "/doc/page")
	if code != http.StatusOK {
		t.Fatalf("GET /doc/page = %d, want 200", code)
	}
	if want := `<base href="` + site.URL + `/a/page">`; !strings.Contains(body, want) {
		t.Errorf("document is missing %s:\n%s", want, body)
	}
	if !strings.Contains(body, "Remote body") {
		t.Errorf("document body not passed through:\n%s", body)
	}

	// The shell frames the fetched document, and the toolbar shows its URL.
	code, shell := get(t, srv, "/")
	if code != http.StatusOK {
		t.Fatalf("GET / = %d, want 200", code)
	}
	if !strings.Contains(shell, `src="/doc/page"`) {
		t.Errorf("shell does not frame /doc/page:\n%s", shell)
	}
	if !strings.Contains(shell, site.URL+"/a/page") {
		t.Errorf("shell does not show the document URL:\n%s", shell)
	}
}

func TestRemoteAssetRedirectsToOrigin(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	site := remoteSite()
	defer site.Close()

	f, err := fetchRemote(site.URL + "/a/page")
	if err != nil {
		t.Fatal(err)
	}
	srv := newRemoteHTMLServer(f, kindHTML)

	req := httptest.NewRequest("GET", "/doc/app.css?v=2", nil)
	rec := httptest.NewRecorder()
	srv.handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("GET /doc/app.css = %d, want 302", rec.Code)
	}
	if want := site.URL + "/a/app.css?v=2"; rec.Header().Get("Location") != want {
		t.Errorf("Location = %q, want %q", rec.Header().Get("Location"), want)
	}
}

func TestServeRemoteMarkdownDoc(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	site := remoteSite()
	defer site.Close()

	f, err := fetchRemote(site.URL + "/a/plan.md")
	if err != nil {
		t.Fatal(err)
	}
	srv := newRemoteHTMLServer(f, remoteKind(f))
	if !srv.markdown {
		t.Fatal("remote .md document should be in markdown mode")
	}

	code, body := get(t, srv, "/doc/plan.md")
	if code != http.StatusOK {
		t.Fatalf("GET /doc/plan.md = %d, want 200", code)
	}
	if !strings.Contains(body, "<h1") || !strings.Contains(body, "<strong>bold</strong>") {
		t.Errorf("Markdown was not rendered:\n%s", body)
	}
}

func TestFetchRemoteError(t *testing.T) {
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusForbidden)
	}))
	defer site.Close()

	if _, err := fetchRemote(site.URL + "/private"); err == nil {
		t.Fatal("fetchRemote of a 403 should fail")
	} else if !strings.Contains(err.Error(), "403") {
		t.Errorf("error %q should mention the status", err)
	}
}

func TestRemoteDocKeyIsTheURL(t *testing.T) {
	ref := "https://x.test/a/plan.md"
	if got := docKey(ref); got != ref {
		t.Errorf("docKey(%q) = %q, want the URL unchanged", ref, got)
	}
	if got := repoRelPath(ref); got != ref {
		t.Errorf("repoRelPath(%q) = %q, want the URL unchanged", ref, got)
	}
}
