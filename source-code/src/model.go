package src

import (
	"fmt"
	"os"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type item struct {
	title, desc string
	status      string // For Git status
	selected    bool
	isDir       bool
	size        int64
	modTime     time.Time
}

func (i item) Title() string {
	title := i.title
	if i.selected {
		title = "* " + title
	}
	if i.status != "" {
		switch i.status[0] {
			case 'M':
				title = gitModifiedStyle.Render(title)
			case 'A':
				title = gitAddedStyle.Render(title)
			case 'D':
				title = gitDeletedStyle.Render(title)
		}
	}
	return title
}

func (i item) Description() string {
	return fmt.Sprintf("%s | Size: %d | Mod: %s", i.desc, i.size, i.modTime.Format("2006-01-02 15:04"))
}

func (i item) FilterValue() string { return i.title }

type CommandResult struct {
	Output string
	Err    error
}

type ProgressMsg struct {
	Percent float64
}

type tickMsg time.Time

type panel struct {
	currentDir    string
	gitBranch     string
	fileList      list.Model
	selectedFiles map[string]bool
	preview       viewport.Model
	vfs           vfsHandler
}

type Model struct {
	panels         [2]panel
	activePanel    int
	commandInput   textinput.Model
	statusMsg      string
	keys           keyMap
	mode           mode
	editor         textarea.Model
	editorFile     string
	progress       progress.Model
	quitting       bool
	ProgressChan   chan ProgressMsg
	ResultChan     chan CommandResult
	fuzzyInput     textinput.Model
	fuzzyResults   []string
	bulkRenameFrom string
	bulkRenameTo   string
	subShell       bool
}

func InitialModel() Model {
	wd, _ := os.Getwd()
	ti := textinput.New()
	ti.Placeholder = "Enter command (e.g., cd .., mv file1 file2, sftp://user:pass@host)"
	ti.Focus()
	ti.Width = 80
	del := list.NewDefaultDelegate()
	del.Styles.SelectedTitle = del.Styles.SelectedTitle.Foreground(lipgloss.Color("#FFFFFF")).Bold(true)
	del.Styles.NormalTitle = del.Styles.NormalTitle.Foreground(lipgloss.Color("#CCCCCC"))
	l1 := list.New([]list.Item{}, del, 0, 0)
	l1.Title = "Left Panel"
	l1.Styles.Title = subtitleStyle
	l1.SetShowFilter(true)
	l2 := list.New([]list.Item{}, del, 0, 0)
	l2.Title = "Right Panel"
	l2.Styles.Title = subtitleStyle
	l2.SetShowFilter(true)
	ta := textarea.New()
	ta.Placeholder = "Edit your file here..."
	ta.Focus()
	pv1 := viewport.New(0, 0)
	pv1.SetContent("Preview")
	pv2 := viewport.New(0, 0)
	pv2.SetContent("Preview")
	prog := progress.New(progress.WithDefaultGradient())
	fi := textinput.New()
	fi.Placeholder = "Fuzzy search..."
	m := Model{
		panels: [2]panel{
			{currentDir: wd, fileList: l1, selectedFiles: make(map[string]bool), preview: pv1, vfs: localVFS{}},
			{currentDir: wd, fileList: l2, selectedFiles: make(map[string]bool), preview: pv2, vfs: localVFS{}},
		},
		activePanel:  0,
		commandInput: ti,
		keys:         newKeyMap(),
		mode:         explorerMode,
		editor:       ta,
		progress:     prog,
		ProgressChan: make(chan ProgressMsg),
		ResultChan:   make(chan CommandResult),
		fuzzyInput:   fi,
	}
	for i := range m.panels {
		m.refreshPanel(i)
	}
	return m
}

func (m Model) Init() tea.Cmd {
	return textinput.Blink
}
