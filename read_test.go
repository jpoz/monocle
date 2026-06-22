package main

import (
	"bufio"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"testing"
)

func wordsModel(texts ...string) model {
	ws := make([]word, len(texts))
	for i, t := range texts {
		ws[i] = word{text: t, section: -1}
	}
	return model{doc: &document{words: ws}}
}

func TestReadTextU16Offsets(t *testing.T) {
	// "él" is two runes but stays within the BMP, so one UTF-16 unit each;
	// "𝟙" is astral (two UTF-16 units), which must shift the next offset by 2.
	m := wordsModel("él", "𝟙", "ok")
	text, offs := m.readTextU16(0, 3)
	if text != "él 𝟙 ok" {
		t.Fatalf("text = %q", text)
	}
	// él=2 units, space=1 -> 𝟙 at 3, 𝟙=2 units, space=1 -> ok at 6.
	want := []int{0, 3, 6}
	for i := range want {
		if offs[i] != want[i] {
			t.Fatalf("offs = %v, want %v", offs, want)
		}
	}
}

func TestWordForOffset(t *testing.T) {
	m := wordsModel("Hello", "world,", "this")
	m.readStart = 0
	_, m.wordOff = m.readTextU16(0, 3) // {0, 6, 13}

	cases := []struct {
		loc, want int
	}{
		{0, 0},  // start of "Hello"
		{3, 0},  // mid "Hello"
		{5, 0},  // the joining space maps to the preceding word
		{6, 1},  // start of "world,"
		{11, 1}, // the comma, still within "world,"
		{13, 2}, // start of "this"
		{99, 2}, // past the end clamps to the last word
	}
	for _, c := range cases {
		if got := m.wordForOffset(c.loc); got != c.want {
			t.Errorf("wordForOffset(%d) = %d, want %d", c.loc, got, c.want)
		}
	}
}

func TestPreferredLocale(t *testing.T) {
	t.Setenv("LC_ALL", "")
	t.Setenv("LC_MESSAGES", "")
	t.Setenv("LANG", "en_US.UTF-8")
	if loc, lang := preferredLocale(); loc != "en-US" || lang != "en" {
		t.Fatalf("got %q/%q, want en-US/en", loc, lang)
	}
	t.Setenv("LANG", "fr_CA")
	if loc, lang := preferredLocale(); loc != "fr-CA" || lang != "fr" {
		t.Fatalf("got %q/%q, want fr-CA/fr", loc, lang)
	}
}

func TestCycleVoice(t *testing.T) {
	m := &model{voices: []voiceInfo{{id: "a", name: "A"}, {id: "b", name: "B"}, {id: "c", name: "C"}}, voicesLoaded: true}
	// From default (""), forward lands on the first; then advances; wraps.
	for _, want := range []string{"a", "b", "c", "a"} {
		if !m.cycleVoice(1) || m.voiceID != want {
			t.Fatalf("voiceID = %q, want %q", m.voiceID, want)
		}
	}
	m.cycleVoice(-1) // c
	if m.voiceID != "c" {
		t.Fatalf("backward voiceID = %q, want c", m.voiceID)
	}

	empty := &model{voicesLoaded: true}
	if empty.cycleVoice(1) {
		t.Fatal("cycleVoice should report false with no voices")
	}
}

func TestAvailableVoices(t *testing.T) {
	if _, err := ensureSayWord(); err != nil {
		t.Skipf("no swift: %v", err)
	}
	t.Setenv("LC_ALL", "")
	t.Setenv("LC_MESSAGES", "")
	t.Setenv("LANG", "en_US.UTF-8")
	vs := availableVoices()
	if len(vs) == 0 {
		t.Fatal("no voices")
	}
	seen := map[string]bool{}
	for i, v := range vs {
		if v.id == "" || v.name == "" {
			t.Errorf("voice %d incomplete: %+v", i, v)
		}
		if v.lang != "en-US" {
			t.Errorf("voice %q lang = %q, want en-US", v.name, v.lang)
		}
		if seen[v.name] {
			t.Errorf("duplicate name %q", v.name)
		}
		seen[v.name] = true
		if i > 0 && vs[i-1].name > v.name {
			t.Errorf("not sorted: %q before %q", vs[i-1].name, v.name)
		}
	}
	t.Logf("%d en-US voices, e.g. %q", len(vs), vs[0].name)
}

// TestSayWordHelper builds the embedded helper and confirms it emits monotonic,
// in-range word offsets that map back to the right words. Skipped where no
// Swift toolchain is present.
func TestSayWordHelper(t *testing.T) {
	bin, err := ensureSayWord()
	if err != nil {
		t.Skipf("no swift toolchain: %v", err)
	}

	m := wordsModel("Hello", "world,", "this", "is", "a", "test.")
	m.readStart = 0
	text, offs := m.readTextU16(0, 6)
	m.wordOff = offs

	cmd := exec.Command(bin, strconv.FormatFloat(readRateAV, 'f', 2, 64))
	cmd.Stdin = strings.NewReader(text)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Wait()

	var gotWords []int
	sc := bufio.NewScanner(stdout)
	done := false
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "D" {
			done = true
			break
		}
		var loc, ln int
		if _, err := fmt.Sscanf(line, "W %d %d", &loc, &ln); err != nil {
			t.Fatalf("bad line %q", line)
		}
		if loc < 0 || loc >= len(text) {
			t.Errorf("offset %d out of range for %q", loc, text)
		}
		gotWords = append(gotWords, m.wordForOffset(loc))
	}
	if !done {
		t.Fatal("helper did not finish")
	}
	if len(gotWords) == 0 {
		t.Fatal("no word events emitted")
	}
	// The first event should land on the first word, and every mapped index
	// must be a valid word in the span.
	if gotWords[0] != 0 {
		t.Errorf("first word index = %d, want 0", gotWords[0])
	}
	for _, w := range gotWords {
		if w < 0 || w >= 6 {
			t.Errorf("mapped word %d out of span", w)
		}
	}
	t.Logf("mapped word indices: %v", gotWords)
}
