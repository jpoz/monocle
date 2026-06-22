package main

import (
	"bufio"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
)

// sayWordSrc is the Swift word-boundary helper, compiled on first use.
//
//go:embed sayword.swift
var sayWordSrc string

// readRate is the words-per-minute handed to `say` on the fallback path.
const readRate = 185

// readRateAV is the helper's utterance rate on AVSpeechUtterance's 0...1 scale,
// where ~0.5 is a natural reading pace.
const readRateAV = 0.5

// sayWordMsg reports that the synthesizer is now speaking the word at UTF-16
// offset loc in the section text; the highlight jumps to the matching word.
type sayWordMsg struct {
	gen int
	loc int
}

// sayTickMsg advances the highlight by one word on the estimate-based fallback
// path. gen guards against ticks left over from a read that was already stopped.
type sayTickMsg struct{ gen int }

// sayDoneMsg fires when the speech finishes (the helper exits or `say` returns).
type sayDoneMsg struct{ gen int }

// sectionRange returns the [start,end) word span of the section enclosing word
// i: a heading-delimited block, or the untitled preamble before the first
// heading.
func (m model) sectionRange(i int) (start, end int) {
	sec := m.doc.words[i].section
	if sec < 0 {
		if len(m.doc.sections) > 0 {
			return 0, m.doc.sections[0].start
		}
		return 0, len(m.doc.words)
	}
	start = m.doc.sections[sec].start
	if sec+1 < len(m.doc.sections) {
		end = m.doc.sections[sec+1].start
	} else {
		end = len(m.doc.words)
	}
	return start, end
}

// readText is the plain prose of words [start,end), for handing to `say`.
func (m model) readText(start, end int) string {
	var b strings.Builder
	for i := start; i < end; i++ {
		if i > start {
			b.WriteByte(' ')
		}
		b.WriteString(m.doc.words[i].text)
	}
	return b.String()
}

// readTextU16 builds the same prose as readText and, alongside it, the UTF-16
// start offset of each word within that string — the unit the synthesizer
// reports word ranges in, so the helper's offsets map straight back to words.
func (m model) readTextU16(start, end int) (string, []int) {
	var b strings.Builder
	offs := make([]int, 0, end-start)
	u16 := 0
	for i := start; i < end; i++ {
		if i > start {
			b.WriteByte(' ')
			u16++ // the joining space is one UTF-16 code unit
		}
		offs = append(offs, u16)
		t := m.doc.words[i].text
		b.WriteString(t)
		u16 += len(utf16.Encode([]rune(t)))
	}
	return b.String(), offs
}

// startReading speaks from the focal word to the end of its section, advancing
// the highlight in exact step with the speech. It prefers the Swift helper,
// whose word-boundary callbacks track the spoken word precisely, and falls back
// to the timing estimate when no Swift toolchain is available.
func (m model) startReading() (tea.Model, tea.Cmd) {
	_, end := m.sectionRange(m.idx)
	from := m.idx
	if end <= from {
		return m, nil
	}

	bin, err := ensureSayWord()
	if err != nil {
		return m.startReadingEstimate(from, end)
	}
	m.ensureVoices()

	text, offs := m.readTextU16(from, end)
	args := []string{strconv.FormatFloat(readRateAV, 'f', 2, 64)}
	if m.voiceID != "" {
		args = append(args, m.voiceID)
	}
	cmd := exec.Command(bin, args...)
	cmd.Stdin = strings.NewReader(text)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return m.startReadingEstimate(from, end)
	}
	if err := cmd.Start(); err != nil {
		m.flash = "Read aloud failed: " + err.Error()
		return m, nil
	}

	m.stopProc() // kill a lingering tail from a prior read
	m.readGen++
	m.reading = true
	m.readStart = from
	m.readEnd = end
	m.wordOff = offs
	m.sayProc = cmd
	m.sayOut = bufio.NewReader(stdout)
	m.selecting = false

	return m, m.readWordCmd(m.readGen)
}

