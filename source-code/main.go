// main.go
package main

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	appName = "ngt (night)"
	version = "v0.1.0"
)

var (
	// Styles
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
)

type mode int

const (
	explorerMode mode = iota
	editorMode
)

type item struct {
	title, desc string
}

func (i item) Title() string       { return i.title }
func (i item) Description() string { return i.desc }
func (i item) FilterValue() string { return i.title }

type model struct {
	currentDir   string
	fileList     list.Model
	commandInput textinput.Model
	statusMsg    string
	keys         keyMap
	mode         mode
	editor       textarea.Model
	editorFile   string
	viewport     viewport.Model
	quitting     bool
}

type keyMap struct {
	quit     key.Binding
	execute  key.Binding
	save     key.Binding
	cancel   key.Binding
	refresh  key.Binding
}

func newKeyMap() keyMap {
	return keyMap{
		quit:    key.NewBinding(key.WithKeys("ctrl+c", "q"), key.WithHelp("q/ctrl+c", "quit")),
		execute: key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "execute command")),
		save:    key.NewBinding(key.WithKeys("ctrl+s"), key.WithHelp("ctrl+s", "save file")),
		cancel:  key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel/back")),
		refresh: key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
	}
}

func initialModel() model {
	wd, _ := os.Getwd()
	ti := textinput.New()
	ti.Placeholder = "Enter command (e.g., cd .., mv file1 file2)"
	ti.Focus()
	ti.Width = 80

	l := list.New([]list.Item{}, list.NewDefaultDelegate(), 0, 0)
	l.Title = "Files in current directory"
	l.Styles.Title = subtitleStyle

	ta := textarea.New()
	ta.Placeholder = "Edit your file here..."
	ta.Focus()

	vp := viewport.New(0, 0)

	m := model{
		currentDir:   wd,
		fileList:     l,
		commandInput: ti,
		keys:         newKeyMap(),
		mode:         explorerMode,
		editor:       ta,
		viewport:     vp,
	}

	m.refreshFileList()
	return m
}

func (m *model) refreshFileList() {
	items := []list.Item{}
	files, err := os.ReadDir(m.currentDir)
	if err != nil {
		m.statusMsg = errorStyle.Render(fmt.Sprintf("Error reading directory: %v", err))
		return
	}
	for _, file := range files {
		desc := "File"
		if file.IsDir() {
			desc = "Directory"
		}
		items = append(items, item{title: file.Name(), desc: desc})
	}
	m.fileList.SetItems(items)
	m.statusMsg = successStyle.Render("Directory refreshed")
}

