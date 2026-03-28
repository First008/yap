package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/First008/yap/internal/git"
	"github.com/First008/yap/internal/ipc"
	"github.com/First008/yap/internal/review"
	"github.com/First008/yap/internal/stt"
	"github.com/First008/yap/internal/tts"
)

type App struct {
	program      *tea.Program
	ipc          *ipc.Server
	tts          tts.TTSAdapter
	stt          stt.STTAdapter
	git          git.Provider
	tracker      *review.FileTracker
	repoDir      string
	pttCh        *PTTChannel
	diffCache    map[string]string // pre-cached diffs keyed by file path
	initialFiles []fileEntry       // pre-fetched for TUI init
	staged       bool              // review staged changes (--staged flag)
}

func NewApp(repoDir string, ttsAdapter tts.TTSAdapter, sttAdapter stt.STTAdapter) (*App, error) {
	ipcServer, err := ipc.NewServer(repoDir)
	if err != nil {
		return nil, fmt.Errorf("create IPC server: %w", err)
	}

	gitProv := git.NewProvider(repoDir)
	tracker := review.NewTracker(repoDir+"/.yap-state.json", gitProv)
	if err := tracker.Load(); err != nil {
		return nil, fmt.Errorf("load review state: %w", err)
	}

	pttCh := NewPTTChannel()

	app := &App{
		ipc:     ipcServer,
		tts:     ttsAdapter,
		stt:     sttAdapter,
		git:     gitProv,
		tracker: tracker,
		repoDir: repoDir,
		pttCh:   pttCh,
	}

	app.registerHandlers()
	return app, nil
}

// ChangedFilesSummary returns a pre-fetched summary of changed files for the Claude prompt.
// This eliminates the need for Claude to call get_changed_files on startup.
func (a *App) ChangedFilesSummary(staged bool) string {
	a.staged = staged
	a.tracker.Refresh()
	var files []git.ChangedFile
	var err error
	if staged {
		files, err = a.git.StagedFiles()
	} else {
		files, err = a.git.ChangedFiles()
	}
	if err != nil {
		return ""
	}

	// Pre-cache diffs
	a.diffCache = make(map[string]string)

	var sb strings.Builder
	var pending []string
	for _, f := range files {
		a.diffCache[f.Path] = f.Diff
		reviewed, _ := a.tracker.IsReviewed(f.Path)
		if !reviewed {
			lines := strings.Count(f.Diff, "\n")
			pending = append(pending, fmt.Sprintf("  - %s (%s, %d lines)", f.Path, f.Status, lines))
		}
	}

	if len(pending) == 0 {
		sb.WriteString("No pending files to review.")
	} else {
		sb.WriteString(fmt.Sprintf("%d files pending review:\n", len(pending)))
		sb.WriteString(strings.Join(pending, "\n"))
	}

	// Send file list to TUI immediately
	var entries []fileEntry
	for _, f := range files {
		reviewed, _ := a.tracker.IsReviewed(f.Path)
		entries = append(entries, fileEntry{
			path:     f.Path,
			status:   f.Status,
			reviewed: reviewed,
			lines:    strings.Count(f.Diff, "\n"),
		})
	}

	// We can't send to program yet (it hasn't started), so store for later
	a.initialFiles = entries

	return sb.String()
}

// StartIPC begins accepting IPC connections in the background.
// Call this before launching Claude so the MCP server can connect.
func (a *App) StartIPC(ctx context.Context) {
	go a.ipc.Serve(ctx)
}

func (a *App) Run(ctx context.Context) error {

	a.tracker.Refresh()

	model := NewModel(a.pttCh)

	// Wire the diff loader callback so the user can browse files manually
	model.loadDiff = func(filePath string, fileIndex int) tea.Cmd {
		diff, _ := a.getDiff(filePath)
		return func() tea.Msg {
			return ShowDiffMsg{
				FilePath:   filePath,
				Diff:       diff,
				FileIndex:  fileIndex,
				TotalFiles: len(a.initialFiles),
			}
		}
	}

	// Pre-populate file list if available
	if len(a.initialFiles) > 0 {
		model.fileList.SetFiles(a.initialFiles)
		model.status.totalFiles = len(a.initialFiles)
	}
	a.program = tea.NewProgram(model, tea.WithAltScreen())

	if _, err := a.program.Run(); err != nil {
		return fmt.Errorf("TUI: %w", err)
	}

	if err := a.tracker.Save(); err != nil {
		return fmt.Errorf("save review state: %w", err)
	}
	a.ipc.Close()
	return nil
}

