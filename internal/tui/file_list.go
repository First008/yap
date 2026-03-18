package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	fileListSelected   = lipgloss.NewStyle().Foreground(lipgloss.Color("#eceff4")).Background(lipgloss.Color("#4c566a")).Bold(true)
	fileListReviewed   = lipgloss.NewStyle().Foreground(lipgloss.Color("#a3be8c"))
	fileListPending    = lipgloss.NewStyle().Foreground(lipgloss.Color("#d8dee9"))
	fileListAdded      = lipgloss.NewStyle().Foreground(lipgloss.Color("#a3be8c"))
	fileListModified   = lipgloss.NewStyle().Foreground(lipgloss.Color("#ebcb8b"))
	fileListDeleted    = lipgloss.NewStyle().Foreground(lipgloss.Color("#bf616a"))
	groupHeaderStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#88c0d0")).Bold(true)
)

// scrollPadding is the number of rows reserved for borders/status below the file list viewport.
const scrollPadding = 3

type fileEntry struct {
	path     string
	status   string
	reviewed bool
	lines    int
}

type fileGroup struct {
	name  string
	files []fileEntry
}

type displayRow struct {
	isHeader bool
	text     string // for headers
	fileIdx  int    // index into flat files slice
}

type fileListView struct {
	files    []fileEntry
	groups   []fileGroup
	rows     []displayRow
	selected int
	width    int
	height   int
	offset   int
}

func newFileListView() fileListView {
	return fileListView{}
}

func (f *fileListView) SetFiles(files []fileEntry) {
	f.files = files
	f.groups = nil
	f.selected = 0
	f.offset = 0
	f.buildRows()
}

func (f *fileListView) SetGroups(groups []fileGroup) {
	f.groups = groups

	// Flatten into files for index-based operations
	f.files = nil
	for _, g := range groups {
		f.files = append(f.files, g.files...)
	}

	f.selected = 0
	f.offset = 0
	f.buildRows()
}

// SetGroupsKeepExisting overlays batch groups while keeping files not in the batch.
// Files already in the flat list that aren't covered by any group go into an "Other" group.
func (f *fileListView) SetGroupsKeepExisting(groups []fileGroup) {
	// Track which files are covered by batch groups
	covered := make(map[string]bool)
	for _, g := range groups {
		for _, file := range g.files {
			covered[file.path] = true
		}
	}

	// Preserve review status from existing files
	existingStatus := make(map[string]fileEntry)
	for _, file := range f.files {
		existingStatus[file.path] = file
	}

	// Apply existing review status to batch files
	for gi, g := range groups {
		for fi, file := range g.files {
			if existing, ok := existingStatus[file.path]; ok {
				groups[gi].files[fi].reviewed = existing.reviewed
				groups[gi].files[fi].status = existing.status
				groups[gi].files[fi].lines = existing.lines
			}
		}
	}

	// Collect uncovered existing files into "Other" group
	var other []fileEntry
	for _, file := range f.files {
		if !covered[file.path] {
			other = append(other, file)
		}
	}

	// Remember current selection to restore after rebuild
	var currentPath string
	if f.selected >= 0 && f.selected < len(f.files) {
		currentPath = f.files[f.selected].path
	}

	f.groups = groups
	if len(other) > 0 {
		f.groups = append(f.groups, fileGroup{name: "Other files", files: other})
	}

	// Flatten
	f.files = nil
	for _, g := range f.groups {
		f.files = append(f.files, g.files...)
	}

	// Restore selection if the file still exists in the new list
	f.selected = 0
	for i, file := range f.files {
		if file.path == currentPath {
			f.selected = i
			break
		}
	}
	f.offset = 0
	f.buildRows()
}

func (f *fileListView) buildRows() {
	f.rows = nil

	if len(f.groups) > 0 {
		fileIdx := 0
		for _, g := range f.groups {
			f.rows = append(f.rows, displayRow{isHeader: true, text: g.name})
			for range g.files {
				f.rows = append(f.rows, displayRow{fileIdx: fileIdx})
				fileIdx++
			}
		}
	} else {
		for i := range f.files {
			f.rows = append(f.rows, displayRow{fileIdx: i})
		}
	}
}

func (f *fileListView) SelectByPath(path string) {
	for i, file := range f.files {
		if file.path == path {
			f.selected = i
			// Find the display row for this file and ensure it's visible
			for ri, row := range f.rows {
				if !row.isHeader && row.fileIdx == i {
					if ri < f.offset {
						f.offset = ri
					} else if ri >= f.offset+f.height-3 {
						f.offset = ri - f.height + 4
					}
					break
				}
			}
			return
		}
	}
}

func (f *fileListView) MoveDown() {
	if f.selected < len(f.files)-1 {
		f.selected++
		// Ensure visible in scroll
		for ri, row := range f.rows {
			if !row.isHeader && row.fileIdx == f.selected {
				if ri >= f.offset+f.height-scrollPadding {
					f.offset = ri - f.height + scrollPadding + 1
				}
				break
			}
		}
	}
}

func (f *fileListView) MoveUp() {
	if f.selected > 0 {
		f.selected--
		for ri, row := range f.rows {
			if !row.isHeader && row.fileIdx == f.selected {
				if ri < f.offset {
					f.offset = ri
				}
				break
			}
		}
	}
}

func (f *fileListView) MarkReviewed(path string) {
	for i, file := range f.files {
		if file.path == path {
			f.files[i].reviewed = true
			return
		}
	}
}

func (f *fileListView) View() string {
	contentHeight := f.height - 2
	if contentHeight < 1 {
		contentHeight = 1
	}

	reviewed := 0
	for _, file := range f.files {
		if file.reviewed {
			reviewed++
		}
	}

	end := f.offset + contentHeight
	if end > len(f.rows) {
		end = len(f.rows)
	}

	var lines []string
	for i := f.offset; i < end; i++ {
		row := f.rows[i]

		if row.isHeader {
			// Group header
			name := row.text
			maxLen := f.width - 5
			if maxLen > 0 && len(name) > maxLen {
				name = name[:maxLen]
			}
			lines = append(lines, groupHeaderStyle.Render(" "+name))
			continue
		}

		file := f.files[row.fileIdx]

		// Status icon
		var icon string
		if file.reviewed {
			icon = fileListReviewed.Render("✓")
		} else {
			icon = fileListPending.Render("·")
		}

		// Status letter
		var statusMark string
		switch file.status {
		case "added":
			statusMark = fileListAdded.Render("A")
		case "modified":
			statusMark = fileListModified.Render("M")
		case "deleted":
			statusMark = fileListDeleted.Render("D")
		default:
			statusMark = " "
		}

		// File name
		name := file.path
		indent := "  "
		if len(f.groups) > 0 {
			indent = "    " // extra indent under group header
		}
		maxNameLen := f.width - len(indent) - 6
		if maxNameLen > 0 && len(name) > maxNameLen {
			name = "…" + name[len(name)-maxNameLen+1:]
		}

		line := fmt.Sprintf("%s%s %s %s", indent, icon, statusMark, name)

		if row.fileIdx == f.selected {
			line = fileListSelected.Width(f.width - 4).Render(line)
		}

		lines = append(lines, line)
	}

	for len(lines) < contentHeight {
		lines = append(lines, "")
	}

	content := strings.Join(lines, "\n")
	title := titleStyle.Render(fmt.Sprintf(" Files %d/%d ", reviewed, len(f.files)))

	return panelBorder.
		Width(f.width - 2).
		Height(f.height - 2).
		Render(title + "\n" + content)
}
