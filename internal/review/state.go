package review

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
)

func (t *FileTracker) Status() []FileStatus {
	var result []FileStatus
	for _, fs := range t.files {
		result = append(result, *fs)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Path < result[j].Path
	})
	return result
}

func (t *FileTracker) MarkReviewed(path string) error {
	hash, err := t.gitProv.ContentHash(path)
	if err != nil {
		return fmt.Errorf("content hash for %s: %w", path, err)
	}

	t.files[path] = &FileStatus{
		Path:        path,
		ContentHash: hash,
		Reviewed:    true,
	}
	return nil
}

func (t *FileTracker) IsReviewed(path string) (bool, error) {
	fs, ok := t.files[path]
	if !ok {
		return false, nil
	}
	if !fs.Reviewed {
		return false, nil
	}

	// Check if file has changed since review
	currentHash, err := t.gitProv.ContentHash(path)
	if err != nil {
		return false, err
	}

	if currentHash != fs.ContentHash {
		// File changed — invalidate review
		fs.Reviewed = false
		return false, nil
	}

	return true, nil
}

func (t *FileTracker) Refresh() error {
	changedFiles, err := t.gitProv.ChangedFiles()
	if err != nil {
		return fmt.Errorf("get changed files: %w", err)
	}

	// Add new files, keep existing review state
	currentPaths := make(map[string]bool)
	for _, cf := range changedFiles {
		currentPaths[cf.Path] = true
		if _, exists := t.files[cf.Path]; !exists {
			t.files[cf.Path] = &FileStatus{
				Path:        cf.Path,
				ContentHash: cf.ContentHash,
				Reviewed:    false,
			}
		}
	}

	// Remove files that are no longer changed
	for path := range t.files {
		if !currentPaths[path] {
			delete(t.files, path)
		}
	}

	// Invalidate reviews where hash changed
	for path, fs := range t.files {
		if fs.Reviewed {
			currentHash, err := t.gitProv.ContentHash(path)
			if err != nil {
				continue
			}
			if currentHash != fs.ContentHash {
				fs.Reviewed = false
			}
		}
	}

	return nil
}

func (t *FileTracker) Save() error {
	data, err := json.MarshalIndent(t.files, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	return os.WriteFile(t.stateFile, data, 0644)
}

func (t *FileTracker) Load() error {
	data, err := os.ReadFile(t.stateFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read state: %w", err)
	}

	var files map[string]*FileStatus
	if err := json.Unmarshal(data, &files); err != nil {
		return fmt.Errorf("unmarshal state: %w", err)
	}
	t.files = files
	return nil
}