func (a *App) registerHandlers() {
	a.ipc.Register("get_changed_files", a.handleGetChangedFiles)
	a.ipc.Register("show_diff", a.handleShowDiff)
	a.ipc.Register("speak", a.handleSpeak)
	a.ipc.Register("listen", a.handleListen)
	a.ipc.Register("mark_reviewed", a.handleMarkReviewed)
	a.ipc.Register("get_review_status", a.handleGetReviewStatus)
	a.ipc.Register("show_message", a.handleShowMessage)
	a.ipc.Register("review_file", a.handleReviewFile)
	a.ipc.Register("batch_review", a.handleBatchReview)
	a.ipc.Register("finish_review", a.handleFinishReview)
}

// waitForVoice implements push-to-talk: show prompt, wait for space, record, wait for space, transcribe.
func (a *App) waitForVoice(ctx context.Context) (string, error) {
	// Drain any stale signals from previous interactions
	drainCh(a.pttCh.StartRecord)
	drainCh(a.pttCh.StopRecord)
	drainCh(a.pttCh.QuickNext)

	// Tell TUI to show "PRESS SPACE TO TALK"
	if a.program != nil {
		a.program.Send(WaitForPTTMsg{})
	}

	// Wait for user to press space (start recording) or 'n' (quick next)
	select {
	case <-a.pttCh.StartRecord:
		// User pressed space — proceed to record
	case <-a.pttCh.QuickNext:
		// User pressed 'n' — skip STT, return "next"
		// Note: model.go already adds the "next (keyboard)" message to conversation
		return "next", nil
	case <-ctx.Done():
		return "", ctx.Err()
	}

	// Tell TUI we're recording
	if a.program != nil {
		a.program.Send(PTTRecordingMsg{})
	}

	// Record until user presses space again
	text, err := a.stt.ListenPTT(ctx, a.pttCh.StopRecord)
	if err != nil {
		if a.program != nil {
			a.program.Send(ListeningMsg{Active: false})
		}
		return "", err
	}

	if a.program != nil {
		a.program.Send(UserResponseMsg{Text: text})
	}

	return text, nil
}

func (a *App) handleGetChangedFiles(ctx context.Context, params json.RawMessage) (json.RawMessage, error) {
	a.tracker.Refresh()
	var files []git.ChangedFile
	var err error
	if a.staged {
		files, err = a.git.StagedFiles()
	} else {
		files, err = a.git.ChangedFiles()
	}
	if err != nil {
		return nil, err
	}

	// Pre-cache all diffs so review_file is instant
	a.diffCache = make(map[string]string)
	for _, f := range files {
		a.diffCache[f.Path] = f.Diff
	}

	// Return lightweight list — no diffs in the response
	type fileInfo struct {
		Path     string `json:"path"`
		Status   string `json:"status"`
		Lines    int    `json:"lines_changed"`
		Reviewed bool   `json:"reviewed"`
	}

	var result []fileInfo
	for _, f := range files {
		reviewed, _ := a.tracker.IsReviewed(f.Path)
		lineCount := strings.Count(f.Diff, "\n")
		result = append(result, fileInfo{
			Path:     f.Path,
			Status:   f.Status,
			Lines:    lineCount,
			Reviewed: reviewed,
		})
	}

	// Send file list to TUI
	if a.program != nil {
		var entries []fileEntry
		for _, f := range files {
			reviewed, _ := a.tracker.IsReviewed(f.Path)
			entries = append(entries, fileEntry{
				path:     f.Path,
				status:   f.Status,
				reviewed: reviewed,
				lines:    strings.Count(f.Diff, "\n"),
			})
		}
		a.program.Send(FileListMsg{Files: entries})
	}

	return json.Marshal(map[string]any{"files": result})
}

