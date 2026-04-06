package src

import (
	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/charmbracelet/lipgloss"
)

// ─── Version ──────────────────────────────────────────────────────────────────
const (
	appName            = "ngt"
	version            = "v0.3"
	maxFileSizeForEdit = 10 * 1024 * 1024
	previewLines       = 40
	selectedMarker     = "▶"
)

// ─── Color Palette ────────────────────────────────────────────────────────────
const (
	colorBg       = "#0D1117" // deep midnight
	colorSurface  = "#161B22" // card bg
	colorSurface2 = "#21262D" // hover/selected
	colorBorder   = "#30363D" // subtle border
	colorBorderHi = "#58A6FF" // highlighted border (blue)
colorAccent   = "#58A6FF" // primary accent – GitHub-style blue
colorGreen    = "#3FB950" // success / added
colorRed      = "#F85149" // error / deleted
colorYellow   = "#D29922" // warning / modified
colorMuted    = "#8B949E" // secondary text
colorText     = "#C9D1D9" // primary text
colorSuccess  = "#3FB950"
colorDim      = "#484F58"
)

// ─── Component Styles ─────────────────────────────────────────────────────────
var (
	// App title bar
	titleBarStyle = lipgloss.NewStyle().
	Background(lipgloss.Color(colorSurface)).
	Foreground(lipgloss.Color(colorAccent)).
	Bold(true).
	Padding(0, 2)

	appNameStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color(colorAccent)).
	Bold(true)

	versionStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color(colorMuted))

	// Panel title shown above each file list
	panelTitleStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color(colorAccent)).
	Bold(true)

	activePanelBorder = lipgloss.NewStyle().
	Border(lipgloss.RoundedBorder()).
	BorderForeground(lipgloss.Color(colorBorderHi)).
	Padding(0, 1)

	inactivePanelBorder = lipgloss.NewStyle().
	Border(lipgloss.RoundedBorder()).
	BorderForeground(lipgloss.Color(colorBorder)).
	Padding(0, 1)

	// Path bar above panels
	pathStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color(colorText)).
	Background(lipgloss.Color(colorSurface)).
	Padding(0, 1)

	branchStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color(colorGreen)).
	Bold(true)

	vfsTagStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color(colorBg)).
	Background(lipgloss.Color(colorAccent)).
	Padding(0, 1).
	Bold(true)

	sortTagStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color(colorBg)).
	Background(lipgloss.Color(colorYellow)).
	Padding(0, 1)

	// File item styles
	dirStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color(colorAccent)).
	Bold(true)

	fileStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color(colorText))

	// Status bar
	statusBarStyle = lipgloss.NewStyle().
	Background(lipgloss.Color(colorSurface)).
	Padding(0, 1)

	errorStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color(colorRed)).
	Bold(true)

	successStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color(colorGreen)).
	Bold(true)

	warnStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color(colorYellow)).
	Bold(true)

	// Command input
	inputLabelStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color(colorMuted)).
	Padding(0, 1)

	inputStyle = lipgloss.NewStyle().
	Border(lipgloss.NormalBorder()).
	BorderForeground(lipgloss.Color(colorBorderHi)).
	Padding(0, 1).
	Background(lipgloss.Color(colorSurface))

	// Editor
	editorStyle = lipgloss.NewStyle().
	Border(lipgloss.RoundedBorder()).
	BorderForeground(lipgloss.Color(colorBorderHi)).
	Padding(1).
	Background(lipgloss.Color(colorSurface))

	// Preview pane
	previewStyle = lipgloss.NewStyle().
	Border(lipgloss.RoundedBorder()).
	BorderForeground(lipgloss.Color(colorBorder)).
	Padding(0, 1).
	Background(lipgloss.Color(colorBg))

	// Confirmation dialog
	dialogStyle = lipgloss.NewStyle().
	Border(lipgloss.RoundedBorder()).
	BorderForeground(lipgloss.Color(colorRed)).
	Padding(1, 3).
	Background(lipgloss.Color(colorSurface)).
	Align(lipgloss.Center)

	// Function bar (bottom)
	fBarStyle = lipgloss.NewStyle().
	Background(lipgloss.Color(colorSurface2)).
	Foreground(lipgloss.Color(colorMuted)).
	Padding(0, 0)

	fBarKeyStyle = lipgloss.NewStyle().
	Background(lipgloss.Color(colorDim)).
	Foreground(lipgloss.Color(colorText)).
	Padding(0, 1).
	Bold(true)

	fBarDescStyle = lipgloss.NewStyle().
	Background(lipgloss.Color(colorSurface2)).
	Foreground(lipgloss.Color(colorMuted)).
	Padding(0, 1)

	// Git
	gitModifiedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(colorYellow))
	gitAddedStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color(colorGreen))
	gitDeletedStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color(colorRed))

	// Podman
	podmanStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color(colorBg)).
	Background(lipgloss.Color("#892BE2")). // purple
	Padding(0, 1).
	Bold(true)

	// Chroma
	chromaStyle     = styles.Get("github-dark")
	chromaFormatter = formatters.TTY256
)

// fBarSegments returns the rendered bottom function bar
func renderFBar(w int) string {
	segments := []struct{ key, desc string }{
		{"F1", "Help"},
		{"F5", "Copy"},
		{"F6", "Move"},
		{"F8", "Delete"},
		{"^P", "Fuzzy"},
		{"^R", "Rename"},
		{"^O", "Shell"},
		{"^S", "Sort"},
		{"Tab", "Switch"},
		{"Q", "Quit"},
	}
	var parts []string
	for _, s := range segments {
		parts = append(parts, fBarKeyStyle.Render(s.key)+fBarDescStyle.Render(s.desc))
	}
	bar := lipgloss.JoinHorizontal(lipgloss.Top, parts...)
	return fBarStyle.Width(w).Render(bar)
}
