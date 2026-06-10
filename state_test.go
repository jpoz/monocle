package main

import "testing"

func TestStateRoundTrip(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	s := loadState()
	if len(s.Docs) != 0 {
		t.Fatalf("fresh state not empty: %+v", s)
	}

	s.setProgress("/tmp/doc.md", 42, 100)
	if err := s.save(); err != nil {
		t.Fatal(err)
	}

	got := loadState()
	if got.resumeIndex("/tmp/doc.md", 100) != 42 {
		t.Errorf("resumeIndex = %d, want 42", got.resumeIndex("/tmp/doc.md", 100))
	}
}

func TestResumeIndexRestarts(t *testing.T) {
	s := &state{Docs: map[string]docState{}}
	s.setProgress("doc", 42, 100)

	if got := s.resumeIndex("unknown", 100); got != 0 {
		t.Errorf("unknown doc: resumeIndex = %d, want 0", got)
	}
	if got := s.resumeIndex("doc", 80); got != 0 {
		t.Errorf("changed doc: resumeIndex = %d, want 0", got)
	}
	s.setProgress("doc", 99, 100)
	if got := s.resumeIndex("doc", 100); got != 0 {
		t.Errorf("finished doc: resumeIndex = %d, want 0", got)
	}
}
