package tui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
)

var (
	pttStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#ebcb8b")).Bold(true)
	barStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#d8dee9"))
	barDimStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#4c566a"))
)

type statusBar struct {
	currentFile string
	fileIndex   int
	totalFiles  int
	reviewed    int
	speaking    bool
	listening   bool
	waitingPTT  bool
	analyzing   bool
	batchGroup  string
	width       int
}

func newStatusBar() statusBar {
	return statusBar{analyzing: true} // start in analyzing state
}

func (s *statusBar) View() string {
	// Left: file info or status
	var left string
	if s.analyzing {
		left = pttStyle.Render(" ⏳ Claude is analyzing your changes...")
	} else if s.currentFile != "" {
		groupPrefix := ""
		if s.batchGroup != "" {
			groupPrefix = s.batchGroup + " > "
		}
		left = statusFileStyle.Render(
			fmt.Sprintf(" [%d/%d] %s%s", s.fileIndex+1, s.totalFiles, groupPrefix, s.currentFile),
		)
	} else if s.totalFiles > 0 {
		left = barStyle.Render(" Ready")
	} else {
		left = barDimStyle.Render(" Waiting for session...")
	}

	// Middle: progress
	var progress string
	if s.totalFiles > 0 {
		progress = statusProgressStyle.Render(
			fmt.Sprintf(" %d/%d reviewed ", s.reviewed, s.totalFiles),
		)
	}

	// Right: indicator or help
	var right string
	if s.waitingPTT {
		right = pttStyle.Render(" ⎵ TALK  n:NEXT ")
	} else if s.listening {
		right = listeningStyle.Render(" ● REC — SPACE TO STOP ")
	} else if s.speaking {
		right = speakingStyle.Render(" ♪ SPEAKING ")
	} else {
		right = barDimStyle.Render(" tab:panels e:edit j/k:scroll q:quit ")
	}

	// Compose without background fill — just the content
	gap := s.width - lipgloss.Width(left) - lipgloss.Width(progress) - lipgloss.Width(right)
	if gap < 0 {
		gap = 0
	}

	return left + lipgloss.NewStyle().Width(gap).Render("") + progress + right
}