func (a *App) handleShowDiff(ctx context.Context, params json.RawMessage) (json.RawMessage, error) {
	var p struct {
		FilePath   string `json:"file_path"`
		FileIndex  int    `json:"file_index"`
		TotalFiles int    `json:"total_files"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, err
	}

	diff, err := a.getDiff(p.FilePath)
	if err != nil {
		return nil, err
	}

	if a.program != nil {
		a.program.Send(ShowDiffMsg{
			FilePath:   p.FilePath,
			Diff:       diff,
			FileIndex:  p.FileIndex,
			TotalFiles: p.TotalFiles,
		})
	}

	return json.Marshal(map[string]string{"status": "ok"})
}

func (a *App) handleSpeak(ctx context.Context, params json.RawMessage) (json.RawMessage, error) {
	var p struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, err
	}

	if a.program != nil {
		a.program.Send(SpeakingMsg{Text: p.Text})
	}

	if err := a.tts.Speak(ctx, p.Text); err != nil {
		return nil, err
	}

	if a.program != nil {
		a.program.Send(SpeakDoneMsg{})
	}

	return json.Marshal(map[string]string{"status": "ok"})
}

func (a *App) handleListen(ctx context.Context, params json.RawMessage) (json.RawMessage, error) {
	text, err := a.waitForVoice(ctx)
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]string{"text": text})
}

func (a *App) handleReviewFile(ctx context.Context, params json.RawMessage) (json.RawMessage, error) {
	var p struct {
		FilePath    string `json:"file_path"`
		FileIndex   int    `json:"file_index"`
		TotalFiles  int    `json:"total_files"`
		Explanation string `json:"explanation"`
		ScrollTo    int    `json:"scroll_to"` // optional: line number to scroll to
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, err
	}

	// 1. Show the diff
	diff, err := a.getDiff(p.FilePath)
	if err != nil {
		return nil, err
	}

	if a.program != nil {
		a.program.Send(ShowDiffMsg{
			FilePath:   p.FilePath,
			Diff:       diff,
			FileIndex:  p.FileIndex,
			TotalFiles: p.TotalFiles,
			ScrollTo:   p.ScrollTo,
		})
	}

	// 2. Speak the explanation (can be interrupted by 'n')
	skippedBySpeechNext := false
	if p.Explanation != "" {
		if a.program != nil {
			a.program.Send(SpeakingMsg{Text: p.Explanation})
		}

		drainCh(a.pttCh.QuickNext)

		speakDone := make(chan error, 1)
		go func() {
			speakDone <- a.tts.Speak(ctx, p.Explanation)
		}()

		select {
		case err := <-speakDone:
			if err != nil {
				if a.program != nil {
					a.program.Send(SpeakDoneMsg{})
				}
				return nil, err
			}
		case <-a.pttCh.QuickNext:
			a.tts.Stop()
			<-speakDone
			skippedBySpeechNext = true
		}

		if a.program != nil {
			a.program.Send(SpeakDoneMsg{})
		}
	}

	// If 'n' pressed during speech, return "next" immediately
	if skippedBySpeechNext {
		return json.Marshal(map[string]string{"text": "next", "file_path": p.FilePath})
	}

	// 3. Wait a moment, then push-to-talk
	time.Sleep(300 * time.Millisecond)

	text, err := a.waitForVoice(ctx)
	if err != nil {
		return nil, err
	}

	return json.Marshal(map[string]string{"text": text, "file_path": p.FilePath})
}

func (a *App) handleMarkReviewed(ctx context.Context, params json.RawMessage) (json.RawMessage, error) {
	var p struct {
		FilePath string `json:"file_path"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, err
	}

	if err := a.tracker.MarkReviewed(p.FilePath); err != nil {
		return nil, err
	}

	// Stage the file with git add
	cmd := exec.Command("git", "add", p.FilePath)
	cmd.Dir = a.repoDir
	if err := cmd.Run(); err != nil {
		// Non-fatal: log but don't fail the review
		if a.program != nil {
			a.program.Send(ShowMessageMsg{
				Text:   fmt.Sprintf("Warning: could not stage %s: %v", p.FilePath, err),
				Source: "system",
			})
		}
	}

	if err := a.tracker.Save(); err != nil {
		if a.program != nil {
			a.program.Send(ShowMessageMsg{
				Text:   fmt.Sprintf("Warning: could not save review state: %v", err),
				Source: "system",
			})
		}
	}

	if a.program != nil {
		status := a.tracker.Status()
		reviewed := 0
		for _, s := range status {
			if s.Reviewed {
				reviewed++
			}
		}
		a.program.Send(ReviewProgressMsg{
			Reviewed:    reviewed,
			Total:       len(status),
			CurrentFile: p.FilePath,
		})
	}

	return json.Marshal(map[string]string{"status": "ok", "staged": "true"})
}

