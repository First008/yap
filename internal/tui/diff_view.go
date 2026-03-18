package tui

import (
	"fmt"
	"strings"
)

type diffLine struct {
	raw    string
	lineNo int    // -1 for metadata/headers
	kind   string // "add", "del", "ctx", "hunk", "header", "meta"
}

type diffView struct {
	parsed        []diffLine
	highlights    map[int]string // chroma-highlighted versions of code lines
	offset        int
	width         int
	height        int
	filePath      string
	fileIndex     int
	totalFiles    int
	highlightLine int // line number to highlight (-1 = none)
}

func newDiffView() diffView {
	return diffView{}
}

func (d *diffView) SetContent(filePath, diff string, index, total int) {
	d.filePath = filePath
	d.fileIndex = index
	d.totalFiles = total
	d.offset = 0
	d.highlightLine = -1
	d.parsed = parseDiff(diff)

	// Build code lines for chroma highlighting
	var codeLines []string
	var codeIndices []int
	for i, dl := range d.parsed {
		if dl.kind == "add" || dl.kind == "del" || dl.kind == "ctx" {
			codeLines = append(codeLines, dl.raw)
			codeIndices = append(codeIndices, i)
		}
	}

	// Highlight all code lines as a block
	highlighted := highlightCode(codeLines, filePath)

	// Map highlights back to diff line indices
	d.highlights = make(map[int]string)
	for codeIdx, diffIdx := range codeIndices {
		if h, ok := highlighted[codeIdx]; ok {
			d.highlights[diffIdx] = h
		}
	}
}

func parseDiff(diff string) []diffLine {
	lines := strings.Split(diff, "\n")
	var result []diffLine
	lineNo := 0

	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "diff ") || strings.HasPrefix(line, "index "):
			result = append(result, diffLine{raw: line, lineNo: -1, kind: "meta"})
		case strings.HasPrefix(line, "---") || strings.HasPrefix(line, "+++"):
			result = append(result, diffLine{raw: line, lineNo: -1, kind: "header"})
		case strings.HasPrefix(line, "@@"):
			lineNo = parseHunkStart(line)
			result = append(result, diffLine{raw: line, lineNo: -1, kind: "hunk"})
		case strings.HasPrefix(line, "+"):
			lineNo++
			result = append(result, diffLine{raw: line[1:], lineNo: lineNo, kind: "add"})
		case strings.HasPrefix(line, "-"):
			result = append(result, diffLine{raw: line[1:], lineNo: -1, kind: "del"})
		default:
			if line != "" {
				lineNo++
			}
			result = append(result, diffLine{raw: line, lineNo: lineNo, kind: "ctx"})
		}
	}
	return result
}

func parseHunkStart(line string) int {
	_, after, found := strings.Cut(line, "+")
	if !found {
		return 0
	}
	n := 0
	for _, c := range after {
		if c >= '0' && c <= '9' {
			n = n*10 + int(c-'0')
		} else {
			break
		}
	}
	if n > 0 {
		return n - 1
	}
	return 0
}

// ScrollToLine scrolls so the given source line number is visible and highlighted.
func (d *diffView) ScrollToLine(lineNo int) {
	d.highlightLine = lineNo
	for i, dl := range d.parsed {
		if dl.lineNo >= lineNo {
			d.offset = i - d.height/3
			if d.offset < 0 {
				d.offset = 0
			}
			return
		}
	}
}

func (d *diffView) ScrollUp(n int) {
	d.offset -= n
	if d.offset < 0 {
		d.offset = 0
	}
}

func (d *diffView) ScrollDown(n int) {
	d.offset += n
	mx := len(d.parsed) - d.height + 2
	if mx < 0 {
		mx = 0
	}
	if d.offset > mx {
		d.offset = mx
	}
}

func (d *diffView) GoToTop() {
	d.offset = 0
}

func (d *diffView) GoToBottom() {
	mx := len(d.parsed) - d.height + 2
	if mx < 0 {
		mx = 0
	}
	d.offset = mx
}

func (d *diffView) View() string {
	if len(d.parsed) == 0 {
		return systemStyle.Render("No diff to display. Waiting for review session...")
	}

	contentHeight := max(d.height-2, 1)
	end := min(d.offset+contentHeight, len(d.parsed))
	codeWidth := d.width - 9

	var rendered []string
	for i := d.offset; i < end; i++ {
		rendered = append(rendered, d.renderLine(i, codeWidth))
	}

	for len(rendered) < contentHeight {
		rendered = append(rendered, "")
	}

	content := strings.Join(rendered, "\n")

	title := titleStyle.Render(" Diff ")
	if d.filePath != "" {
		title = titleStyle.Render(fmt.Sprintf(" %s [%d/%d] ", d.filePath, d.fileIndex+1, d.totalFiles))
	}

	return panelBorder.
		Width(d.width - 2).
		Height(d.height - 2).
		Render(title + "\n" + content)
}

func (d *diffView) renderLine(idx int, codeWidth int) string {
	dl := d.parsed[idx]
	isTarget := d.highlightLine > 0 && dl.lineNo == d.highlightLine

	// Use chroma-highlighted text if available, otherwise raw
	code := truncate(dl.raw, codeWidth)
	if h, ok := d.highlights[idx]; ok {
		code = truncate(stripTrailingReset(h), codeWidth)
	}

	var line string

	switch dl.kind {
	case "meta":
		line = systemStyle.Render(truncate(dl.raw, codeWidth))

	case "header":
		line = diffHeader.Render(truncate(dl.raw, codeWidth))

	case "hunk":
		line = hunkStyle.Render(truncate(dl.raw, codeWidth))

	case "add":
		gutter := lineNumStyle.Render(fmt.Sprintf("%d", dl.lineNo))
		marker := addedGutter.Render("+")
		line = gutter + " " + marker + " " + addedStyle.Render(code)

	case "del":
		gutter := lineNumStyle.Render(" ")
		marker := removedGutter.Render("-")
		line = gutter + " " + marker + " " + removedStyle.Render(code)

	default:
		if dl.lineNo > 0 {
			gutter := lineNumStyle.Render(fmt.Sprintf("%d", dl.lineNo))
			line = gutter + "   " + code
		} else {
			line = code
		}
	}

	// Apply highlight to the target scroll-to line
	if isTarget {
		line = highlightedLine.Render("▸ " + line)
	}

	return line
}

// stripTrailingReset removes trailing ANSI reset sequences that chroma adds.
func stripTrailingReset(s string) string {
	return strings.TrimRight(s, "\033[0m \n")
}

func truncate(s string, maxWidth int) string {
	if maxWidth <= 0 {
		return s
	}
	// For ANSI-colored strings, we can't just count runes.
	// Simple approach: truncate if the raw length is very long.
	runes := []rune(s)
	if len(runes) > maxWidth*3 { // rough limit accounting for ANSI codes
		return string(runes[:maxWidth*3])
	}
	return s
}