// readWordCmd blocks on the next line from the helper and turns it into the
// matching message: a word advance, or done on the terminating "D"/EOF. It is
// re-issued after each word so the stream drives the highlight one event at a
// time, mirroring how the fallback tick re-arms itself. The closure reaps the
// process on the terminating read, so the stream is fully consumed first.
func (m model) readWordCmd(gen int) tea.Cmd {
	r, proc := m.sayOut, m.sayProc
	return func() tea.Msg {
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				if proc != nil {
					_ = proc.Wait()
				}
				return sayDoneMsg{gen}
			}
			line = strings.TrimSpace(line)
			switch {
			case line == "":
				continue
			case line[0] == 'D':
				if proc != nil {
					_ = proc.Wait()
				}
				return sayDoneMsg{gen}
			case line[0] == 'W':
				var loc, ln int
				if _, err := fmt.Sscanf(line, "W %d %d", &loc, &ln); err == nil {
					return sayWordMsg{gen, loc}
				}
			}
		}
	}
}

// wordForOffset maps a UTF-16 offset reported by the helper to a word index:
// the word whose span contains it, i.e. the last word starting at or before the
// offset. This collapses the engine's own re-segmentation — grouped
// abbreviations like "Dr. Smith", or "$1996." reported after "1996" — onto a
// single word rather than chasing boundaries we never split on.
func (m model) wordForOffset(loc int) int {
	k := max(sort.Search(len(m.wordOff), func(i int) bool { return m.wordOff[i] > loc })-1, 0)
	return m.readStart + k
}

// ensureSayWord compiles the embedded Swift helper to a cached binary keyed by
// the source hash (so a changed helper recompiles) and returns its path. It
// fails when no Swift toolchain is present, which sends startReading to the
// fallback.
func ensureSayWord() (string, error) {
	if _, err := exec.LookPath("swiftc"); err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(sayWordSrc))
	tag := hex.EncodeToString(sum[:])[:8]
	dir := filepath.Join(cacheDir(), "monocle")
	bin := filepath.Join(dir, "sayword-"+tag)
	if _, err := os.Stat(bin); err == nil {
		return bin, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	src := bin + ".swift"
	if err := os.WriteFile(src, []byte(sayWordSrc), 0o644); err != nil {
		return "", err
	}
	defer os.Remove(src)
	if out, err := exec.Command("swiftc", "-O", "-suppress-warnings", "-o", bin, src).CombinedOutput(); err != nil {
		return "", fmt.Errorf("swiftc: %v: %s", err, out)
	}
	return bin, nil
}

func cacheDir() string {
	if d, err := os.UserCacheDir(); err == nil {
		return d
	}
	return os.TempDir()
}

// voiceInfo is one installed speech voice the user can read with.
type voiceInfo struct {
	id      string
	name    string
	lang    string
	quality int // AVSpeechSynthesisVoiceQuality: 1 default, 2 enhanced, 3 premium
}

// ensureVoices populates the cyclable voice list on first use, memoizing the
// result (including an empty list, so a host with no helper isn't re-probed).
func (m *model) ensureVoices() {
	if m.voicesLoaded {
		return
	}
	m.voices = availableVoices()
	m.voicesLoaded = true
}

// cycleVoice moves the selected voice by delta within the available set,
// loading the set on first use. It reports whether any voices were available.
func (m *model) cycleVoice(delta int) bool {
	m.ensureVoices()
	n := len(m.voices)
	if n == 0 {
		return false
	}
	cur := -1
	for i, v := range m.voices {
		if v.id == m.voiceID {
			cur = i
			break
		}
	}
	m.voiceID = m.voices[((cur+delta)%n+n)%n].id
	return true
}

// voiceName is the display name of the selected voice, "default" when none is
// chosen, falling back to the identifier's last segment if it isn't in the
// loaded set.
func (m model) voiceName() string {
	if m.voiceID == "" {
		return "default"
	}
	for _, v := range m.voices {
		if v.id == m.voiceID {
			return v.name
		}
	}
	if i := strings.LastIndex(m.voiceID, "."); i >= 0 {
		return m.voiceID[i+1:]
	}
	return m.voiceID
}