func (a *App) handleGetReviewStatus(ctx context.Context, params json.RawMessage) (json.RawMessage, error) {
	a.tracker.Refresh()
	status := a.tracker.Status()

	reviewed := 0
	type fileStatus struct {
		Path     string `json:"path"`
		Reviewed bool   `json:"reviewed"`
	}
	var files []fileStatus
	for _, s := range status {
		if s.Reviewed {
			reviewed++
		}
		files = append(files, fileStatus{Path: s.Path, Reviewed: s.Reviewed})
	}

	return json.Marshal(map[string]any{
		"reviewed": reviewed,
		"pending":  len(status) - reviewed,
		"total":    len(status),
		"files":    files,
	})
}

func (a *App) handleShowMessage(ctx context.Context, params json.RawMessage) (json.RawMessage, error) {
	var p struct {
		Text   string `json:"text"`
		Source string `json:"source"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, err
	}

	if p.Source == "" {
		p.Source = "system"
	}

	if a.program != nil {
		a.program.Send(ShowMessageMsg{Text: p.Text, Source: p.Source})
	}

	return json.Marshal(map[string]string{"status": "ok"})
}

func (a *App) handleFinishReview(ctx context.Context, params json.RawMessage) (json.RawMessage, error) {
	var p struct {
		Summary string `json:"summary"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, err
	}

	// Speak the summary
	if p.Summary != "" {
		if a.program != nil {
			a.program.Send(SpeakingMsg{Text: p.Summary})
		}
		a.tts.Speak(ctx, p.Summary)
		if a.program != nil {
			a.program.Send(SpeakDoneMsg{})
		}
	}

	// Save state and exit TUI
	a.tracker.Save()

	if a.program != nil {
		a.program.Send(ReviewFinishedMsg{Summary: p.Summary})
	}

	return json.Marshal(map[string]string{"status": "finished"})
}

// drainCh removes any pending signals from a buffered channel.
func drainCh(ch chan struct{}) {
	select {
	case <-ch:
	default:
	}
}

type batchQueueEntry struct {
	groupName   string
	path        string
	explanation string
	scrollTo    int
}

