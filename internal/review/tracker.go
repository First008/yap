package review

import (
	"github.com/First008/yap/internal/git"
)

type FileStatus struct {
	Path        string `json:"path"`
	ContentHash string `json:"content_hash"`
	Reviewed    bool   `json:"reviewed"`
}

type Tracker interface {
	Status() []FileStatus
	MarkReviewed(path string) error
	IsReviewed(path string) (bool, error)
	Refresh() error
	Save() error
	Load() error
}

type FileTracker struct {
	stateFile string
	gitProv   git.Provider
	files     map[string]*FileStatus
}

func NewTracker(stateFile string, gitProv git.Provider) *FileTracker {
	return &FileTracker{
		stateFile: stateFile,
		gitProv:   gitProv,
		files:     make(map[string]*FileStatus),
	}
}
