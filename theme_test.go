package main

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestThemeIndex(t *testing.T) {
	if got := themeIndex("Default"); got != 0 {
		t.Errorf("Default index = %d, want 0", got)
	}
	if got := themeIndex("Nord"); got != 1 {
		t.Errorf("Nord index = %d, want 1", got)
	}
	if got := themeIndex("Nonexistent"); got != 0 {
		t.Errorf("unknown theme index = %d, want 0 (Default)", got)
	}
}

// TestThemesComplete guards against a typo'd theme literal leaving a color
// blank (which would render as the terminal default and look broken).
func TestThemesComplete(t *testing.T) {
	for _, th := range themes {
		if th.name == "" {
			t.Error("theme with empty name")
		}
		fields := map[string]string{
			"text": th.text, "bright": th.bright, "dim": th.dim, "muted": th.muted,
			"code": th.code, "link": th.link, "accent": th.accent, "border": th.border,
			"cursorBG": th.cursorBG, "selBG": th.selBG, "focusFG": th.focusFG, "focusBG": th.focusBG,
		}
		for label, v := range fields {
			if v == "" {
				t.Errorf("theme %q missing %s", th.name, label)
			}
		}
	}
}

func TestSetTheme(t *testing.T) {
	doc := writeDoc(t, "c.md", "alpha beta")
	m := newModel(doc)
	if m.themeIdx != 0 {
		t.Fatalf("default themeIdx = %d, want 0", m.themeIdx)
	}

	m.setTheme(themeIndex("Nord"))
	if m.themeIdx != 1 {
		t.Errorf("setTheme(Nord) idx = %d, want 1", m.themeIdx)
	}
	if m.st.cursorLine != lipgloss.Color(themes[1].cursorBG) {
		t.Errorf("setTheme did not rebuild styles: cursorLine = %v", m.st.cursorLine)
	}

	// Out-of-range indices clamp.
	m.setTheme(999)
	if m.themeIdx != len(themes)-1 {
		t.Errorf("setTheme(999) idx = %d, want %d", m.themeIdx, len(themes)-1)
	}
	m.setTheme(-5)
	if m.themeIdx != 0 {
		t.Errorf("setTheme(-5) idx = %d, want 0", m.themeIdx)
	}
}

func TestThemePickerNavigation(t *testing.T) {
	doc := writeDoc(t, "c.md", "alpha beta gamma")
	m := newModel(doc)
	m.width, m.height = 80, 24

	press := func(k string) {
		next, _ := m.Update(keyMsg(k))
		m = next.(model)
	}

	press("t")
	if !m.themePicker {
		t.Fatal("t should open the theme picker")
	}
	press("j")
	if m.themeIdx != 1 {
		t.Errorf("j -> themeIdx %d, want 1", m.themeIdx)
	}
	press("l") // document movement must not leak through the picker
	if m.idx != 0 {
		t.Errorf("movement leaked through picker: idx = %d", m.idx)
	}
	press("k")
	if m.themeIdx != 0 {
		t.Errorf("k -> themeIdx %d, want 0", m.themeIdx)
	}
	press("esc")
	if m.themePicker {
		t.Error("esc should close the theme picker")
	}
}

func TestThemePickerRendersAllThemes(t *testing.T) {
	doc := writeDoc(t, "v.md", "# Heading\n\nalpha beta gamma delta epsilon zeta\n")
	for i := range themes {
		m := newModel(doc)
		m.width, m.height = 80, 20
		m.idx = 3
		m.setTheme(i)
		m.themePicker = true
		_ = m.View() // must not panic for any theme
	}
}

// TestFocalWordKeepsItsBackground guards the regression where the cursor-line
// bar painted over the focal word's highlight, leaving it the same color as
// the bar (e.g. Default's 236-on-236, invisible).
func TestFocalWordKeepsItsBackground(t *testing.T) {
	r := lipgloss.DefaultRenderer()
	prev := r.ColorProfile()
	defer r.SetColorProfile(prev)
	r.SetColorProfile(0) // TrueColor: keep 256-index escapes for inspection

	doc := writeDoc(t, "v.md", "alpha beta gamma delta")
	m := newModel(doc)
	m.width, m.height = 40, 9
	m.idx = 2

	out := m.View()
	// Default theme: focal word is focusFG(236) on focusBG(252). The bar is
	// cursorLineBG(236); the focusBG must survive.
	if !strings.Contains(out, "48;5;252") {
		t.Errorf("focal word lost its highlight background (focusBG 252):\n%s", out)
	}
}

func TestThemePersistence(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	s := loadState()
	s.Theme = "Dracula"
	if err := s.save(); err != nil {
		t.Fatal(err)
	}
	if got := loadState().Theme; got != "Dracula" {
		t.Errorf("persisted theme = %q, want Dracula", got)
	}
}
