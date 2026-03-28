package tui

import (
	"os"
	"os/exec"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// PTT (push-to-talk) state
type pttState int

const (
	pttIdle      pttState = iota
	pttWaiting
	pttRecording
)

type PTTChannel struct {
	StartRecord chan struct{} // user pressed space to start recording
	StopRecord  chan struct{} // user pressed space to stop recording
	QuickNext   chan struct{} // user pressed 'n' to skip STT and approve
}

func NewPTTChannel() *PTTChannel {
	return &PTTChannel{
		StartRecord: make(chan struct{}, 1),
		StopRecord:  make(chan struct{}, 1),
		QuickNext:   make(chan struct{}, 1),
	}
}

type focusPanel int

const (
	focusDiff     focusPanel = iota
	focusFileList
)

// DiffLoader is a callback to fetch a file's diff from the App's cache.
type DiffLoader func(filePath string, fileIndex int) tea.Cmd

type Model struct {
	fileList     fileListView
	diff         diffView
	conversation conversationView
	status       statusBar

	width    int
	height   int
	focus    focusPanel
	loadDiff DiffLoader

	pendingG bool
	ptt      pttState
	pttCh    *PTTChannel
}

func NewModel(pttCh *PTTChannel) Model {
	return Model{
		fileList:     newFileListView(),
		diff:         newDiffView(),
		conversation: newConversationView(),
		status:       newStatusBar(),
		pttCh:        pttCh,
	}
}

func (m Model) Init() tea.Cmd {
	return nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.updateLayout()
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case ReviewStartedMsg:
		m.status.analyzing = false
		return m, nil

	case BatchStartMsg:
		m.status.analyzing = false
		// Merge batch groups with existing file list — preserve files not in the batch
		m.fileList.SetGroupsKeepExisting(msg.Groups)
		m.status.totalFiles = len(m.fileList.files)
		return m, nil

	case BatchGroupMsg:
		m.status.batchGroup = msg.GroupName
		return m, nil

	case ReviewFinishedMsg:
		m.status.analyzing = false
		if msg.Summary != "" {
			m.conversation.AddMessage(msg.Summary, "system")
		}
		return m, tea.Quit

	case RequestDiffMsg:
		// User manually selected a file — load diff via callback
		if m.loadDiff != nil {
			return m, m.loadDiff(msg.FilePath, msg.FileIndex)
		}
		return m, nil

	case ShowDiffMsg:
		m.status.analyzing = false
		m.diff.SetContent(msg.FilePath, msg.Diff, msg.FileIndex, msg.TotalFiles)
		if msg.ScrollTo > 0 {
			m.diff.ScrollToLine(msg.ScrollTo)
		}
		m.fileList.SelectByPath(msg.FilePath)
		m.status.currentFile = msg.FilePath
		m.status.fileIndex = msg.FileIndex
		m.status.totalFiles = msg.TotalFiles
		return m, nil

	case FileListMsg:
		m.fileList.SetFiles(msg.Files)
		m.status.totalFiles = len(msg.Files)
		return m, nil

	case ShowMessageMsg:
		m.conversation.AddMessage(msg.Text, msg.Source)
		return m, nil

	case SpeakingMsg:
		m.status.speaking = true
		m.conversation.AddMessage(msg.Text, "claude")
		return m, nil

	case SpeakDoneMsg:
		m.status.speaking = false
		return m, nil

	case WaitForPTTMsg:
		m.ptt = pttWaiting
		m.status.listening = false
		m.status.waitingPTT = true
		return m, nil

	case PTTRecordingMsg:
		m.ptt = pttRecording
		m.status.waitingPTT = false
		m.status.listening = true
		return m, nil

	case ListeningMsg:
		m.status.listening = msg.Active
		if !msg.Active {
			m.ptt = pttIdle
			m.status.waitingPTT = false
		}
		return m, nil

	case UserResponseMsg:
		m.status.listening = false
		m.status.waitingPTT = false
		m.ptt = pttIdle
		m.conversation.AddMessage(msg.Text, "user")
		return m, nil

	case EditorFinishedMsg:
		return m, nil

	case ReviewProgressMsg:
		m.status.reviewed = msg.Reviewed
		m.status.totalFiles = msg.Total
		if msg.CurrentFile != "" {
			m.status.currentFile = msg.CurrentFile
			m.fileList.MarkReviewed(msg.CurrentFile)
		}
		return m, nil
	}

	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Quick next: press 'n' during PTT wait or while speaking to skip and approve
	if key.Matches(msg, keys.Next) && (m.ptt == pttWaiting || m.status.speaking) {
		m.ptt = pttIdle
		m.status.waitingPTT = false
		m.status.speaking = false
		m.conversation.AddMessage("next (keyboard)", "user")
		select {
		case m.pttCh.QuickNext <- struct{}{}:
		default:
		}
		return m, nil
	}

	if key.Matches(msg, keys.Talk) {
		switch m.ptt {
		case pttWaiting:
			m.ptt = pttRecording
			m.status.waitingPTT = false
			m.status.listening = true
			select {
			case m.pttCh.StartRecord <- struct{}{}:
			default:
			}
			return m, nil
		case pttRecording:
			m.status.listening = false
			select {
			case m.pttCh.StopRecord <- struct{}{}:
			default:
			}
			return m, nil
		}
		return m, nil
	}

	switch {
	case key.Matches(msg, keys.Tab):
		if m.focus == focusDiff {
			m.focus = focusFileList
		} else {
			m.focus = focusDiff
		}
		return m, nil

	case key.Matches(msg, keys.Enter):
		// In file list mode, request the diff for the selected file
		if m.focus == focusFileList && m.fileList.selected < len(m.fileList.files) {
			file := m.fileList.files[m.fileList.selected]
			idx := m.fileList.selected
			return m, func() tea.Msg {
				return RequestDiffMsg{
					FilePath:  file.path,
					FileIndex: idx,
				}
			}
		}
		return m, nil

	case key.Matches(msg, keys.Edit):
		if m.diff.filePath != "" {
			return m, openEditor(m.diff.filePath)
		}
		return m, nil

	case key.Matches(msg, keys.Quit):
		return m, tea.Quit

	case key.Matches(msg, keys.Down):
		m.pendingG = false
		if m.focus == focusFileList {
			m.fileList.MoveDown()
		} else {
			m.diff.ScrollDown(1)
		}

	case key.Matches(msg, keys.Up):
		m.pendingG = false
		if m.focus == focusFileList {
			m.fileList.MoveUp()
		} else {
			m.diff.ScrollUp(1)
		}

	case key.Matches(msg, keys.HalfDown):
		m.pendingG = false
		m.diff.ScrollDown(m.diff.height / 2)

	case key.Matches(msg, keys.HalfUp):
		m.pendingG = false
		m.diff.ScrollUp(m.diff.height / 2)

	case key.Matches(msg, keys.Bottom):
		m.pendingG = false
		m.diff.GoToBottom()

	case key.Matches(msg, keys.Top):
		if m.pendingG {
			m.diff.GoToTop()
			m.pendingG = false
		} else {
			m.pendingG = true
		}

	default:
		m.pendingG = false
	}

	return m, nil
}

func (m *Model) updateLayout() {
	fileListWidth := int(float64(m.width) * 0.18)
	if fileListWidth < 20 {
		fileListWidth = 20
	}
	convWidth := int(float64(m.width) * 0.28)
	diffWidth := m.width - fileListWidth - convWidth
	contentHeight := m.height - 1

	m.fileList.width = fileListWidth
	m.fileList.height = contentHeight
	m.diff.width = diffWidth
	m.diff.height = contentHeight
	m.conversation.width = convWidth
	m.conversation.height = contentHeight
	m.status.width = m.width
}

func (m Model) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	mainContent := lipgloss.JoinHorizontal(
		lipgloss.Top,
		m.fileList.View(),
		m.diff.View(),
		m.conversation.View(),
	)

	return lipgloss.JoinVertical(
		lipgloss.Left,
		mainContent,
		m.status.View(),
	)
}

func openEditor(filePath string) tea.Cmd {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vim"
	}
	c := exec.Command(editor, filePath)
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return EditorFinishedMsg{Err: err}
	})
}
