package git

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type ChangedFile struct {
	Path        string
	Status      string // "added", "modified", "deleted", "renamed"
	Diff        string
	ContentHash string
}

type Provider interface {
	ChangedFiles() ([]ChangedFile, error)
	StagedFiles() ([]ChangedFile, error)
	FileDiff(path string) (string, error)
	ContentHash(path string) (string, error)
}

type GitProvider struct {
	repoDir string
}

func NewProvider(repoDir string) *GitProvider {
	return &GitProvider{repoDir: repoDir}
}

// ChangedFiles returns unstaged changes + untracked files (default review mode).
// Does NOT include already-staged files unless they also have unstaged modifications.
func (g *GitProvider) ChangedFiles() ([]ChangedFile, error) {
	var files []ChangedFile
	seen := make(map[string]bool)

	// 1. Unstaged changes (working tree vs index/staged)
	unstaged, err := g.run("diff", "--name-status")
	if err != nil {
		return nil, fmt.Errorf("unstaged: %w", err)
	}
	for _, f := range parseNameStatus(unstaged) {
		f.Status = "modified"
		seen[f.Path] = true
		files = append(files, f)
	}

	// 2. Untracked files (brand new, never staged)
	untracked, err := g.run("ls-files", "--others", "--exclude-standard")
	if err != nil {
		return nil, fmt.Errorf("untracked: %w", err)
	}
	for _, line := range strings.Split(strings.TrimSpace(untracked), "\n") {
		if line == "" || seen[line] {
			continue
		}
		seen[line] = true
		files = append(files, ChangedFile{Path: line, Status: "added"})
	}

	return g.fillDiffsAndHashes(files)
}

// StagedFiles returns staged changes (--staged mode).
// Shows what's in the index vs HEAD.
func (g *GitProvider) StagedFiles() ([]ChangedFile, error) {
	var files []ChangedFile

	staged, err := g.run("diff", "--cached", "--name-status")
	if err != nil {
		// Fresh repo — use empty tree
		staged, _ = g.run("diff", "--cached", "--name-status", "--diff-filter=ACDMR", "4b825dc642cb6eb9a060e54bf899d15f3f9382e7")
	}
	for _, f := range parseNameStatus(staged) {
		files = append(files, f)
	}

	return g.fillDiffsAndHashes(files)
}

func (g *GitProvider) fillDiffsAndHashes(files []ChangedFile) ([]ChangedFile, error) {
	var validFiles []ChangedFile
	for _, f := range files {
		diff, err := g.FileDiff(f.Path)
		if err != nil {
			continue
		}
		f.Diff = diff

		hash, err := g.ContentHash(f.Path)
		if err != nil {
			if f.Status != "deleted" {
				continue
			}
		} else {
			f.ContentHash = hash
		}

		validFiles = append(validFiles, f)
	}
	return validFiles, nil
}

func (g *GitProvider) FileDiff(path string) (string, error) {
	// Priority 1: Unstaged changes
	out, err := g.run("diff", "--", path)
	if err == nil && strings.TrimSpace(out) != "" {
		return out, nil
	}

	// Priority 2: Staged changes
	out, err = g.run("diff", "--cached", "--", path)
	if err == nil && strings.TrimSpace(out) != "" {
		return out, nil
	}

	// Priority 3: Untracked file
	return g.untrackedDiff(path)
}

func (g *GitProvider) ContentHash(path string) (string, error) {
	out, err := g.run("hash-object", path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func (g *GitProvider) untrackedDiff(path string) (string, error) {
	data, err := os.ReadFile(filepath.Join(g.repoDir, path))
	if err != nil {
		return "", fmt.Errorf("cannot read %s: %w", path, err)
	}

	lines := strings.Split(string(data), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	var diff strings.Builder
	diff.WriteString(fmt.Sprintf("--- /dev/null\n+++ b/%s\n", path))
	diff.WriteString(fmt.Sprintf("@@ -0,0 +1,%d @@\n", len(lines)))
	for _, line := range lines {
		diff.WriteString("+" + line + "\n")
	}
	return diff.String(), nil
}

func (g *GitProvider) run(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = g.repoDir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %s: %w", strings.Join(args, " "), stderr.String(), err)
	}
	return stdout.String(), nil
}

func parseNameStatus(output string) []ChangedFile {
	var files []ChangedFile
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		status := parseStatus(parts[0])
		path := parts[len(parts)-1]
		files = append(files, ChangedFile{Path: path, Status: status})
	}
	return files
}

func parseStatus(code string) string {
	switch {
	case strings.HasPrefix(code, "A"):
		return "added"
	case strings.HasPrefix(code, "M"):
		return "modified"
	case strings.HasPrefix(code, "D"):
		return "deleted"
	case strings.HasPrefix(code, "R"):
		return "renamed"
	default:
		return "modified"
	}
}
