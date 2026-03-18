package tui

import (
	"bytes"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
)

// highlightCode uses chroma to syntax-highlight a block of code lines.
// Returns a map of line index -> ANSI-colored string.
func highlightCode(lines []string, filename string) map[int]string {
	result := make(map[int]string)
	if len(lines) == 0 {
		return result
	}

	lexer := lexers.Match(filename)
	if lexer == nil {
		lexer = lexers.Analyse(strings.Join(lines, "\n"))
	}
	if lexer == nil {
		return result
	}
	lexer = chroma.Coalesce(lexer)

	source := strings.Join(lines, "\n")
	iterator, err := lexer.Tokenise(nil, source)
	if err != nil {
		return result
	}

	style := styles.Get("nord")
	formatter := formatters.Get("terminal256")

	var buf bytes.Buffer
	if err := formatter.Format(&buf, style, iterator); err != nil {
		return result
	}

	highlighted := strings.Split(buf.String(), "\n")
	for i, h := range highlighted {
		if i < len(lines) {
			// Strip trailing reset if present
			h = strings.TrimRight(h, " ")
			result[i] = h
		}
	}

	return result
}
