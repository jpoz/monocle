package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

func usage() {
	fmt.Fprintln(os.Stderr, "usage: monocle [--tui] <file|url>")
	fmt.Fprintln(os.Stderr, "  --tui   read Markdown in the terminal instead of the browser")
}

func main() {
	tui := false
	var files []string
	for _, arg := range os.Args[1:] {
		switch arg {
		case "--tui", "-tui":
			tui = true
		case "-h", "--help":
			usage()
			os.Exit(2)
		default:
			files = append(files, arg)
		}
	}
	if len(files) != 1 {
		usage()
		os.Exit(2)
	}

	path := files[0]

	// An http(s) argument is a remote document: monocle fetches it and reviews
	// it the same way it reviews a local one.
	if isRemoteRef(path) {
		if err := openRemote(path, tui); err != nil {
			fmt.Fprintln(os.Stderr, "monocle:", err)
			os.Exit(1)
		}
		return
	}

	// HTML documents always open in the browser with a commenting overlay:
	// their structure is theirs to render, not ours. Markdown opens there by
	// default too (rendered server-side); --tui keeps it in the terminal.
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".html" || ext == ".htm" {
		if tui {
			fmt.Fprintln(os.Stderr, "monocle: HTML files only open in the browser (drop --tui)")
			os.Exit(2)
		}
	}
	if ext == ".html" || ext == ".htm" || (isMarkdownPath(path) && !tui) {
		if err := serveWeb(path); err != nil {
			fmt.Fprintln(os.Stderr, "monocle:", err)
			os.Exit(1)
		}
		return
	}

	doc, err := loadDocument(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "monocle:", err)
		os.Exit(1)
	}

	if err := runTUI(doc, path); err != nil {
		fmt.Fprintln(os.Stderr, "monocle:", err)
		os.Exit(1)
	}
}

// openRemote reviews a document fetched over HTTP: HTML and (unless --tui)
// Markdown open in the browser, everything else in the terminal reader.
func openRemote(ref string, tui bool) error {
	f, err := fetchRemote(ref)
	if err != nil {
		return err
	}
	kind := remoteKind(f)

	if kind == kindHTML && tui {
		return errors.New("HTML documents only open in the browser (drop --tui)")
	}
	if kind == kindHTML || (kind != kindPlain && !tui) {
		return serveRemoteWeb(f, kind)
	}

	doc, err := parseDocument(f.body, kind.ext(), ref)
	if err != nil {
		return err
	}
	return runTUI(doc, f.url.String())
}

// runTUI reads doc in the terminal, restoring the saved position, comments, and
// display preferences for path (a file path or a document URL) and saving them
// back when the reader exits.
func runTUI(doc *document, path string) error {
	st := loadState()
	cs := loadComments()
	key := docKey(path)
	m := newModel(doc)
	m.path = path
	m.idx = st.resumeIndex(key, len(doc.words))
	m.comments = cs.forDoc(key)
	if st.Style != nil {
		m.style = *st.Style
	}
	m.setTheme(themeIndex(st.Theme))
	m.voiceID = st.Voice

	final, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	if err != nil {
		return err
	}

	fm, ok := final.(model)
	if !ok {
		return nil
	}
	fm.stopProc() // silence any read-aloud tail still playing
	st.Style = &fm.style
	st.Theme = themes[fm.themeIdx].name
	st.Voice = fm.voiceID
	st.setProgress(key, fm.idx, len(doc.words))
	if err := st.save(); err != nil {
		fmt.Fprintln(os.Stderr, "monocle: saving state:", err)
	}
	cs.setForDoc(key, fm.comments)
	if err := cs.save(); err != nil {
		fmt.Fprintln(os.Stderr, "monocle: saving comments:", err)
	}
	return nil
}
