package main

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

const (
	minWPM  = 60
	maxWPM  = 1500
	wpmStep = 25
)

type model struct {
	doc    *document
	idx    int
	wpm    int
	paused bool
	tickID int // invalidates in-flight ticks after seeks/pauses
	width  int
	height int
}

type tickMsg struct{ id int }

func newModel(doc *document, wpm int) model {
	return model{doc: doc, wpm: min(max(wpm, minWPM), maxWPM)}
}

func (m model) Init() tea.Cmd {
	// Brief lead-in so the reader can find the pivot before words start.
	id := m.tickID
	return tea.Tick(500*time.Millisecond, func(time.Time) tea.Msg { return tickMsg{id} })
}

func (m model) tick() tea.Cmd {
	w := m.doc.words[m.idx]
	var next *word
	if m.idx+1 < len(m.doc.words) {
		next = &m.doc.words[m.idx+1]
	}
	id := m.tickID
	return tea.Tick(wordDelay(w, next, m.wpm), func(time.Time) tea.Msg { return tickMsg{id} })
}

// reschedule cancels any in-flight tick and, when playing, arms a new one.
func (m *model) reschedule() tea.Cmd {
	m.tickID++
	if m.paused {
		return nil
	}
	return m.tick()
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case tickMsg:
		if msg.id != m.tickID || m.paused {
			return m, nil
		}
		if m.idx+1 >= len(m.doc.words) {
			m.paused = true
			return m, nil
		}
		m.idx++
		return m, m.tick()

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			return m, tea.Quit

		case " ", "space":
			m.paused = !m.paused
			if !m.paused && m.idx+1 >= len(m.doc.words) {
				m.idx = 0 // replay from the top when resumed at the end
			}
			return m, m.reschedule()

		case "right", "l":
			m.idx = min(m.idx+1, len(m.doc.words)-1)
			return m, m.reschedule()

		case "left", "h":
			m.idx = max(m.idx-1, 0)
			return m, m.reschedule()

		case "down", "j":
			p := m.doc.words[m.idx].para
			if p+1 < len(m.doc.paraStarts) {
				m.idx = m.doc.paraStarts[p+1]
			} else {
				m.idx = len(m.doc.words) - 1
			}
			return m, m.reschedule()

		case "up", "k":
			p := m.doc.words[m.idx].para
			// First press rewinds to the paragraph start; pressing again
			// from the start jumps to the previous paragraph.
			if start := m.doc.paraStarts[p]; m.idx > start {
				m.idx = start
			} else if p > 0 {
				m.idx = m.doc.paraStarts[p-1]
			}
			return m, m.reschedule()

		case "+", "=":
			m.wpm = min(m.wpm+wpmStep, maxWPM)
			return m, m.reschedule()

		case "-", "_":
			m.wpm = max(m.wpm-wpmStep, minWPM)
			return m, m.reschedule()
		}
	}
	return m, nil
}
