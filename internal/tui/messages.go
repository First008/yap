package tui

type ShowDiffMsg struct {
	FilePath   string
	Diff       string
	FileIndex  int
	TotalFiles int
	ScrollTo   int // line number to scroll to (0 = top)
}

type FileListMsg struct {
	Files []fileEntry
}

type ShowMessageMsg struct {
	Text   string
	Source string // "claude", "user", "system"
}

type SpeakingMsg struct {
	Text string
}

type SpeakDoneMsg struct{}

// WaitForPTTMsg tells the TUI to show "Press SPACE to talk" and wait.
type WaitForPTTMsg struct{}

// PTTRecordingMsg means the user pressed space — recording has started.
type PTTRecordingMsg struct{}

// PTTStopMsg means the user pressed space again — recording stopped.
type PTTStopMsg struct{}

type ListeningMsg struct {
	Active bool
}

type UserResponseMsg struct {
	Text string
}

type ReviewProgressMsg struct {
	Reviewed    int
	Total       int
	CurrentFile string
}

type WindowSizeMsg struct {
	Width  int
	Height int
}

// EditorFinishedMsg is sent when the user returns from the external editor.
type EditorFinishedMsg struct {
	Err error
}

// RequestDiffMsg asks the App to load a diff for a file (from cache or git).
// Sent when the user manually selects a file in the sidebar.
type RequestDiffMsg struct {
	FilePath  string
	FileIndex int
}

// ReviewStartedMsg clears the "analyzing" state when Claude begins reviewing.
type ReviewStartedMsg struct{}

// BatchStartMsg tells the TUI a batch review is starting with grouped files.
type BatchStartMsg struct {
	Groups []fileGroup
}

// BatchGroupMsg tells the TUI which group is currently active.
type BatchGroupMsg struct {
	GroupName string
}

// ReviewFinishedMsg tells the TUI that the review session is complete.
type ReviewFinishedMsg struct {
	Summary string
}
