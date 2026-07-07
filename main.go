package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

func usage() {
	fmt.Fprintln(os.Stderr, "usage: monocle [--tui] <file>")
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
		fmt.Fprintln(os.Stderr, "monocle:", err)
		os.Exit(1)
	}

	if fm, ok := final.(model); ok {
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
	}
}