// availableVoices lists the installed voices via the helper, narrowed to the
// user's locale (then language, then all), deduped by name keeping the highest
// quality, and sorted by name for a stable cycling order.
func availableVoices() []voiceInfo {
	bin, err := ensureSayWord()
	if err != nil {
		return nil
	}
	out, err := exec.Command(bin, "--voices").Output()
	if err != nil {
		return nil
	}

	var all []voiceInfo
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		f := strings.Split(line, "\t")
		if len(f) < 3 {
			continue
		}
		v := voiceInfo{id: f[0], name: f[1], lang: f[2]}
		if len(f) > 3 {
			v.quality, _ = strconv.Atoi(f[3])
		}
		all = append(all, v)
	}

	locale, lang := preferredLocale()
	vs := pickFrom(all, func(v voiceInfo) bool { return locale != "" && v.lang == locale })
	if len(vs) == 0 {
		vs = pickFrom(all, func(v voiceInfo) bool { return lang != "" && (v.lang == lang || strings.HasPrefix(v.lang, lang+"-")) })
	}
	if len(vs) == 0 {
		vs = all
	}

	// Drop the legacy MacinTalk voices (the novelty set — Zarvox, Boing, … — and
	// the ancient robotic ones), which are useless for reading prose. Modern
	// voices live under com.apple.voice.* / com.apple.ttsbundle.*. Keep them only
	// as a last resort, if nothing else is installed.
	if real := pickFrom(vs, func(v voiceInfo) bool { return !strings.HasPrefix(v.id, legacyVoicePrefix) }); len(real) > 0 {
		vs = real
	}

	// The Eloquence voices (Eddy, Flo, Grandma, …) are one formant engine with
	// preset tweaks; they sound nearly identical, so switching between them feels
	// like nothing changed. Collapse the family to a single representative.
	elo := false
	kept := make([]voiceInfo, 0, len(vs))
	for _, v := range vs {
		if strings.HasPrefix(v.id, eloquencePrefix) {
			if elo {
				continue
			}
			elo = true
		}
		kept = append(kept, v)
	}
	vs = kept

	best := map[string]voiceInfo{}
	for _, v := range vs {
		if b, ok := best[v.name]; !ok || v.quality > b.quality {
			best[v.name] = v
		}
	}
	result := make([]voiceInfo, 0, len(best))
	for _, v := range best {
		result = append(result, v)
	}
	// Best quality first (so downloaded enhanced/premium voices lead the cycle),
	// then by name for a stable order within a quality tier.
	sort.Slice(result, func(i, j int) bool {
		if result[i].quality != result[j].quality {
			return result[i].quality > result[j].quality
		}
		return result[i].name < result[j].name
	})
	return result
}

// legacyVoicePrefix is the identifier namespace of the old MacinTalk voices.
const legacyVoicePrefix = "com.apple.speech.synthesis.voice."

// eloquencePrefix is the namespace of the Eloquence formant voices, which are
// one engine with preset variations that sound nearly the same.
const eloquencePrefix = "com.apple.eloquence."

// pickFrom returns the voices in vs matching keep.
func pickFrom(vs []voiceInfo, keep func(voiceInfo) bool) []voiceInfo {
	var out []voiceInfo
	for _, v := range vs {
		if keep(v) {
			out = append(out, v)
		}
	}
	return out
}

// preferredLocale derives the user's locale (e.g. "en-US") and language
// ("en") from the environment, for narrowing the voice list.
func preferredLocale() (locale, lang string) {
	l := os.Getenv("LANG")
	if l == "" {
		l = os.Getenv("LC_ALL")
	}
	if l == "" {
		l = os.Getenv("LC_MESSAGES")
	}
	l, _, _ = strings.Cut(l, ".")       // strip ".UTF-8"
	l = strings.ReplaceAll(l, "_", "-") // en_US -> en-US
	lang, _, _ = strings.Cut(l, "-")    // en-US -> en
	return l, lang
}

