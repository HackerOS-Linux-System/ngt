package main

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	appName               = "ngt (night)"
	version               = "v0.2"
	maxFileSizeForEdit    = 10 * 1024 * 1024 // 10MB limit for editing
	previewLines          = 20
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

	previewStyle = lipgloss.NewStyle().
	Border(lipgloss.RoundedBorder()).
	BorderForeground(lipgloss.Color("#808080")).
	Padding(1).
	Margin(1).
	Background(lipgloss.Color("#1A1A1A"))

	// Chroma style
	chromaStyle = styles.Register(styles.Fallback)
	chromaFormatter = formatters.TTY256
)

type mode int

const (
	explorerMode mode = iota
	editorMode
	progressMode
)

type item struct {
	title, desc string
	status      string // For Git status
	selected    bool
}

func (i item) Title() string       { return i.title }
func (i item) Description() string { return i.desc }
func (i item) FilterValue() string { return i.title }

type commandResult struct {
	output string
	err    error
}

type progressMsg struct {
	percent float64
}

type tickMsg time.Time

type model struct {
	currentDir    string
	gitBranch     string
	fileList      list.Model
	commandInput  textinput.Model
	statusMsg     string
	keys          keyMap
	mode          mode
	editor        textarea.Model
	editorFile    string
	preview       viewport.Model
	selectedFiles map[string]bool
	progress      progress.Model
	quitting      bool
	progressChan  chan progressMsg
	resultChan    chan commandResult
}

type keyMap struct {
	quit     key.Binding
	execute  key.Binding
	save     key.Binding
	cancel   key.Binding
	refresh  key.Binding
	enter    key.Binding
	back     key.Binding
	selectIt key.Binding
	filter   key.Binding
}

func newKeyMap() keyMap {
	return keyMap{
		quit:     key.NewBinding(key.WithKeys("ctrl+c", "q"), key.WithHelp("q/ctrl+c", "quit")),
		execute:  key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "execute/enter")),
		save:     key.NewBinding(key.WithKeys("ctrl+s"), key.WithHelp("ctrl+s", "save file")),
		cancel:   key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel/back")),
		refresh:  key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
		enter:    key.NewBinding(key.WithKeys("enter")),
		back:     key.NewBinding(key.WithKeys("backspace"), key.WithHelp("backspace", "cd ..")),
		selectIt: key.NewBinding(key.WithKeys(" "), key.WithHelp("space", "select item")),
		filter:   key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
	}
}

func initialModel() model {
	wd, _ := os.Getwd()
	ti := textinput.New()
	ti.Placeholder = "Enter command (e.g., cd .., mv file1 file2)"
	ti.Focus()
	ti.Width = 80

	del := list.NewDefaultDelegate()
	del.Styles.SelectedTitle = del.Styles.SelectedTitle.Foreground(lipgloss.Color("#FFFFFF")).Bold(true)
	del.Styles.NormalTitle = del.Styles.NormalTitle.Foreground(lipgloss.Color("#CCCCCC"))

	l := list.New([]list.Item{}, del, 0, 0)
	l.Title = "Files in current directory"
	l.Styles.Title = subtitleStyle
	l.SetShowFilter(true) // Enable filtering

	ta := textarea.New()
	ta.Placeholder = "Edit your file here..."
	ta.Focus()

	pv := viewport.New(0, 0)
	pv.SetContent("Preview")

	prog := progress.New(progress.WithDefaultGradient())

	m := model{
		currentDir:    wd,
		fileList:      l,
		commandInput:  ti,
		keys:          newKeyMap(),
		mode:          explorerMode,
		editor:        ta,
		preview:       pv,
		selectedFiles: make(map[string]bool),
		progress:      prog,
		progressChan:  make(chan progressMsg),
		resultChan:    make(chan commandResult),
	}
	m.refreshFileList()
	m.updateGitBranch()
	return m
}

func (m *model) refreshFileList() {
	items := []list.Item{}
	files, err := os.ReadDir(m.currentDir)
	if err != nil {
		m.statusMsg = errorStyle.Render(fmt.Sprintf("Error reading directory: %v", err))
		return
	}

	gitStatus := m.getGitStatus()

	for _, file := range files {
		desc := "File"
		if file.IsDir() {
			desc = "Directory"
		}
		status := gitStatus[file.Name()]
		items = append(items, item{title: file.Name(), desc: desc, status: status})
	}
	m.fileList.SetItems(items)
	m.statusMsg = successStyle.Render("Directory refreshed")
}

