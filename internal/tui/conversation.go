package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type conversationEntry struct {
	text   string
	source string
}

type conversationView struct {
	entries []conversationEntry
	width   int
	height  int
}

func newConversationView() conversationView {
	return conversationView{}
}

func (c *conversationView) AddMessage(text, source string) {
	c.entries = append(c.entries, conversationEntry{text: text, source: source})
}

func (c *conversationView) View() string {
	contentHeight := c.height - 2
	if contentHeight < 1 {
		contentHeight = 1
	}

	maxWidth := c.width - 6 // border + padding

	var lines []string
	for _, entry := range c.entries {
		var prefix string
		var prefixStyle lipgloss.Style
		var textStyle lipgloss.Style

		switch entry.source {
		case "claude":
			prefix = "Claude"
			prefixStyle = claudeStyle.Bold(true)
			textStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#d8dee9"))
		case "user":
			prefix = "You"
			prefixStyle = userStyle.Bold(true)
			textStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#d8dee9"))
		default:
			prefix = "System"
			prefixStyle = systemStyle
			textStyle = systemStyle
		}

		// Render prefix on its own line
		lines = append(lines, prefixStyle.Render(prefix+":"))

		// Word-wrap the body text
		wrapped := wrapText(entry.text, maxWidth)
		for _, line := range wrapped {
			lines = append(lines, textStyle.Render(line))
		}

		// Visual separator between entries
		sep := lipgloss.NewStyle().Foreground(lipgloss.Color("#3b4252")).Render(strings.Repeat("─", maxWidth))
		lines = append(lines, sep)
	}

	// Show only the last N lines that fit (auto-scroll to bottom)
	if len(lines) > contentHeight {
		lines = lines[len(lines)-contentHeight:]
	}

	// Pad remaining
	for len(lines) < contentHeight {
		lines = append(lines, "")
	}

	content := strings.Join(lines, "\n")
	title := titleStyle.Render(" Conversation ")

	return panelBorder.
		Width(c.width - 2).
		Height(c.height - 2).
		Render(title + "\n" + content)
}

func wrapText(text string, maxWidth int) []string {
	if maxWidth <= 0 {
		return []string{text}
	}

	words := strings.Fields(text)
	if len(words) == 0 {
		return []string{""}
	}

	var lines []string
	current := words[0]
	for _, word := range words[1:] {
		if len(current)+1+len(word) > maxWidth {
			lines = append(lines, current)
			current = word
		} else {
			current = fmt.Sprintf("%s %s", current, word)
		}
	}
	lines = append(lines, current)
	return lines
}
