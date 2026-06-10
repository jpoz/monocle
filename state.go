package main

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// state persists per-document reading progress and display preferences in
// ~/.config/monocle/state.json (honoring $XDG_CONFIG_HOME).
type state struct {
	// Style is a pointer so a missing key keeps the defaults rather than
	// reading as all-off.
	Style *styleConfig        `json:"style,omitempty"`
	Docs  map[string]docState `json:"docs"`
}

type docState struct {
	Word    int       `json:"word"`
	Words   int       `json:"words"`
	Updated time.Time `json:"updated"`
}

// configPath returns the path to a file in monocle's config directory,
// ~/.config/monocle (honoring $XDG_CONFIG_HOME).
func configPath(name string) (string, error) {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "monocle", name), nil
}

func statePath() (string, error) {
	return configPath("state.json")
}

// loadState returns the saved state, or an empty one if none exists yet.
func loadState() *state {
	s := &state{Docs: map[string]docState{}}
	path, err := statePath()
	if err != nil {
		return s
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return s
	}
	if json.Unmarshal(data, s) != nil || s.Docs == nil {
		s.Docs = map[string]docState{}
	}
	return s
}

func (s *state) save() error {
	path, err := statePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	// Write via a temp file so a crash mid-write can't corrupt the state.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// resumeIndex returns the saved position for a document, restarting from the
// beginning if the document was finished or has changed size since.
func (s *state) resumeIndex(key string, totalWords int) int {
	d, ok := s.Docs[key]
	if !ok || d.Words != totalWords || d.Word >= totalWords-1 || d.Word < 0 {
		return 0
	}
	return d.Word
}

func (s *state) setProgress(key string, idx, totalWords int) {
	s.Docs[key] = docState{Word: idx, Words: totalWords, Updated: time.Now()}
}

// docKey identifies a document across runs by its absolute path.
func docKey(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved
	} else if !errors.Is(err, fs.ErrNotExist) {
		return abs
	}
	return abs
}
