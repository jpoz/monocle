package main

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	gmhtml "github.com/yuin/goldmark/renderer/html"
)

//go:embed web/shell.html web/overlay.js web/overlay.css web/docpage.html web/markdown.css web/themes.css web/mdx.css web/mdx.js web/mdxpage.html
var webFS embed.FS

// htmlServer hosts a single document for review: it serves the file (and its
// sibling assets) inside an iframe, layers a commenting overlay on top, and
// persists notes through a small JSON API. HTML files are served verbatim;
// Markdown files are rendered to HTML on each request, so a browser refresh
// picks up edits. Remote documents (remote != nil) are re-fetched per request
// instead of re-read. One process serves one document; the comment list is
// guarded by mu because the browser fires concurrent fetches.
type htmlServer struct {
	mu       sync.Mutex
	store    *htmlCommentStore
	key      string   // docKey, the persistence key
	path     string   // absolute path to the document, or its URL when remote
	dir      string   // directory the file lives in (asset root); local only
	base     string   // file name within dir, or the URL's last segment
	relPath  string   // path (or URL) shown in the toolbar
	remote   *url.URL // document location when fetched over HTTP, else nil
	markdown bool     // render the document as Markdown instead of serving it raw
	mdx      bool     // render as MDX (components) rather than plain Markdown
	counter  int64    // bumped per created comment to keep IDs unique within a run
}

// newHTMLServer builds the review server for an HTML or Markdown file.
func newHTMLServer(path string) (*htmlServer, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(abs); err != nil {
		return nil, err
	}

	return &htmlServer{
		store:    loadHTMLComments(),
		key:      docKey(path),
		path:     abs,
		dir:      filepath.Dir(abs),
		base:     filepath.Base(abs),
		relPath:  repoRelPath(path),
		markdown: isMarkdownPath(path),
		mdx:      isMDXPath(path),
	}, nil
}

// newRemoteHTMLServer builds the review server for a document fetched over
// HTTP. The document is re-fetched per request, so a browser refresh picks up
// changes the same way it picks up edits to a local file.
func newRemoteHTMLServer(f *remoteFetch, kind docKind) *htmlServer {
	ref := f.url.String()
	return &htmlServer{
		store:    loadHTMLComments(),
		key:      docKey(ref),
		path:     ref,
		base:     remoteBase(f.url),
		relPath:  ref,
		remote:   f.url,
		markdown: kind == kindMarkdown || kind == kindMDX,
		mdx:      kind == kindMDX,
	}
}

// handler routes the shell page, embedded assets, comment API, and the
// document itself (with its sibling assets).
func (s *htmlServer) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleShell)
	mux.HandleFunc("/__monocle/overlay.js", asset("web/overlay.js", "text/javascript"))
	mux.HandleFunc("/__monocle/overlay.css", asset("web/overlay.css", "text/css"))
	mux.HandleFunc("/__monocle/markdown.css", asset("web/markdown.css", "text/css"))
	mux.HandleFunc("/__monocle/themes.css", asset("web/themes.css", "text/css"))
	mux.HandleFunc("/__monocle/mdx.css", asset("web/mdx.css", "text/css"))
	mux.HandleFunc("/__monocle/mdx.js", asset("web/mdx.js", "text/javascript"))
	mux.HandleFunc("/__monocle/api/comments", s.handleComments)
	mux.HandleFunc("/__monocle/api/comments/", s.handleComment)
	mux.HandleFunc("/__monocle/api/export", s.handleExport)
	mux.HandleFunc("/__monocle/api/path", s.handlePath)
	mux.Handle("/doc/", http.StripPrefix("/doc/", s.docHandler()))
	return mux
}

// docHandler serves the document directory. In Markdown mode requests for
// the document itself get the rendered page; everything else (images and
// other relative assets) falls through to the file server.
func (s *htmlServer) docHandler() http.Handler {
	if s.remote != nil {
		return s.remoteDocHandler()
	}
	files := http.FileServer(http.Dir(s.dir))
	if !s.markdown {
		return files
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == s.base {
			if s.mdx {
				s.handleMDXDoc(w, r)
			} else {
				s.handleMarkdownDoc(w, r)
			}
			return
		}
		files.ServeHTTP(w, r)
	})
}

