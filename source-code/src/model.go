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
	status      string
	selected    bool
	isDir       bool
	size        int64
	modTime     time.Time
}

func (i item) Title() string {
	title := i.title
	prefix := "  "
	if i.selected {
		prefix = selectedMarker + " "
	}
	if i.isDir {
		title = dirStyle.Render(title + "/")
	} else {
		title = fileStyle.Render(title)
	}
	if i.status != "" {
		switch i.status[0] {
			case 'M':
				title = gitModifiedStyle.Render(i.title)
			case 'A':
				title = gitAddedStyle.Render(i.title)
			case 'D':
				title = gitDeletedStyle.Render(i.title)
		}
	}
	return prefix + title
}

func (i item) Description() string {
	kind := "file"
	if i.isDir {
		kind = "dir "
	}
	sizeStr := humanSize(i.size)
	return fmt.Sprintf("%s │ %s │ %s", kind, sizeStr, i.modTime.Format("2006-01-02 15:04"))
}

func (i item) FilterValue() string { return i.title }

func humanSize(s int64) string {
	const unit = 1024
	if s < unit {
		return fmt.Sprintf("%d B", s)
	}
	div, exp := int64(unit), 0
	for n := s / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(s)/float64(div), "KMGTPE"[exp])
}

type CommandResult struct {
	Output string
	Err    error
}

type ProgressMsg struct {
	Percent float64
}

type tickMsg time.Time

// SortMode defines how file list is sorted
type SortMode int

const (
	SortByName SortMode = iota
	SortBySize
	SortByDate
	SortByExt
)

func (s SortMode) String() string {
	return [...]string{"Name", "Size", "Date", "Ext"}[s]
}

type panel struct {
	currentDir    string
	gitBranch     string
	fileList      list.Model
	selectedFiles map[string]bool
	preview       viewport.Model
	vfs           vfsHandler
	sortMode      SortMode
}

type Model struct {
	panels       [2]panel
	activePanel  int
	commandInput textinput.Model
	statusMsg    string
	keys         keyMap
	mode         mode

	editor     textarea.Model
	editorFile string

	progress progress.Model
	quitting bool

	ProgressChan chan ProgressMsg
	ResultChan   chan CommandResult

	fuzzyInput   textinput.Model
	fuzzyResults []string

	bulkRenameFrom string
	bulkRenameTo   string

	// confirmation dialog
	confirmMsg    string
	confirmAction func()

	// podman browser
	podmanContainers []string

	subShell bool

	termW int
	termH int
}

func InitialModel() Model {
	wd, _ := os.Getwd()

	ti := textinput.New()
	ti.Placeholder = "cmd: cd, cp, mv, rm, mkdir, touch, sftp, podman, hedit…"
	ti.Focus()
	ti.Width = 80

	del := list.NewDefaultDelegate()
	del.Styles.SelectedTitle = del.Styles.SelectedTitle.
	Foreground(lipgloss.Color(colorAccent)).
	Background(lipgloss.Color(colorSurface2)).
	Bold(true).
	BorderLeft(true).
	BorderStyle(lipgloss.ThickBorder()).
	BorderForeground(lipgloss.Color(colorAccent))
	del.Styles.NormalTitle = del.Styles.NormalTitle.
	Foreground(lipgloss.Color(colorText))
	del.Styles.SelectedDesc = del.Styles.SelectedDesc.
	Foreground(lipgloss.Color(colorMuted)).
	Background(lipgloss.Color(colorSurface2))
	del.Styles.NormalDesc = del.Styles.NormalDesc.
	Foreground(lipgloss.Color(colorMuted))
	del.ShowDescription = true

	l1 := list.New([]list.Item{}, del, 0, 0)
	l1.Title = "● Left"
	l1.Styles.Title = panelTitleStyle
	l1.SetShowFilter(true)
	l1.SetFilteringEnabled(true)

	l2 := list.New([]list.Item{}, del, 0, 0)
	l2.Title = "○ Right"
	l2.Styles.Title = panelTitleStyle
	l2.SetShowFilter(true)
	l2.SetFilteringEnabled(true)

	ta := textarea.New()
	ta.Placeholder = "Edit your file here…"
	ta.Focus()

	pv1 := viewport.New(0, 0)
	pv1.SetContent("Select a file to preview")
	pv2 := viewport.New(0, 0)
	pv2.SetContent("Select a file to preview")

	prog := progress.New(
		progress.WithGradient(colorAccent, colorSuccess),
			     progress.WithoutPercentage(),
	)

	fi := textinput.New()
	fi.Placeholder = "Fuzzy search…"

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
		ProgressChan: make(chan ProgressMsg, 10),
		ResultChan:   make(chan CommandResult, 10),
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