func (m *model) getGitStatus() map[string]string {
	statusMap := make(map[string]string)
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = m.currentDir
	output, err := cmd.Output()
	if err != nil {
		return statusMap
	}
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		file := fields[1]
		st := line[:2]
		statusMap[file] = st
	}
	return statusMap
}

func (m *model) updateGitBranch() {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = m.currentDir
	output, err := cmd.Output()
	if err != nil {
		m.gitBranch = ""
		return
	}
	m.gitBranch = strings.TrimSpace(string(output))
}

func (m *model) updatePreview() {
	selected, ok := m.fileList.SelectedItem().(item)
	if !ok {
		m.preview.SetContent("")
		return
	}
	if selected.desc != "File" {
		m.preview.SetContent("Directory or non-text file")
		return
	}
	filePath := filepath.Join(m.currentDir, selected.title)
	ext := filepath.Ext(selected.title)
	if !isTextFile(ext) {
		m.preview.SetContent("Non-text file")
		return
	}
	byteContent, err := os.ReadFile(filePath)
	if err != nil {
		m.preview.SetContent("Error reading file")
		return
	}
	content := string(byteContent)
	lines := strings.Split(content, "\n")
	if len(lines) > previewLines {
		lines = lines[:previewLines]
	}
	previewContent := strings.Join(lines, "\n")

	// Syntax highlighting
	lexer := lexers.Match(selected.title)
	if lexer == nil {
		lexer = lexers.Fallback
	}
	iterator, err := lexer.Tokenise(nil, previewContent)
	if err != nil {
		m.preview.SetContent(previewContent)
		return
	}
	formatter := chromaFormatter
		var sb strings.Builder
		err = formatter.Format(&sb, chromaStyle, iterator)
		if err != nil {
			m.preview.SetContent(previewContent)
			return
		}
		m.preview.SetContent(sb.String())
}

func isTextFile(ext string) bool {
	textExts := []string{".txt", ".md", ".go", ".py", ".js", ".html", ".css"}
	for _, e := range textExts {
		if ext == e {
			return true
		}
	}
	return false
}

