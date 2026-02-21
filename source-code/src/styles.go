package src

import (
	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/charmbracelet/lipgloss"
)

const (
	appName            = "ngt (night)"
	version            = "v0.2"
	maxFileSizeForEdit = 10 * 1024 * 1024 // 10MB limit for editing
	previewLines       = 20
)

var (
	titleStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color("#FFD700")).
	Background(lipgloss.Color("#1E1E1E")).
	Padding(0, 1).
	Bold(true)
	subtitleStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color("#00FF00")).
	Italic(true)
	errorStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color("#FF0000")).
	Bold(true)
	successStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color("#00FF00")).
	Bold(true)
	listStyle = lipgloss.NewStyle().
	Border(lipgloss.RoundedBorder()).
	BorderForeground(lipgloss.Color("#808080")).
	Padding(1).
	Margin(1)
	inputStyle = lipgloss.NewStyle().
	Border(lipgloss.NormalBorder()).
	BorderForeground(lipgloss.Color("#FFFFFF")).
	Padding(0, 1).
	Background(lipgloss.Color("#2F2F2F"))
	editorStyle = lipgloss.NewStyle().
	Border(lipgloss.RoundedBorder()).
	BorderForeground(lipgloss.Color("#00FFFF")).
	Padding(1).
	Margin(1).
	Background(lipgloss.Color("#1A1A1A"))
	previewStyle = lipgloss.NewStyle().
	Border(lipgloss.RoundedBorder()).
	BorderForeground(lipgloss.Color("#808080")).
	Padding(1).
	Margin(1).
	Background(lipgloss.Color("#1A1A1A"))
	chromaStyle     = styles.Register(styles.Fallback)
	chromaFormatter = formatters.TTY256
	gitModifiedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF0000"))
	gitAddedStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#00FF00"))
	gitDeletedStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFF00"))
	fBarStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color("#FFFF00")).
	Background(lipgloss.Color("#000000")).
	Width(80).
	Padding(0, 1).
	Bold(true)
	fBarContent = "1Help  2Menu  3View  4Edit  5Copy  6Move  7Mkdir  8Delete  9PulMn  10Quit"
)