func (a *App) handleBatchReview(ctx context.Context, params json.RawMessage) (json.RawMessage, error) {
	var p struct {
		Groups []struct {
			Name  string `json:"name"`
			Files []struct {
				Path        string `json:"path"`
				Explanation string `json:"explanation"`
				ScrollTo    int    `json:"scroll_to"`
			} `json:"files"`
		} `json:"groups"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, err
	}

	// Build groups for the TUI sidebar
	var groups []fileGroup
	var queue []batchQueueEntry

	for _, g := range p.Groups {
		var entries []fileEntry
		for _, f := range g.Files {
			entries = append(entries, fileEntry{
				path:   f.Path,
				status: "modified",
			})
			queue = append(queue, batchQueueEntry{
				groupName:   g.Name,
				path:        f.Path,
				explanation: f.Explanation,
				scrollTo:    f.ScrollTo,
			})
		}
		groups = append(groups, fileGroup{name: g.Name, files: entries})
	}

	// Populate the sidebar with grouped files
	if a.program != nil {
		a.program.Send(BatchStartMsg{Groups: groups})
	}

	total := len(queue)
	var completed []map[string]string
	var interrupted map[string]string
	var remaining []map[string]string
	reviewed := 0
	skipped := 0

	// Process each file locally
fileLoop:
	for i, entry := range queue {
		// Show the diff
		diff, _ := a.getDiff(entry.path)
		if a.program != nil {
			a.program.Send(BatchGroupMsg{GroupName: entry.groupName})
			a.program.Send(ShowDiffMsg{
				FilePath:   entry.path,
				Diff:       diff,
				FileIndex:  i,
				TotalFiles: total,
				ScrollTo:   entry.scrollTo,
			})
		}

		// Speak the explanation (can be interrupted by 'n')
		skippedBySpeechNext := false
		if entry.explanation != "" {
			if a.program != nil {
				a.program.Send(SpeakingMsg{Text: entry.explanation})
			}

			// Drain any stale QuickNext before speaking
			drainCh(a.pttCh.QuickNext)

			// Race: speak vs 'n' key
			speakDone := make(chan error, 1)
			go func() {
				speakDone <- a.tts.Speak(ctx, entry.explanation)
			}()

			select {
			case err := <-speakDone:
				if err != nil {
					if a.program != nil {
						a.program.Send(SpeakDoneMsg{})
					}
					break fileLoop
				}
			case <-a.pttCh.QuickNext:
				// User pressed 'n' during speech — stop TTS and skip to next
				a.tts.Stop()
				<-speakDone // wait for speak goroutine to finish
				skippedBySpeechNext = true
			}

			if a.program != nil {
				a.program.Send(SpeakDoneMsg{})
			}
		}

		// If user pressed 'n' during speech, treat as approve and move on
		if skippedBySpeechNext {
			a.tracker.MarkReviewed(entry.path)
			cmd := exec.Command("git", "add", entry.path)
			cmd.Dir = a.repoDir
			cmd.Run()
			a.tracker.Save()
			reviewed++

			if a.program != nil {
				a.program.Send(ReviewProgressMsg{
					Reviewed:    reviewed,
					Total:       total,
					CurrentFile: entry.path,
				})
			}

			completed = append(completed, map[string]string{
				"path": entry.path, "group": entry.groupName, "response": "next",
			})
			continue
		}

		// Brief pause then push-to-talk (retry up to 2 times on empty)
		time.Sleep(300 * time.Millisecond)
		var text string
		for attempt := 0; attempt < 3; attempt++ {
			var err error
			text, err = a.waitForVoice(ctx)
			if err != nil {
				break fileLoop
			}
			if classifyResponse(text) != responseEmpty {
				break // got actual input
			}
			// Empty — notify user and retry
			if a.program != nil {
				a.program.Send(ShowMessageMsg{
					Text:   "Didn't catch that. Press SPACE to try again, or N to continue.",
					Source: "system",
				})
			}
		}

		// Classify and act
		switch classifyResponse(text) {
		case responseEmpty:
			// Still empty after retries — treat as skip to avoid getting stuck
			skipped++
			completed = append(completed, map[string]string{
				"path": entry.path, "group": entry.groupName, "response": "(no input)",
			})
			continue
		case responseSimple:
			a.tracker.MarkReviewed(entry.path)
			cmd := exec.Command("git", "add", entry.path)
			cmd.Dir = a.repoDir
			cmd.Run()
			a.tracker.Save()
			reviewed++

			if a.program != nil {
				a.program.Send(ReviewProgressMsg{
					Reviewed:    reviewed,
					Total:       total,
					CurrentFile: entry.path,
				})
			}

			completed = append(completed, map[string]string{
				"path": entry.path, "group": entry.groupName, "response": text,
			})

		case responseSkip:
			skipped++
			completed = append(completed, map[string]string{
				"path": entry.path, "group": entry.groupName, "response": "skip",
			})

		case responseStop:
			for _, r := range queue[i+1:] {
				remaining = append(remaining, map[string]string{
					"path": r.path, "group": r.groupName,
				})
			}
			break fileLoop

		case responseComplex:
			interrupted = map[string]string{
				"path": entry.path, "group": entry.groupName, "response": text,
			}
			for _, r := range queue[i+1:] {
				remaining = append(remaining, map[string]string{
					"path": r.path, "group": r.groupName,
				})
			}
			break fileLoop
		}
	}

	interruptedCount := 0
	if interrupted != nil {
		interruptedCount = 1
	}

	return json.Marshal(map[string]any{
		"completed":   completed,
		"interrupted": interrupted,
		"remaining":   remaining,
		"summary": map[string]int{
			"reviewed":    reviewed,
			"skipped":     skipped,
			"interrupted": interruptedCount,
			"total":       total,
		},
	})
}
