package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type word struct {
	text string
	para int
}

type document struct {
	words      []word
	paraStarts []int // index into words of the first word of each paragraph
}

var (
	fenceRe     = regexp.MustCompile("(?m)^\\s*(```|~~~).*$")
	imageLinkRe = regexp.MustCompile(`!?\[([^\]]*)\]\([^)]*\)`)
	headingRe   = regexp.MustCompile(`(?m)^#{1,6}\s+`)
	quoteRe     = regexp.MustCompile(`(?m)^\s*>\s?`)
	listRe      = regexp.MustCompile(`(?m)^\s*([-*+]|\d+[.)])\s+`)
	emphasisRe  = regexp.MustCompile("[*_`~]+")
	paraSplitRe = regexp.MustCompile(`\n\s*\n`)
)

func loadDocument(path string) (*document, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	text := string(data)
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".markdown", ".mdx":
		text = stripMarkdown(text)
	}

	doc := &document{}
	for _, para := range paraSplitRe.Split(text, -1) {
		fields := strings.Fields(para)
		if len(fields) == 0 {
			continue
		}
		p := len(doc.paraStarts)
		doc.paraStarts = append(doc.paraStarts, len(doc.words))
		for _, f := range fields {
			doc.words = append(doc.words, word{text: f, para: p})
		}
	}
	if len(doc.words) == 0 {
		return nil, fmt.Errorf("%s: no readable text", path)
	}
	return doc, nil
}

func stripMarkdown(s string) string {
	s = fenceRe.ReplaceAllString(s, "")
	s = imageLinkRe.ReplaceAllString(s, "$1")
	s = headingRe.ReplaceAllString(s, "")
	s = quoteRe.ReplaceAllString(s, "")
	s = listRe.ReplaceAllString(s, "")
	s = emphasisRe.ReplaceAllString(s, "")
	return s
}

// orpIndex returns the rune index of the word's optimal recognition point:
// roughly a third of the way into the word, skipping leading punctuation
// such as quotes or parentheses.
func orpIndex(s string) int {
	rs := []rune(s)
	start, end := 0, len(rs)
	for start < end && !isWordRune(rs[start]) {
		start++
	}
	for end > start && !isWordRune(rs[end-1]) {
		end--
	}
	if start == end { // all punctuation
		return (len(rs) - 1) / 2
	}
	var o int
	switch core := end - start; {
	case core <= 1:
		o = 0
	case core <= 4:
		o = 1
	case core <= 9:
		o = 2
	case core <= 13:
		o = 3
	default:
		o = 4
	}
	return start + o
}

func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsNumber(r)
}

// wordDelay returns how long a word stays on screen, lingering on
// punctuation, long words, and paragraph boundaries.
func wordDelay(w word, next *word, wpm int) time.Duration {
	base := float64(time.Minute) / float64(wpm)
	mult := 1.0
	switch t := strings.TrimRight(w.text, "\"')]}»"); {
	case strings.HasSuffix(t, "."), strings.HasSuffix(t, "!"),
		strings.HasSuffix(t, "?"), strings.HasSuffix(t, "…"):
		mult = 2.2
	case strings.HasSuffix(t, ","), strings.HasSuffix(t, ";"),
		strings.HasSuffix(t, ":"), strings.HasSuffix(t, "—"):
		mult = 1.6
	}
	if utf8.RuneCountInString(w.text) > 9 {
		mult += 0.3
	}
	if next == nil || next.para != w.para {
		mult = max(mult, 2.5)
	}
	return time.Duration(base * mult)
}
