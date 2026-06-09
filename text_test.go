package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDocumentParagraphs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sample.md")
	src := "# Title\n\nFirst paragraph **bold** [link](http://x).\n\nSecond paragraph here.\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	doc, err := loadDocument(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(doc.paraStarts); got != 3 {
		t.Fatalf("paragraphs = %d, want 3", got)
	}
	if doc.words[0].text != "Title" {
		t.Errorf("first word = %q, want %q", doc.words[0].text, "Title")
	}
	for _, w := range doc.words {
		if w.text == "bold" {
			return
		}
	}
	t.Error("markdown emphasis not stripped to plain word")
}

func TestOrpIndex(t *testing.T) {
	tests := []struct {
		word string
		want int
	}{
		{"a", 0},
		{"the", 1},
		{"word", 1},
		{"hello", 2},
		{"\"hello\"", 3}, // leading quote skipped
		{"comprehension", 3},
		{"...", 1}, // all punctuation falls back to middle
	}
	for _, tt := range tests {
		if got := orpIndex(tt.word); got != tt.want {
			t.Errorf("orpIndex(%q) = %d, want %d", tt.word, got, tt.want)
		}
	}
}

func TestViewRendersAtAnySizeAndPosition(t *testing.T) {
	doc := &document{}
	words := []string{"One", "small", "paragraph.", "And", "a", "considerably-longer", "second", "one", "right", "here."}
	doc.paraStarts = []int{0, 3}
	for i, w := range words {
		para := 0
		if i >= 3 {
			para = 1
		}
		doc.words = append(doc.words, word{text: w, para: para})
	}

	for _, size := range [][2]int{{80, 24}, {20, 8}, {5, 3}, {120, 40}} {
		m := newModel(doc, 350)
		m.width, m.height = size[0], size[1]
		for i := range doc.words {
			m.idx = i
			_ = m.View() // must not panic at any word/size combination
		}
	}
}
