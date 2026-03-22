package review

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/First008/yap/internal/git"
)

type mockGitProvider struct {
	hashes map[string]string
}

func (m *mockGitProvider) ChangedFiles() ([]git.ChangedFile, error) {
	var files []git.ChangedFile
	for path, hash := range m.hashes {
		files = append(files, git.ChangedFile{
			Path:        path,
			Status:      "modified",
			ContentHash: hash,
		})
	}
	return files, nil
}

func (m *mockGitProvider) StagedFiles() ([]git.ChangedFile, error) {
	return m.ChangedFiles()
}

func (m *mockGitProvider) FileDiff(path string) (string, error) {
	return "", nil
}

func (m *mockGitProvider) StagedFileDiff(path string) (string, error) {
	return "", nil
}

func (m *mockGitProvider) ContentHash(path string) (string, error) {
	return m.hashes[path], nil
}

func TestMarkReviewed(t *testing.T) {
	mock := &mockGitProvider{hashes: map[string]string{
		"main.go": "abc123",
	}}
	tracker := NewTracker("", mock)

	if err := tracker.MarkReviewed("main.go"); err != nil {
		t.Fatalf("MarkReviewed: %v", err)
	}

	reviewed, err := tracker.IsReviewed("main.go")
	if err != nil {
		t.Fatalf("IsReviewed: %v", err)
	}
	if !reviewed {
		t.Fatal("expected main.go to be reviewed")
	}
}

func TestHashInvalidation(t *testing.T) {
	mock := &mockGitProvider{hashes: map[string]string{
		"main.go": "abc123",
	}}
	tracker := NewTracker("", mock)

	tracker.MarkReviewed("main.go")

	// Simulate file change
	mock.hashes["main.go"] = "def456"

	reviewed, err := tracker.IsReviewed("main.go")
	if err != nil {
		t.Fatalf("IsReviewed: %v", err)
	}
	if reviewed {
		t.Fatal("expected main.go to be invalidated after hash change")
	}
}

func TestSaveLoad(t *testing.T) {
	dir := t.TempDir()
	stateFile := filepath.Join(dir, "state.json")

	mock := &mockGitProvider{hashes: map[string]string{
		"main.go": "abc123",
	}}

	tracker := NewTracker(stateFile, mock)
	tracker.MarkReviewed("main.go")

	if err := tracker.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if _, err := os.Stat(stateFile); err != nil {
		t.Fatalf("state file not created: %v", err)
	}

	tracker2 := NewTracker(stateFile, mock)
	if err := tracker2.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	reviewed, err := tracker2.IsReviewed("main.go")
	if err != nil {
		t.Fatalf("IsReviewed: %v", err)
	}
	if !reviewed {
		t.Fatal("expected main.go to be reviewed after load")
	}
}

func TestLoadNonExistent(t *testing.T) {
	tracker := NewTracker("/nonexistent/path.json", nil)
	if err := tracker.Load(); err != nil {
		t.Fatalf("Load on nonexistent file should succeed: %v", err)
	}
}

func TestStatus(t *testing.T) {
	mock := &mockGitProvider{hashes: map[string]string{
		"a.go": "hash1",
		"b.go": "hash2",
	}}
	tracker := NewTracker("", mock)
	tracker.MarkReviewed("a.go")

	tracker.files["b.go"] = &FileStatus{Path: "b.go", ContentHash: "hash2", Reviewed: false}

	status := tracker.Status()
	if len(status) != 2 {
		t.Fatalf("expected 2 files, got %d", len(status))
	}
	if status[0].Path != "a.go" || status[1].Path != "b.go" {
		t.Fatal("expected files sorted by path")
	}
	if !status[0].Reviewed || status[1].Reviewed {
		t.Fatal("unexpected review status")
	}
}

func TestRefresh(t *testing.T) {
	mock := &mockGitProvider{hashes: map[string]string{
		"a.go": "hash1",
		"b.go": "hash2",
	}}
	tracker := NewTracker("", mock)
	tracker.MarkReviewed("a.go")

	if err := tracker.Refresh(); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	status := tracker.Status()
	if len(status) != 2 {
		t.Fatalf("expected 2 files after refresh, got %d", len(status))
	}

	// a.go should still be reviewed (hash unchanged)
	reviewed, _ := tracker.IsReviewed("a.go")
	if !reviewed {
		t.Fatal("a.go should still be reviewed")
	}

	// Simulate a.go changed
	mock.hashes["a.go"] = "newhash"
	if err := tracker.Refresh(); err != nil {
		t.Fatalf("Refresh after change: %v", err)
	}

	reviewed, _ = tracker.IsReviewed("a.go")
	if reviewed {
		t.Fatal("a.go should be invalidated after hash change")
	}
}
