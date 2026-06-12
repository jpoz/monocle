package main

import "github.com/charmbracelet/lipgloss"

// theme is a named palette of semantic colors. Every value is a color string
// lipgloss understands — an ANSI 256 index ("240") or a hex triple
// ("#88C0D0") — and degrades gracefully on terminals without truecolor.
type theme struct {
	name     string
	text     string // body text
	bright   string // headings and emphasized UI
	dim      string // dimmed (out-of-focus) text
	muted    string // markers, table grid, status bar
	code     string // code spans and blocks
	link     string // links
	accent   string // comment markers and gutter
	border   string // overlay borders
	cursorBG string // current-line background bar
	selBG    string // selection background
	focusFG  string // focal word foreground
	focusBG  string // focal word background
}

// themes is the ordered list shown in the theme picker. The first entry is the
// default and matches monocle's original look.
var themes = []theme{
	{
		name: "Default", text: "252", bright: "231", dim: "240", muted: "244",
		code: "179", link: "75", accent: "214", border: "242",
		cursorBG: "236", selBG: "24", focusFG: "236", focusBG: "252",
	},
	{
		name: "Nord", text: "#D8DEE9", bright: "#ECEFF4", dim: "#4C566A", muted: "#616E88",
		code: "#EBCB8B", link: "#88C0D0", accent: "#D08770", border: "#434C5E",
		cursorBG: "#3B4252", selBG: "#434C5E", focusFG: "#2E3440", focusBG: "#88C0D0",
	},
	{
		name: "Dracula", text: "#F8F8F2", bright: "#FFFFFF", dim: "#6272A4", muted: "#6272A4",
		code: "#F1FA8C", link: "#8BE9FD", accent: "#FF79C6", border: "#44475A",
		cursorBG: "#44475A", selBG: "#44475A", focusFG: "#282A36", focusBG: "#BD93F9",
	},
	{
		name: "Gruvbox Dark", text: "#EBDBB2", bright: "#FBF1C7", dim: "#7C6F64", muted: "#928374",
		code: "#FABD2F", link: "#83A598", accent: "#FE8019", border: "#504945",
		cursorBG: "#3C3836", selBG: "#504945", focusFG: "#282828", focusBG: "#FABD2F",
	},
	{
		name: "Solarized Dark", text: "#839496", bright: "#93A1A1", dim: "#586E75", muted: "#586E75",
		code: "#B58900", link: "#268BD2", accent: "#CB4B16", border: "#073642",
		cursorBG: "#073642", selBG: "#073642", focusFG: "#002B36", focusBG: "#2AA198",
	},
	{
		name: "Solarized Light", text: "#657B83", bright: "#586E75", dim: "#93A1A1", muted: "#93A1A1",
		code: "#B58900", link: "#268BD2", accent: "#CB4B16", border: "#93A1A1",
		cursorBG: "#EEE8D5", selBG: "#EEE8D5", focusFG: "#FDF6E3", focusBG: "#268BD2",
	},
	{
		name: "Tokyo Night", text: "#A9B1D6", bright: "#C0CAF5", dim: "#565F89", muted: "#565F89",
		code: "#E0AF68", link: "#7AA2F7", accent: "#FF9E64", border: "#292E42",
		cursorBG: "#292E42", selBG: "#283457", focusFG: "#1A1B26", focusBG: "#7AA2F7",
	},
	{
		name: "Catppuccin Mocha", text: "#CDD6F4", bright: "#FFFFFF", dim: "#6C7086", muted: "#6C7086",
		code: "#F9E2AF", link: "#89B4FA", accent: "#FAB387", border: "#45475A",
		cursorBG: "#313244", selBG: "#45475A", focusFG: "#1E1E2E", focusBG: "#CBA6F7",
	},
	{
		name: "One Dark", text: "#ABB2BF", bright: "#FFFFFF", dim: "#5C6370", muted: "#5C6370",
		code: "#E5C07B", link: "#61AFEF", accent: "#D19A66", border: "#3E4451",
		cursorBG: "#2C323C", selBG: "#3E4451", focusFG: "#282C34", focusBG: "#61AFEF",
	},
	{
		name: "Monokai", text: "#F8F8F2", bright: "#FFFFFF", dim: "#75715E", muted: "#75715E",
		code: "#E6DB74", link: "#66D9EF", accent: "#FD971F", border: "#49483E",
		cursorBG: "#3E3D32", selBG: "#49483E", focusFG: "#272822", focusBG: "#A6E22E",
	},
}

// themeIndex returns the index of the named theme, or 0 (Default) if unknown.
func themeIndex(name string) int {
	for i, t := range themes {
		if t.name == name {
			return i
		}
	}
	return 0
}

// styles holds every lipgloss style the view draws with, built from a theme.
type styles struct {
	read        lipgloss.Style
	head        lipgloss.Style
	focus       lipgloss.Style
	far         lipgloss.Style
	farHead     lipgloss.Style
	code        lipgloss.Style
	link        lipgloss.Style
	marker      lipgloss.Style
	grid        lipgloss.Style
	status      lipgloss.Style
	picker      lipgloss.Style
	pickerSel   lipgloss.Style
	commentMark lipgloss.Style
	cursorLine  lipgloss.Color
	selection   lipgloss.Color
}

// newStyles derives the full style set from a theme.
func newStyles(t theme) styles {
	c := func(s string) lipgloss.Color { return lipgloss.Color(s) }
	return styles{
		read:    lipgloss.NewStyle().Foreground(c(t.text)),
		head:    lipgloss.NewStyle().Foreground(c(t.bright)).Bold(true),
		focus:   lipgloss.NewStyle().Foreground(c(t.focusFG)).Background(c(t.focusBG)).Bold(true),
		far:     lipgloss.NewStyle().Foreground(c(t.dim)),
		farHead: lipgloss.NewStyle().Foreground(c(t.dim)).Bold(true),
		code:    lipgloss.NewStyle().Foreground(c(t.code)),
		link:    lipgloss.NewStyle().Foreground(c(t.link)).Underline(true),
		marker:  lipgloss.NewStyle().Foreground(c(t.muted)),
		grid:    lipgloss.NewStyle().Foreground(c(t.muted)),
		status:  lipgloss.NewStyle().Foreground(c(t.muted)),
		picker: lipgloss.NewStyle().Foreground(c(t.text)).
			Border(lipgloss.RoundedBorder()).BorderForeground(c(t.border)).
			Padding(0, 2),
		pickerSel:   lipgloss.NewStyle().Foreground(c(t.bright)).Bold(true),
		commentMark: lipgloss.NewStyle().Foreground(c(t.accent)),
		cursorLine:  c(t.cursorBG),
		selection:   c(t.selBG),
	}
}

// defaultStyles builds the style set for the first (Default) theme.
func defaultStyles() styles { return newStyles(themes[0]) }