func (m model) Init() tea.Cmd {
	return textinput.Blink
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.mode == editorMode {
			if key.Matches(msg, m.keys.save) {
				err := os.WriteFile(m.editorFile, []byte(m.editor.Value()), 0644)
				if err != nil {
					m.statusMsg = errorStyle.Render(fmt.Sprintf("Error saving file: %v", err))
				} else {
					m.statusMsg = successStyle.Render("File saved")
				}
				m.mode = explorerMode
				m.commandInput.Focus()
				return m, nil
			}
			if key.Matches(msg, m.keys.cancel) {
				m.mode = explorerMode
				m.commandInput.Focus()
				return m, nil
			}
			m.editor, cmd = m.editor.Update(msg)
			cmds = append(cmds, cmd)
			return m, tea.Batch(cmds...)
		}

		if key.Matches(msg, m.keys.quit) {
			m.quitting = true
			return m, tea.Quit
		}
		if key.Matches(msg, m.keys.refresh) {
			m.refreshFileList()
			return m, nil
		}
		if key.Matches(msg, m.keys.execute) && m.commandInput.Focused() {
			cmdStr := m.commandInput.Value()
			m.commandInput.Reset()
			m.executeCommand(cmdStr)
			return m, nil
		}
		if key.Matches(msg, m.keys.cancel) {
			// Optional: handle cancel in explorer
		}

	case tea.WindowSizeMsg:
		h, v := msg.Width, msg.Height
		topHeight := lipgloss.Height(titleStyle.Render(appName + " " + version)) + 1
		listHeight := v - topHeight - 5 // Room for input and status
		m.fileList.SetSize(h-4, listHeight)
		m.viewport.Width = h - 4
		m.viewport.Height = v - topHeight - 5
		m.editor.SetWidth(h - 4)
		m.editor.SetHeight(v - topHeight - 5)
		m.commandInput.Width = h - 4
	}

	if m.mode == explorerMode {
		m.fileList, cmd = m.fileList.Update(msg)
		cmds = append(cmds, cmd)
		m.commandInput, cmd = m.commandInput.Update(msg)
		cmds = append(cmds, cmd)
	} else {
		m.editor, cmd = m.editor.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

func (m model) View() string {
	if m.quitting {
		return "Goodbye!\n"
	}

	title := titleStyle.Render(appName + " " + version)
	help := subtitleStyle.Render(m.keys.quit.Help().String() + " • " + m.keys.execute.Help().String() + " • " + m.keys.refresh.Help().String())

	if m.mode == editorMode {
		help = subtitleStyle.Render(m.keys.save.Help().String() + " • " + m.keys.cancel.Help().String())
		editorView := editorStyle.Render(m.editor.View())
		status := m.statusMsg
		return lipgloss.JoinVertical(lipgloss.Left, title, subtitleStyle.Render("Editing: "+m.editorFile), editorView, status, help)
	}

	listView := listStyle.Render(m.fileList.View())
	inputView := inputStyle.Render(m.commandInput.View())
	status := m.statusMsg
	pwd := subtitleStyle.Render("Current Dir: " + m.currentDir)

	return lipgloss.JoinVertical(lipgloss.Left, title, pwd, listView, inputView, status, help)
}

func (m *model) executeCommand(cmdStr string) {
	args := strings.Fields(cmdStr)
	if len(args) == 0 {
		m.statusMsg = errorStyle.Render("Empty command")
		return
	}

	cmd := args[0]
	switch cmd {
	case "cd":
		if len(args) < 2 {
			m.statusMsg = errorStyle.Render("cd requires a directory")
			return
		}
		newDir := args[1]
		if !filepath.IsAbs(newDir) {
			newDir = filepath.Join(m.currentDir, newDir)
		}
		err := os.Chdir(newDir)
		if err != nil {
			m.statusMsg = errorStyle.Render(fmt.Sprintf("cd failed: %v", err))
			return
		}
		m.currentDir, _ = os.Getwd()
		m.refreshFileList()
		m.statusMsg = successStyle.Render("Changed directory to " + m.currentDir)

	case "mv":
		if len(args) < 3 {
			m.statusMsg = errorStyle.Render("mv requires source and destination")
			return
		}
		src := filepath.Join(m.currentDir, args[1])
		dst := filepath.Join(m.currentDir, args[2])
		err := os.Rename(src, dst)
		if err != nil {
			m.statusMsg = errorStyle.Render(fmt.Sprintf("mv failed: %v", err))
			return
		}
		m.refreshFileList()
		m.statusMsg = successStyle.Render("Moved " + args[1] + " to " + args[2])

	case "rm":
		if len(args) < 2 {
			m.statusMsg = errorStyle.Render("rm requires a file/dir")
			return
		}
		target := filepath.Join(m.currentDir, args[1])
		err := os.RemoveAll(target)
		if err != nil {
			m.statusMsg = errorStyle.Render(fmt.Sprintf("rm failed: %v", err))
			return
		}
		m.refreshFileList()
		m.statusMsg = successStyle.Render("Removed " + args[1])

	case "cp":
		if len(args) < 3 {
			m.statusMsg = errorStyle.Render("cp requires source and destination")
			return
		}
		src := filepath.Join(m.currentDir, args[1])
		dst := filepath.Join(m.currentDir, args[2])
		err := copyFile(src, dst)
		if err != nil {
			m.statusMsg = errorStyle.Render(fmt.Sprintf("cp failed: %v", err))
			return
		}
		m.refreshFileList()
		m.statusMsg = successStyle.Render("Copied " + args[1] + " to " + args[2])

	case "touch":
		if len(args) < 2 {
			m.statusMsg = errorStyle.Render("touch requires a filename")
			return
		}
		file := filepath.Join(m.currentDir, args[1])
		f, err := os.Create(file)
		if err != nil {
			m.statusMsg = errorStyle.Render(fmt.Sprintf("touch failed: %v", err))
			return
		}
		f.Close()
		m.refreshFileList()
		m.statusMsg = successStyle.Render("Created file " + args[1])

	case "mkdir":
		if len(args) < 2 {
			m.statusMsg = errorStyle.Render("mkdir requires a directory name")
			return
		}
		dir := filepath.Join(m.currentDir, args[1])
		err := os.MkdirAll(dir, os.ModePerm)
		if err != nil {
			m.statusMsg = errorStyle.Render(fmt.Sprintf("mkdir failed: %v", err))
			return
		}
		m.refreshFileList()
		m.statusMsg = successStyle.Render("Created directory " + args[1])

	case "hedit":
		if len(args) < 2 {
			m.statusMsg = errorStyle.Render("hedit requires a filename")
			return
		}
		file := filepath.Join(m.currentDir, args[1])
		content, err := os.ReadFile(file)
		if err != nil && !os.IsNotExist(err) {
			m.statusMsg = errorStyle.Render(fmt.Sprintf("hedit failed: %v", err))
			return
		}
		m.editor.SetValue(string(content))
		m.editorFile = file
		m.mode = editorMode
		m.editor.Focus()
		m.statusMsg = successStyle.Render("Editing " + args[1])

	default:
		// Fallback to system command if not built-in
		sysCmd := exec.Command(cmd, args[1:]...)
		sysCmd.Dir = m.currentDir
		output, err := sysCmd.CombinedOutput()
		if err != nil {
			m.statusMsg = errorStyle.Render(fmt.Sprintf("Command failed: %v\n%s", err, output))
		} else {
			m.statusMsg = successStyle.Render(string(output))
		}
		m.refreshFileList()
	}
}

func copyFile(src, dst string) error {
	s, err := os.Open(src)
	if err != nil {
		return err
	}
	defer s.Close()

	stat, err := s.Stat()
	if err != nil {
		return err
	}
	if stat.IsDir() {
		return copyDir(src, dst)
	}

	d, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer d.Close()

	_, err = io.Copy(d, s)
	return err
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		dstPath := filepath.Join(dst, relPath)

		if info.IsDir() {
			return os.Mkdir(dstPath, info.Mode())
		}

		return copyFile(path, dstPath)
	})
}

func main() {
	p := tea.NewProgram(initialModel(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Println("Error running program:", err)
		os.Exit(1)
	}
}
