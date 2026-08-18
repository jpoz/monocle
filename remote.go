package main

import (
	"bytes"
	"fmt"
	"html"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

// remote.go lets monocle review documents that live over HTTP, not only on
// disk: `monocle https://example.com/plan.md`.
//
// The commenting overlay reaches into the reviewed document's DOM, so that
// document has to be same-origin with the shell page. We therefore fetch it
// ourselves and serve it from the local review server rather than pointing the
// iframe at the remote URL. Subresources stay with the origin: an injected
// <base> makes every relative URL inside a remote HTML document resolve
// against its original location, so stylesheets, images, and scripts load
// straight from the site — with the user's own cookies — instead of through us.

const (
	// remoteTimeout bounds a single document fetch.
	remoteTimeout = 30 * time.Second
	// remoteMaxBytes caps the document body we will hold in memory.
	remoteMaxBytes = 32 << 20
)

var remoteClient = &http.Client{Timeout: remoteTimeout}

// isRemoteRef reports whether ref names an http(s) URL rather than a file.
func isRemoteRef(ref string) bool {
	u, err := url.Parse(ref)
	if err != nil {
		return false
	}
	return (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// remoteFetch is one fetched document: its final URL (after redirects), body,
// and declared content type.
type remoteFetch struct {
	url         *url.URL
	body        []byte
	contentType string
}

// fetchRemote GETs ref and returns the document. Non-2xx replies are errors —
// a login page or a 404 is not something worth opening a review server for.
func fetchRemote(ref string) (*remoteFetch, error) {
	req, err := http.NewRequest(http.MethodGet, ref, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "monocle")
	req.Header.Set("Accept", "text/html,text/markdown,text/plain;q=0.9,*/*;q=0.8")

	resp, err := remoteClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("%s: %s", ref, resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, remoteMaxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", ref, err)
	}
	if len(body) > remoteMaxBytes {
		return nil, fmt.Errorf("%s: document is larger than %d MiB", ref, remoteMaxBytes>>20)
	}

	// resp.Request.URL is the last URL in the redirect chain, which is what
	// relative links inside the document resolve against.
	final := resp.Request.URL
	if final == nil {
		if final, err = url.Parse(ref); err != nil {
			return nil, err
		}
	}
	return &remoteFetch{url: final, body: body, contentType: resp.Header.Get("Content-Type")}, nil
}

// docKind is how monocle should present a document.
type docKind int

const (
	kindPlain docKind = iota
	kindMarkdown
	kindMDX
	kindHTML
)

// remoteKind decides how to present a fetched document. The URL's extension
// wins when it names a format (matching how local files are classified), then
// the declared content type, then a sniff of the body for servers that say
// nothing useful.
func remoteKind(f *remoteFetch) docKind {
	p := f.url.Path
	switch {
	case isMDXPath(p):
		return kindMDX
	case isMarkdownPath(p):
		return kindMarkdown
	case isHTMLPath(p):
		return kindHTML
	}
	switch mediaType(f.contentType) {
	case "text/mdx":
		return kindMDX
	case "text/markdown", "text/x-markdown":
		return kindMarkdown
	case "text/html", "application/xhtml+xml":
		return kindHTML
	case "text/plain":
		return kindPlain
	}
	if looksLikeHTML(f.body) {
		return kindHTML
	}
	return kindPlain
}

// ext is the file extension a kind's source would carry, for handing to the
// terminal reader's parser.
func (k docKind) ext() string {
	switch k {
	case kindMDX:
		return ".mdx"
	case kindMarkdown:
		return ".md"
	}
	return ""
}

// isHTMLPath reports whether path names an HTML file.
func isHTMLPath(p string) bool {
	switch strings.ToLower(pathExt(p)) {
	case ".html", ".htm", ".xhtml":
		return true
	}
	return false
}

// mediaType is the bare type of a Content-Type header ("text/html"), lowercased
// and without parameters.
func mediaType(header string) string {
	mt, _, err := mime.ParseMediaType(header)
	if err != nil {
		return ""
	}
	return mt
}

// looksLikeHTML sniffs a body that arrived without a usable content type.
func looksLikeHTML(body []byte) bool {
	head := body
	if len(head) > 512 {
		head = head[:512]
	}
	head = bytes.ToLower(bytes.TrimLeft(head, " \t\r\n\uFEFF"))
	for _, p := range []string{"<!doctype html", "<html", "<head", "<body"} {
		if bytes.HasPrefix(head, []byte(p)) {
			return true
		}
	}
	return false
}

// remoteBase is the file-name-ish last segment of a URL, used as the document's
// title and its path within the review server. URLs that end in a slash (or
// have no path at all) fall back to the host.
func remoteBase(u *url.URL) string {
	base := path.Base(u.Path)
	if base == "" || base == "." || base == "/" {
		return u.Host
	}
	return base
}

// withBaseHref inserts a <base> pointing at the document's real location, so
// the page's relative URLs resolve against the origin instead of against the
// local review server. Ours goes first because the first <base href> in a
// document is the one the browser honors.
func withBaseHref(doc []byte, u *url.URL) []byte {
	tag := []byte(`<base href="` + html.EscapeString(u.String()) + `">`)
	i := baseInsertPoint(doc)
	if i < 0 {
		return append(tag, doc...)
	}
	out := make([]byte, 0, len(doc)+len(tag))
	out = append(out, doc[:i]...)
	out = append(out, tag...)
	return append(out, doc[i:]...)
}

// baseInsertPoint is the offset just after the document's <head> (or <html>)
// start tag, or -1 when it has neither.
func baseInsertPoint(doc []byte) int {
	for _, name := range []string{"head", "html"} {
		if i := startTagEnd(doc, name); i >= 0 {
			return i
		}
	}
	return -1
}

// startTagEnd returns the offset just past the first `<name …>` start tag,
// or -1 if there is none. The name must be followed by a delimiter so `<head`
// does not match `<header`.
func startTagEnd(doc []byte, name string) int {
	low := bytes.ToLower(doc)
	open := []byte("<" + name)
	for from := 0; ; {
		i := bytes.Index(low[from:], open)
		if i < 0 {
			return -1
		}
		i += from
		after := i + len(open)
		if after < len(low) && !isTagNameEnd(low[after]) {
			from = after // a longer name, e.g. <header>
			continue
		}
		end := bytes.IndexByte(low[i:], '>')
		if end < 0 {
			return -1
		}
		return i + end + 1
	}
}

func isTagNameEnd(c byte) bool {
	switch c {
	case ' ', '\t', '\r', '\n', '/', '>':
		return true
	}
	return false
}