func (m model) Init() tea.Cmd {
	return textinput.Blink
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	var cmds []tea.Cmd

	switch msg := msg.(type) {
		case commandResult:
			if msg.err != nil {
				m.statusMsg = errorStyle.Render(fmt.Sprintf("Command failed: %v\n%s", msg.err, msg.output))
			} else {
				m.statusMsg = successStyle.Render(msg.output)
			}
			m.refreshFileList()
			m.mode = explorerMode

		case progressMsg:
			cmd = m.progress.SetPercent(msg.percent)
			cmds = append(cmds, cmd)
			if msg.percent >= 1.0 {
				m.mode = explorerMode
				m.statusMsg = successStyle.Render("Operation completed")
			}

		case tea.KeyMsg:
			if m.mode == progressMode {
				// Ignore keys during progress
				return m, nil
			}

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
				m.updateGitBranch()
				return m, nil
			}

			if key.Matches(msg, m.keys.back) {
				m.executeCommand("cd ..")
				return m, nil
			}

			if key.Matches(msg, m.keys.selectIt) && m.mode == explorerMode {
				selected, ok := m.fileList.SelectedItem().(item)
				if ok {
					fullPath := filepath.Join(m.currentDir, selected.title)
					m.selectedFiles[fullPath] = !m.selectedFiles[fullPath]
					// Update item selected state
					items := m.fileList.Items()
					for i, it := range items {
						ii := it.(item)
						if ii.title == selected.title {
							ii.selected = m.selectedFiles[fullPath]
							items[i] = ii
							break
						}
					}
					m.fileList.SetItems(items)
				}
				return m, nil
			}

			if key.Matches(msg, m.keys.execute) {
				if m.commandInput.Focused() {
					cmdStr := m.commandInput.Value()
					m.commandInput.Reset()
					m.executeCommand(cmdStr)
					return m, nil
				} else if m.mode == explorerMode {
					// Enter on list item
					selected, ok := m.fileList.SelectedItem().(item)
					if ok {
						if selected.desc == "Directory" {
							m.executeCommand("cd " + selected.title)
						} else {
							m.executeCommand("hedit " + selected.title)
						}
					}
					return m, nil
				}
			}

			case tea.WindowSizeMsg:
				h, v := msg.Width, msg.Height
				topHeight := lipgloss.Height(titleStyle.Render(appName+" "+version)) + 1
				listWidth := h/2 - 4
				previewWidth := h/2 - 4
				contentHeight := v - topHeight - 5
				m.fileList.SetSize(listWidth, contentHeight)
				m.preview.Width = previewWidth
				m.preview.Height = contentHeight
				m.editor.SetWidth(h - 4)
				m.editor.SetHeight(contentHeight)
				m.commandInput.Width = h - 4
				m.progress.Width = h - 4
	}

	if m.mode == explorerMode {
		var listCmd tea.Cmd
		m.fileList, listCmd = m.fileList.Update(msg)
		cmds = append(cmds, listCmd)

		// Update preview on selection change
		if _, ok := msg.(tea.KeyMsg); ok {
			m.updatePreview()
		}

		var inputCmd tea.Cmd
		m.commandInput, inputCmd = m.commandInput.Update(msg)
		cmds = append(cmds, inputCmd)
	} else if m.mode == editorMode {
		m.editor, cmd = m.editor.Update(msg)
		cmds = append(cmds, cmd)
	} else if m.mode == progressMode {
		updatedModel, cmd := m.progress.Update(msg)
		m.progress = updatedModel.(progress.Model)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

func helpString(b key.Binding) string {
	h := b.Help()
	return h.Key + ": " + h.Desc
}

func (m model) View() string {
	if m.quitting {
		return "Goodbye!\n"
	}

	title := titleStyle.Render(appName + " " + version)
	help := subtitleStyle.Render(strings.Join([]string{
		helpString(m.keys.quit),
						  helpString(m.keys.execute),
						  helpString(m.keys.refresh),
						  helpString(m.keys.back),
						  helpString(m.keys.selectIt),
						  helpString(m.keys.filter),
	}, " • "))

	if m.mode == progressMode {
		return lipgloss.JoinVertical(lipgloss.Left, title, m.progress.View(), m.statusMsg, help)
	}

	if m.mode == editorMode {
		help = subtitleStyle.Render(helpString(m.keys.save) + " • " + helpString(m.keys.cancel))
		editorView := editorStyle.Render(m.editor.View())
		status := m.statusMsg
		return lipgloss.JoinVertical(lipgloss.Left, title, subtitleStyle.Render("Editing: "+m.editorFile), editorView, status, help)
	}

	listView := listStyle.Width(m.fileList.Width()).Render(m.fileList.View())
	previewView := previewStyle.Width(m.preview.Width).Render(m.preview.View())
	content := lipgloss.JoinHorizontal(lipgloss.Top, listView, previewView)
	inputView := inputStyle.Render(m.commandInput.View())
	status := m.statusMsg
	pwd := subtitleStyle.Render("Current Dir: " + m.currentDir)
	if m.gitBranch != "" {
		pwd += " [Branch: " + m.gitBranch + "]"
	}
	return lipgloss.JoinVertical(lipgloss.Left, title, pwd, content, inputView, status, help)
}

func (m *model) executeCommand(cmdStr string) {
	args := strings.Fields(cmdStr)
	if len(args) == 0 {
		m.statusMsg = errorStyle.Render("Empty command")
		return
	}
	cmdName := args[0]

	switch cmdName {
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
			m.updateGitBranch()
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
			targets := []string{}
			if len(args) < 2 {
				// Use selected files if no arg
				for f := range m.selectedFiles {
					if m.selectedFiles[f] {
						targets = append(targets, f)
					}
				}
				if len(targets) == 0 {
					m.statusMsg = errorStyle.Render("rm requires a file/dir or selected items")
					return
				}
			} else {
				targets = append(targets, filepath.Join(m.currentDir, args[1]))
			}
			for _, target := range targets {
				err := os.RemoveAll(target)
				if err != nil {
					m.statusMsg = errorStyle.Render(fmt.Sprintf("rm failed: %v", err))
					return
				}
			}
			m.selectedFiles = make(map[string]bool)
			m.refreshFileList()
			m.statusMsg = successStyle.Render("Removed items")

		case "cp":
			if len(m.selectedFiles) > 0 && len(args) < 2 {
				m.statusMsg = errorStyle.Render("cp with selections requires destination")
				return
			}
			if len(args) < 3 && len(m.selectedFiles) == 0 {
				m.statusMsg = errorStyle.Render("cp requires source and destination")
				return
			}
			m.mode = progressMode
			go m.copyWithProgress(args)

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
			stat, err := os.Stat(file)
			if err != nil {
				m.statusMsg = errorStyle.Render(fmt.Sprintf("hedit failed: %v", err))
				return
			}
			if stat.Size() > maxFileSizeForEdit {
				m.statusMsg = errorStyle.Render("File too large to edit")
				return
			}
			content, err := os.ReadFile(file)
			if err != nil {
				m.statusMsg = errorStyle.Render(fmt.Sprintf("hedit failed: %v", err))
				return
			}
			m.editor.SetValue(string(content))
			m.editorFile = file
			m.mode = editorMode
			m.editor.Focus()
			m.statusMsg = successStyle.Render("Editing " + args[1])

		default:
			// Run system command in background
			m.runSystemCommand(cmdName, args[1:]...)
	}
}

func (m *model) runSystemCommand(name string, arg ...string) {
	go func() {
		cmd := exec.Command(name, arg...)
		cmd.Dir = m.currentDir
		output, err := cmd.CombinedOutput()
		m.resultChan <- commandResult{output: string(output), err: err}
	}()
}

func (m *model) copyWithProgress(args []string) {
	var err error
	dst := ""
	sources := []string{}

	if len(m.selectedFiles) > 0 {
		if len(args) > 1 {
			dst = args[1]
		} else {
			dst = "."
		}
		dst = filepath.Join(m.currentDir, dst)
		for f, sel := range m.selectedFiles {
			if sel {
				sources = append(sources, f)
			}
		}
	} else {
		sources = append(sources, filepath.Join(m.currentDir, args[1]))
		dst = filepath.Join(m.currentDir, args[2])
	}

	totalFiles := len(sources)
	var mu sync.Mutex
	processed := 0

	for _, src := range sources {
		err = copyFile(src, filepath.Join(dst, filepath.Base(src)), func(percent float64) {
			mu.Lock()
			overall := (float64(processed) + percent) / float64(totalFiles)
			m.progressChan <- progressMsg{percent: overall}
			mu.Unlock()
		})
		if err != nil {
			break
		}
		processed++
	}

	m.selectedFiles = make(map[string]bool)
	m.progressChan <- progressMsg{percent: 1.0}
	if err != nil {
		m.resultChan <- commandResult{err: err}
	} else {
		m.resultChan <- commandResult{output: "Copy completed"}
	}
}

func copyFile(src, dst string, progressCb func(float64)) error {
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
		return copyDir(src, dst, progressCb)
	}
	d, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer d.Close()

	total := stat.Size()
	var written int64
	buf := make([]byte, 4096)
	for {
		n, err := s.Read(buf)
		if err != nil && err != io.EOF {
			return err
		}
		if n == 0 {
			break
		}
		_, err = d.Write(buf[:n])
		if err != nil {
			return err
		}
		written += int64(n)
		progressCb(float64(written) / float64(total))
		time.Sleep(10 * time.Millisecond) // Simulate for visibility
	}
	return nil
}

func copyDir(src, dst string, progressCb func(float64)) error {
	var totalSize int64
	err := filepath.Walk(src, func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			totalSize += info.Size()
		}
		return nil
	})
	if err != nil {
		return err
	}

	var written int64
	err = filepath.Walk(src, func(path string, info fs.FileInfo, err error) error {
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
		return copyFile(path, dstPath, func(p float64) {
			inc := int64(float64(info.Size()) * p)
			written += inc
			progressCb(float64(written) / float64(totalSize))
		})
	})
	return err
}

func main() {
	m := initialModel()
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	go func() {
		for {
			select {
				case pm := <-m.progressChan:
					p.Send(pm)
				case cr := <-m.resultChan:
					p.Send(cr)
			}
		}
	}()
	if _, err := p.Run(); err != nil {
		fmt.Println("Error running program:", err)
		os.Exit(1)
	}
}
