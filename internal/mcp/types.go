package mcp

type GetChangedFilesResult struct {
	Files []ChangedFileInfo `json:"files"`
}

type ChangedFileInfo struct {
	Path     string `json:"path"`
	Status   string `json:"status"`
	Diff     string `json:"diff"`
	Reviewed bool   `json:"reviewed"`
}

type ShowDiffParams struct {
	FilePath   string `json:"file_path"`
	FileIndex  int    `json:"file_index"`
	TotalFiles int    `json:"total_files"`
}

type SpeakParams struct {
	Text string `json:"text"`
}

type ListenResult struct {
	Text string `json:"text"`
}

type MarkReviewedParams struct {
	FilePath string `json:"file_path"`
}

type ReviewStatusResult struct {
	Reviewed int          `json:"reviewed"`
	Pending  int          `json:"pending"`
	Total    int          `json:"total"`
	Files    []FileStatus `json:"files"`
}

type FileStatus struct {
	Path     string `json:"path"`
	Reviewed bool   `json:"reviewed"`
}

type ShowMessageParams struct {
	Text   string `json:"text"`
	Source string `json:"source"`
}

type FinishReviewParams struct {
	Summary string `json:"summary"`
}

type ReviewFileParams struct {
	FilePath    string `json:"file_path"`
	FileIndex   int    `json:"file_index"`
	TotalFiles  int    `json:"total_files"`
	Explanation string `json:"explanation"`
}

// Batch review types

type BatchReviewFileParam struct {
	Path        string `json:"path"`
	Explanation string `json:"explanation"`
	ScrollTo    int    `json:"scroll_to,omitempty"`
}

type BatchReviewGroup struct {
	Name  string                 `json:"name"`
	Files []BatchReviewFileParam `json:"files"`
}

type BatchReviewParams struct {
	Groups []BatchReviewGroup `json:"groups"`
}

type BatchFileResult struct {
	Path     string `json:"path"`
	Group    string `json:"group"`
	Response string `json:"response"`
}

type BatchReviewResult struct {
	Completed   []BatchFileResult `json:"completed"`
	Interrupted *BatchFileResult  `json:"interrupted,omitempty"`
	Remaining   []BatchFileResult `json:"remaining"`
	Summary     struct {
		Reviewed    int `json:"reviewed"`
		Skipped     int `json:"skipped"`
		Interrupted int `json:"interrupted"`
		Total       int `json:"total"`
	} `json:"summary"`
}