// startReadingEstimate is the fallback reader for hosts without a Swift
// toolchain: it speaks from the focal word with `say` and advances the
// highlight on a timer that estimates each word's spoken duration. The estimate
// can't honor a chosen voice, so voice switching is a no-op on this path.
func (m model) startReadingEstimate(from, end int) (tea.Model, tea.Cmd) {
	if _, err := exec.LookPath("say"); err != nil {
		m.flash = "Read aloud needs the macOS `say` command"
		return m, nil
	}

	cmd := exec.Command("say", "-r", strconv.Itoa(readRate), m.readText(from, end))
	if err := cmd.Start(); err != nil {
		m.flash = "Read aloud failed: " + err.Error()
		return m, nil
	}

	m.stopProc()
	m.readGen++
	m.reading = true
	m.readEnd = end
	m.readAvg = m.avgWordLen(from, end)
	m.sayProc = cmd
	m.selecting = false

	gen := m.readGen
	wait := func() tea.Msg { cmd.Wait(); return sayDoneMsg{gen} }
	return m, tea.Batch(wait, m.tickCmd(gen))
}

// avgWordLen is the mean rune length of words [start,end), used to normalize
// per-word dwell so the highlight's average pace matches `say`'s rate.
func (m model) avgWordLen(start, end int) float64 {
	total := 0
	for i := start; i < end; i++ {
		total += utf8.RuneCountInString(m.doc.words[i].text)
	}
	if n := end - start; n > 0 {
		return float64(total) / float64(n)
	}
	return 1
}

// tickCmd schedules the next highlight advance after the current word's dwell.
func (m model) tickCmd(gen int) tea.Cmd {
	return tea.Tick(m.wordDwell(m.idx), func(time.Time) tea.Msg {
		return sayTickMsg{gen}
	})
}

// wordDwell is how long the highlight rests on word i: proportional to its
// length (longer words take longer to speak), plus a beat for sentence- and
// clause-ending punctuation, mirroring `say`'s own phrasing pauses.
func (m model) wordDwell(i int) time.Duration {
	base := 60.0 / float64(readRate) // seconds for an average-length word
	avg := m.readAvg
	if avg < 1 {
		avg = 1
	}
	n := float64(utf8.RuneCountInString(m.doc.words[i].text))
	secs := base * n / avg
	switch {
	case endsSentence(m.doc.words[i].text):
		secs += 0.32
	case endsClause(m.doc.words[i].text):
		secs += 0.14
	}
	return time.Duration(secs * float64(time.Second))
}

// trailingPunct is the closing punctuation trimmed before inspecting the final
// meaningful rune of a word (so `done."` still reads as sentence-ending).
const trailingPunct = "\"')]}»”’"

func endsSentence(s string) bool {
	s = strings.TrimRight(s, trailingPunct)
	if s == "" {
		return false
	}
	r, _ := utf8.DecodeLastRuneInString(s)
	return r == '.' || r == '!' || r == '?'
}

func endsClause(s string) bool {
	s = strings.TrimRight(s, trailingPunct)
	if s == "" {
		return false
	}
	r, _ := utf8.DecodeLastRuneInString(s)
	return r == ',' || r == ';' || r == ':' || r == '—'
}

// stopReading halts the read-aloud and silences any in-flight speech. Bumping
// the generation makes pending word/tick messages and the wait goroutine's done
// message no-ops.
func (m *model) stopReading() {
	m.reading = false
	m.readGen++
	m.sayOut = nil
	m.stopProc()
}

// stopProc kills the running speech process, if any.
func (m *model) stopProc() {
	if m.sayProc != nil && m.sayProc.Process != nil {
		_ = m.sayProc.Process.Kill()
	}
	m.sayProc = nil
}
