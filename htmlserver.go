package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"net"
	"net/http"
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
)

//go:embed web/shell.html web/overlay.js web/overlay.css
var webFS embed.FS

// htmlServer hosts a single HTML document for review: it serves the file (and
// its sibling assets) inside an iframe, layers a commenting overlay on top,
// and persists notes through a small JSON API. One process serves one
// document; the comment list is guarded by mu because the browser fires
// concurrent fetches.
type htmlServer struct {
	mu      sync.Mutex
	store   *htmlCommentStore
	key     string // docKey, the persistence key
	path    string // absolute path to the HTML file
	dir     string // directory the file lives in (asset root)
	base    string // file name within dir
	relPath string // path shown in the toolbar
	counter int64  // bumped per created comment to keep IDs unique within a run
}

// serveHTML starts the review server for an HTML file, opens it in the
// browser, and blocks until interrupted (Ctrl-C).
func serveHTML(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if _, err := os.Stat(abs); err != nil {
		return err
	}

	srv := &htmlServer{
		store:   loadHTMLComments(),
		key:     docKey(path),
		path:    abs,
		dir:     filepath.Dir(abs),
		base:    filepath.Base(abs),
		relPath: repoRelPath(path),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", srv.handleShell)
	mux.HandleFunc("/__monocle/overlay.js", asset("web/overlay.js", "text/javascript"))
	mux.HandleFunc("/__monocle/overlay.css", asset("web/overlay.css", "text/css"))
	mux.HandleFunc("/__monocle/api/comments", srv.handleComments)
	mux.HandleFunc("/__monocle/api/comments/", srv.handleComment)
	mux.HandleFunc("/__monocle/api/export", srv.handleExport)
	mux.HandleFunc("/__monocle/api/path", srv.handlePath)
	mux.Handle("/doc/", http.StripPrefix("/doc/", http.FileServer(http.Dir(srv.dir))))

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	url := fmt.Sprintf("http://%s/", ln.Addr().String())

	server := &http.Server{Handler: mux}
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
		Title   string
		RelPath string
		DocPath string
	}{
		Title:   s.base,
		RelPath: s.relPath,
		DocPath: "/doc/" + s.base,
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

// handlePath copies the document's repo-relative path to the clipboard.
func (s *htmlServer) handlePath(w http.ResponseWriter, r *http.Request) {
	p := repoRelPath(s.path)
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