// remoteDocHandler serves a document fetched over HTTP. Only the document
// itself is proxied — it has to be same-origin for the overlay to reach into
// it. Anything else that lands under /doc/ is a relative asset or link, and is
// bounced back to the origin, which serves it with the user's own cookies.
func (s *htmlServer) remoteDocHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != s.base {
			ref := &url.URL{Path: r.URL.Path, RawQuery: r.URL.RawQuery}
			http.Redirect(w, r, s.remote.ResolveReference(ref).String(), http.StatusFound)
			return
		}
		f, err := fetchRemote(s.remote.String())
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		switch {
		case s.mdx:
			s.writeMDXPage(w, f.body)
		case s.markdown:
			s.writeMarkdownPage(w, f.body)
		default:
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write(withBaseHref(f.body, f.url))
		}
	})
}

// serveWeb starts the review server for an HTML or Markdown file, opens it
// in the browser, and blocks until interrupted (Ctrl-C).
func serveWeb(path string) error {
	srv, err := newHTMLServer(path)
	if err != nil {
		return err
	}
	return srv.serve()
}

// serveRemoteWeb starts the review server for a document fetched over HTTP.
func serveRemoteWeb(f *remoteFetch, kind docKind) error {
	return newRemoteHTMLServer(f, kind).serve()
}

// serve listens on a loopback port, opens the review page in the browser, and
// blocks until interrupted (Ctrl-C).
func (srv *htmlServer) serve() error {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	url := fmt.Sprintf("http://%s/", ln.Addr().String())

	server := &http.Server{Handler: srv.handler()}
	go func() {
		if err := server.Serve(ln); err != nil && err != http.ErrServerClosed {
			fmt.Fprintln(os.Stderr, "monocle:", err)
		}
	}()

	fmt.Printf("monocle: reviewing %s\n", srv.relPath)
	fmt.Printf("monocle: serving %s  (press Ctrl-C to stop)\n", url)
	if err := openBrowser(url); err != nil {
		fmt.Fprintf(os.Stderr, "monocle: open %s in your browser\n", url)
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig
	fmt.Println("\nmonocle: shutting down")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return server.Shutdown(ctx)
}

// isMarkdownPath reports whether path names a Markdown file, matching the
// extensions the terminal reader parses structurally.
func isMarkdownPath(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".markdown", ".mdx":
		return true
	}
	return false
}

// mdRenderer converts Markdown to HTML: GFM for tables/strikethrough/task
// lists, heading IDs so fragment links work, and raw HTML passed through —
// these are the user's own local files.
var mdRenderer = goldmark.New(
	goldmark.WithExtensions(extension.GFM),
	goldmark.WithParserOptions(parser.WithAutoHeadingID()),
	goldmark.WithRendererOptions(gmhtml.WithUnsafe()),
)

