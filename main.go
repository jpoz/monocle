package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	if len(os.Args) != 2 || os.Args[1] == "-h" || os.Args[1] == "--help" {
		fmt.Fprintln(os.Stderr, "usage: monocle <file>")
		os.Exit(2)
	}

	path := os.Args[1]
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

	final, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	if err != nil {
		fmt.Fprintln(os.Stderr, "monocle:", err)
		os.Exit(1)
	}

	if fm, ok := final.(model); ok {
		st.Style = &fm.style
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
