package tui

import "github.com/charmbracelet/lipgloss"

var (
	// Diff panel styles — subtle background tints
	addedStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#d8dee9")).Background(lipgloss.Color("#1a2b1a")) // very subtle green bg
	addedGutter   = lipgloss.NewStyle().Foreground(lipgloss.Color("#a3be8c"))                                      // green gutter marker
	removedStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#d8dee9")).Background(lipgloss.Color("#2b1a1a")) // very subtle red bg
	removedGutter = lipgloss.NewStyle().Foreground(lipgloss.Color("#bf616a"))                                      // red gutter marker
	hunkStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#616e88"))                                      // dim for hunk headers
	diffHeader    = lipgloss.NewStyle().Foreground(lipgloss.Color("#ebcb8b")).Bold(true)                           // yellow bold

	// Highlight for scroll-to target line
	highlightedLine = lipgloss.NewStyle().Background(lipgloss.Color("#3b4252")).Bold(true) // visible but not harsh

	// Line number gutter
	lineNumStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#4c566a")).Width(4).Align(lipgloss.Right)

	// Syntax highlighting
	keywordStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#b48ead")) // purple
	stringStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#a3be8c")) // green
	commentStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#616e88")).Italic(true)
	funcStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#88c0d0")) // cyan
	typeStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#8fbcbb")) // teal

	// Conversation panel styles
	claudeStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#b48ead"))
	userStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#88c0d0"))
	systemStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#616e88"))

	// Status bar
	statusBarStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("#2e3440")).
			Foreground(lipgloss.Color("#d8dee9")).
			Padding(0, 1)

	statusFileStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#ebcb8b")).
			Bold(true)

	statusProgressStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#a3be8c"))

	// Indicators
	speakingStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#b48ead")).Bold(true)
	listeningStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#bf616a")).Bold(true)

	// Panel borders
	panelBorder = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#4c566a"))

	// Title
	titleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#eceff4")).
			Background(lipgloss.Color("#4c566a")).
			Padding(0, 1).
			Bold(true)
)