// handleMarkdownDoc renders the Markdown source into the document page
// template. It re-reads the file per request so a refresh shows edits.
func (s *htmlServer) handleMarkdownDoc(w http.ResponseWriter, _ *http.Request) {
	src, err := os.ReadFile(s.path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.writeMarkdownPage(w, src)
}

// writeMarkdownPage renders Markdown source into the document page template.
func (s *htmlServer) writeMarkdownPage(w http.ResponseWriter, src []byte) {
	var body bytes.Buffer
	if err := mdRenderer.Convert(src, &body); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	tmpl, err := template.ParseFS(webFS, "web/docpage.html")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	data := struct {
		Title string
		Body  template.HTML
	}{
		Title: s.base,
		Body:  template.HTML(body.String()),
	}
	if err := tmpl.Execute(w, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// handleMDXDoc renders the MDX source (Markdown + JSX components) into the MDX
// document page. Like the Markdown path it re-reads per request so a refresh
// shows edits; the frontmatter title/description become the document header.
func (s *htmlServer) handleMDXDoc(w http.ResponseWriter, _ *http.Request) {
	src, err := os.ReadFile(s.path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.writeMDXPage(w, src)
}

// writeMDXPage renders MDX source into the MDX document page template.
func (s *htmlServer) writeMDXPage(w http.ResponseWriter, src []byte) {
	doc := renderMDXDocument(src)
	tmpl, err := template.ParseFS(webFS, "web/mdxpage.html")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	data := struct {
		Title     string
		Head      bool
		HeadTitle string
		HeadDesc  string
		Body      template.HTML
	}{
		Title:     s.base,
		Head:      doc.Title != "",
		HeadTitle: doc.Title,
		HeadDesc:  doc.Desc,
		Body:      template.HTML(doc.Body),
	}
	if err := tmpl.Execute(w, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// handleShell renders the overlay shell page wrapping the document iframe.
func (s *htmlServer) handleShell(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	tmpl, err := template.ParseFS(webFS, "web/shell.html")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	data := struct {
		Title    string
		RelPath  string
		DocPath  string
		Markdown bool
	}{
		Title:    s.base,
		RelPath:  s.relPath,
		DocPath:  "/doc/" + url.PathEscape(s.base),
		Markdown: s.markdown,
	}
	if err := tmpl.Execute(w, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// asset serves an embedded static file with a fixed content type.
func asset(name, contentType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data, err := webFS.ReadFile(name)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", contentType+"; charset=utf-8")
		w.Write(data)
	}
}

// handleComments lists (GET) or creates (POST) comments for the document.
func (s *htmlServer) handleComments(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.mu.Lock()
		list := s.store.forDoc(s.key)
		s.mu.Unlock()
		if list == nil {
			list = []htmlComment{}
		}
		writeJSON(w, list)

	case http.MethodPost:
		var in struct {
			Quote, Prefix, Suffix, Body string
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		now := time.Now()
		s.mu.Lock()
		s.counter++
		c := htmlComment{
			ID:      strconv.FormatInt(now.UnixNano(), 36) + "-" + strconv.FormatInt(s.counter, 36),
			Quote:   in.Quote,
			Prefix:  in.Prefix,
			Suffix:  in.Suffix,
			Body:    in.Body,
			Created: now,
			Updated: now,
		}
		list := append(s.store.forDoc(s.key), c)
		s.persist(list)
		s.mu.Unlock()
		writeJSON(w, c)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleComment updates (PUT) or deletes (DELETE) a single comment by ID.
func (s *htmlServer) handleComment(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/__monocle/api/comments/")
	if id == "" {
		http.NotFound(w, r)
		return
	}

	switch r.Method {
	case http.MethodPut:
		var in struct{ Body string }
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		s.mu.Lock()
		list := s.store.forDoc(s.key)
		var updated *htmlComment
		for i := range list {
			if list[i].ID == id {
				list[i].Body = in.Body
				list[i].Updated = time.Now()
				updated = &list[i]
			}
		}
		if updated != nil {
			s.persist(list)
		}
		c := htmlComment{}
		if updated != nil {
			c = *updated
		}
		s.mu.Unlock()
		if updated == nil {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, c)

	case http.MethodDelete:
		s.mu.Lock()
		list := s.store.forDoc(s.key)
		kept := list[:0:0]
		for _, c := range list {
			if c.ID != id {
				kept = append(kept, c)
			}
		}
		s.persist(kept)
		s.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleExport copies every comment to the clipboard as Markdown, the same
// format the terminal reader's export produces.
func (s *htmlServer) handleExport(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	list := s.store.forDoc(s.key)
	s.mu.Unlock()
	if len(list) == 0 {
		writeJSON(w, map[string]any{"ok": false, "error": "no comments yet"})
		return
	}
	md := exportHTMLComments(s.relPath, list)
	if err := copyToClipboard(md); err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error(), "markdown": md})
		return
	}
	writeJSON(w, map[string]any{"ok": true, "markdown": md})
}

// handlePath copies the document's repo-relative path — or its URL, for a
// remote document — to the clipboard.
func (s *htmlServer) handlePath(w http.ResponseWriter, r *http.Request) {
	p := s.relPath
	if err := copyToClipboard(p); err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error(), "path": p})
		return
	}
	writeJSON(w, map[string]any{"ok": true, "path": p})
}

// persist writes the document's comment list to the store. Callers must hold
// s.mu. A save error is reported but not fatal — the in-memory list stays
// authoritative for the session.
func (s *htmlServer) persist(list []htmlComment) {
	s.store.setForDoc(s.key, list)
	if err := s.store.save(); err != nil {
		fmt.Fprintln(os.Stderr, "monocle: saving comments:", err)
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

// openBrowser launches the platform's default browser pointed at url.
func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", "", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}
