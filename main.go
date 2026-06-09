package main

import (
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	wpm := flag.Int("w", 350, "words per minute")
	flag.Usage = func() {
		fmt.Fprintln(flag.CommandLine.Output(), "usage: rsvp [-w wpm] <file>")
		flag.PrintDefaults()
	}
	flag.Parse()

	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(2)
	}

	doc, err := loadDocument(flag.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, "rsvp:", err)
		os.Exit(1)
	}

	if _, err := tea.NewProgram(newModel(doc, *wpm), tea.WithAltScreen()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "rsvp:", err)
		os.Exit(1)
	}
}
