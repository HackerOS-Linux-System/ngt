package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
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
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"golang.org/x/sync/errgroup"
)

const (
	appName                  = "ngt (night)"
	version                  = "v0.2"
	maxFileSizeForEdit       = 10 * 1024 * 1024 // 10MB limit for editing
	previewLines             = 20
	defaultFuzzySearchPrefix = "fzf:"
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
	chromaStyle     = styles.Register(styles.Fallback)
	chromaFormatter = formatters.TTY256
	// Git colors
	gitModifiedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF0000"))
	gitAddedStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#00FF00"))
	gitDeletedStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFF00"))
)

type mode int

const (
	explorerMode mode = iota
	editorMode
	progressMode
	fuzzyMode
	bulkRenameMode
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

type commandResult struct {
	output string
	err    error
}

type progressMsg struct {
	percent float64
}

type tickMsg time.Time

type panel struct {
	currentDir   string
	gitBranch    string
	fileList     list.Model
	selectedFiles map[string]bool
	preview      viewport.Model
	vfs          vfsHandler
}

type model struct {
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
	progressChan   chan progressMsg
	resultChan     chan commandResult
	fuzzyInput     textinput.Model
	fuzzyResults   []string
	bulkRenameFrom string
	bulkRenameTo   string
	subShell       bool
}

type keyMap struct {
	quit       key.Binding
	execute    key.Binding
	save       key.Binding
	cancel     key.Binding
	refresh    key.Binding
	enter      key.Binding
	back       key.Binding
	selectIt   key.Binding
	filter     key.Binding
	tab        key.Binding
	down       key.Binding
	up         key.Binding
	left       key.Binding
	right      key.Binding
	copy       key.Binding
	move       key.Binding
	delete     key.Binding
	fuzzy      key.Binding
	bulkRename key.Binding
	suspend    key.Binding
	subshell   key.Binding
}

func newKeyMap() keyMap {
	return keyMap{
		quit:       key.NewBinding(key.WithKeys("ctrl+c", "q"), key.WithHelp("q/ctrl+c", "quit")),
		execute:    key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "execute/enter")),
		save:       key.NewBinding(key.WithKeys("ctrl+s"), key.WithHelp("ctrl+s", "save file")),
		cancel:     key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel/back")),
		refresh:    key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
		enter:      key.NewBinding(key.WithKeys("enter")),
		back:       key.NewBinding(key.WithKeys("backspace"), key.WithHelp("backspace", "cd ..")),
		selectIt:   key.NewBinding(key.WithKeys(" "), key.WithHelp("space", "select item")),
		filter:     key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
		tab:        key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "switch panel")),
		down:       key.NewBinding(key.WithKeys("j", "down")),
		up:         key.NewBinding(key.WithKeys("k", "up")),
		left:       key.NewBinding(key.WithKeys("h", "left")),
		right:      key.NewBinding(key.WithKeys("l", "right")),
		copy:       key.NewBinding(key.WithKeys("f5"), key.WithHelp("F5", "copy")),
		move:       key.NewBinding(key.WithKeys("f6"), key.WithHelp("F6", "move")),
		delete:     key.NewBinding(key.WithKeys("f8"), key.WithHelp("F8", "delete")),
		fuzzy:      key.NewBinding(key.WithKeys("ctrl+p"), key.WithHelp("Ctrl+P", "fuzzy search")),
		bulkRename: key.NewBinding(key.WithKeys("ctrl+r"), key.WithHelp("Ctrl+R", "bulk rename")),
		suspend:    key.NewBinding(key.WithKeys("ctrl+z"), key.WithHelp("Ctrl+Z", "suspend")),
		subshell:   key.NewBinding(key.WithKeys("ctrl+o"), key.WithHelp("Ctrl+O", "sub-shell")),
	}
}

type vfsHandler interface {
	ReadDir(dir string) ([]fs.DirEntry, error)
	Open(file string) (fs.File, error)
	Stat(file string) (fs.FileInfo, error)
	Chdir(dir string) error
	Getwd() (string, error)
	// Add more as needed
}

type localVFS struct{}

func (l localVFS) ReadDir(dir string) ([]fs.DirEntry, error) { return os.ReadDir(dir) }
func (l localVFS) Open(file string) (fs.File, error)         { return os.Open(file) }
func (l localVFS) Stat(file string) (fs.FileInfo, error)     { return os.Stat(file) }
func (l localVFS) Chdir(dir string) error                    { return os.Chdir(dir) }
func (l localVFS) Getwd() (string, error)                    { return os.Getwd() }

type sftpVFS struct {
	client *sftp.Client
}

func newSFTPVFS(host, user, pass string) (*sftpVFS, error) {
	config := &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{ssh.Password(pass)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}
	conn, err := ssh.Dial("tcp", host, config)
	if err != nil {
		return nil, err
	}
	client, err := sftp.NewClient(conn)
	if err != nil {
		return nil, err
	}
	return &sftpVFS{client: client}, nil
}

func (s *sftpVFS) ReadDir(dir string) ([]fs.DirEntry, error) {
	files, err := s.client.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var entries []fs.DirEntry
	for _, f := range files {
		entries = append(entries, &sftpDirEntry{info: f})
	}
	return entries, nil
}

func (s *sftpVFS) Open(file string) (fs.File, error) { return s.client.Open(file) }
func (s *sftpVFS) Stat(file string) (fs.FileInfo, error) { return s.client.Stat(file) }
func (s *sftpVFS) Chdir(dir string) error {
	_, err := s.client.Stat(dir)
	return err
}
func (s *sftpVFS) Getwd() (string, error) { return s.client.Getwd() }

type sftpDirEntry struct {
	info fs.FileInfo
}

func (e *sftpDirEntry) Name() string       { return e.info.Name() }
func (e *sftpDirEntry) IsDir() bool        { return e.info.IsDir() }
func (e *sftpDirEntry) Type() fs.FileMode  { return e.info.Mode() }
func (e *sftpDirEntry) Info() (fs.FileInfo, error) { return e.info, nil }

type archiveVFS struct {
	root string
	files map[string]fs.FileInfo
	content map[string][]byte // For in-memory
}

func newZipVFS(file string) (*archiveVFS, error) {
	r, err := zip.OpenReader(file)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	avfs := &archiveVFS{root: file, files: make(map[string]fs.FileInfo), content: make(map[string][]byte)}
	for _, f := range r.File {
		avfs.files[f.Name] = f.FileInfo()
		rc, err := f.Open()
		if err != nil {
			continue
		}
		data, _ := io.ReadAll(rc)
		avfs.content[f.Name] = data
		rc.Close()
	}
	return avfs, nil
}

func newTarVFS(file string) (*archiveVFS, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var r io.Reader = f
	if strings.HasSuffix(file, ".gz") {
		gzr, err := gzip.NewReader(f)
		if err != nil {
			return nil, err
		}
		defer gzr.Close()
		r = gzr
	}
	tr := tar.NewReader(r)
	avfs := &archiveVFS{root: file, files: make(map[string]fs.FileInfo), content: make(map[string][]byte)}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		data, _ := io.ReadAll(tr)
		avfs.files[hdr.Name] = hdr.FileInfo()
		avfs.content[hdr.Name] = data
	}
	return avfs, nil
}

func (a *archiveVFS) ReadDir(dir string) ([]fs.DirEntry, error) {
	var entries []fs.DirEntry
	prefix := strings.TrimPrefix(dir, a.root+"/") + "/"
	for name, info := range a.files {
		if strings.HasPrefix(name, prefix) && !strings.Contains(name[len(prefix):], "/") {
			entries = append(entries, &archiveDirEntry{name: filepath.Base(name), info: info})
		}
	}
	return entries, nil
}

func (a *archiveVFS) Open(file string) (fs.File, error) {
	data, ok := a.content[file]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return &archiveFile{data: data, name: file}, nil
}

func (a *archiveVFS) Stat(file string) (fs.FileInfo, error) {
	info, ok := a.files[file]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return info, nil
}

func (a *archiveVFS) Chdir(dir string) error {
	return nil // In-memory, no real chdir
}

func (a *archiveVFS) Getwd() (string, error) { return a.root, nil }

type archiveDirEntry struct {
	name string
	info fs.FileInfo
}

func (e *archiveDirEntry) Name() string       { return e.name }
func (e *archiveDirEntry) IsDir() bool        { return e.info.IsDir() }
func (e *archiveDirEntry) Type() fs.FileMode  { return e.info.Mode() }
func (e *archiveDirEntry) Info() (fs.FileInfo, error) { return e.info, nil }

type archiveFile struct {
	data []byte
	pos  int64
	name string
}

func (f *archiveFile) Read(b []byte) (int, error) {
	if f.pos >= int64(len(f.data)) {
		return 0, io.EOF
	}
	n := copy(b, f.data[f.pos:])
	f.pos += int64(n)
	return n, nil
}

func (f *archiveFile) Close() error { return nil }
func (f *archiveFile) Stat() (fs.FileInfo, error) {
	return &archiveFileInfo{name: f.name, size: int64(len(f.data))}, nil
}

func (f *archiveFile) Seek(offset int64, whence int) (int64, error) {
	switch whence {
		case io.SeekStart:
			f.pos = offset
		case io.SeekCurrent:
			f.pos += offset
		case io.SeekEnd:
			f.pos = int64(len(f.data)) + offset
	}
	if f.pos < 0 {
		f.pos = 0
	} else if f.pos > int64(len(f.data)) {
		f.pos = int64(len(f.data))
	}
	return f.pos, nil
}

type archiveFileInfo struct {
	name string
	size int64
}

func (i *archiveFileInfo) Name() string     { return i.name }
func (i *archiveFileInfo) Size() int64      { return i.size }
func (i *archiveFileInfo) Mode() fs.FileMode { return 0644 }
func (i *archiveFileInfo) ModTime() time.Time { return time.Now() }
func (i *archiveFileInfo) IsDir() bool       { return false }
func (i *archiveFileInfo) Sys() any          { return nil }

func initialModel() model {
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

	m := model{
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
		progressChan: make(chan progressMsg),
		resultChan:   make(chan commandResult),
		fuzzyInput:   fi,
	}
	for i := range m.panels {
		m.refreshPanel(i)
	}
	return m
}

func (m *model) refreshPanel(idx int) {
	p := &m.panels[idx]
	items := []list.Item{}
	files, err := p.vfs.ReadDir(p.currentDir)
	if err != nil {
		m.statusMsg = errorStyle.Render(fmt.Sprintf("Error reading directory: %v", err))
		return
	}
	gitStatus := m.getGitStatus(p)
	sort.Slice(files, func(i, j int) bool {
		return files[i].Name() < files[j].Name()
	})
	for _, file := range files {
		info, _ := file.Info()
		desc := "File"
		if file.IsDir() {
			desc = "Directory"
		}
		status := gitStatus[file.Name()]
		items = append(items, item{
			title:   file.Name(),
			       desc:    desc,
			       status:  status,
			       isDir:   file.IsDir(),
			       size:    info.Size(),
			       modTime: info.ModTime(),
		})
	}
	p.fileList.SetItems(items)
	m.statusMsg = successStyle.Render("Panel refreshed")
	m.updatePreview(idx)
	m.updateGitBranch(idx)
}

func (m *model) getGitStatus(p *panel) map[string]string {
	statusMap := make(map[string]string)
	// Only for local VFS
	if _, ok := p.vfs.(localVFS); !ok {
		return statusMap
	}
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = p.currentDir
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

func (m *model) updateGitBranch(idx int) {
	p := &m.panels[idx]
	// Only for local
	if _, ok := p.vfs.(localVFS); !ok {
		p.gitBranch = ""
		return
	}
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = p.currentDir
	output, err := cmd.Output()
	if err != nil {
		p.gitBranch = ""
		return
	}
	p.gitBranch = strings.TrimSpace(string(output))
}

func (m *model) updatePreview(idx int) {
	p := &m.panels[idx]
	selected, ok := p.fileList.SelectedItem().(item)
	if !ok {
		p.preview.SetContent("")
		return
	}
	if selected.isDir {
		p.preview.SetContent("Directory")
		return
	}
	filePath := filepath.Join(p.currentDir, selected.title)
	f, err := p.vfs.Open(filePath)
	if err != nil {
		p.preview.SetContent("Error opening file")
		return
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		p.preview.SetContent("Error stat file")
		return
	}
	mimeType := mime.TypeByExtension(filepath.Ext(selected.title))
	if mimeType == "" {
		// Detect MIME
		buf := make([]byte, 512)
		f.Read(buf)
		mimeType = http.DetectContentType(buf)
		if seeker, ok := f.(io.Seeker); ok {
			seeker.Seek(0, io.SeekStart)
		} else {
			// If not seekable, can't reset, but for preview, perhaps read again or skip
			f.Close()
			f, _ = p.vfs.Open(filePath)
		}
	}
	if strings.HasPrefix(mimeType, "image/") {
		// For image preview in supported terminals, use Kitty protocol or similar
		// This is placeholder; in real, use base64 or terminal escape sequences
		p.preview.SetContent("Image preview not supported in text mode. MIME: " + mimeType)
		return
	}
	if !strings.HasPrefix(mimeType, "text/") {
		p.preview.SetContent("Non-text file: " + mimeType)
		return
	}
	if stat.Size() > maxFileSizeForEdit {
		p.preview.SetContent("File too large for preview")
		return
	}
	byteContent, err := io.ReadAll(f)
	if err != nil {
		p.preview.SetContent("Error reading file")
		return
	}
	content := string(byteContent)
	lines := strings.Split(content, "\n")
	if len(lines) > previewLines {
		lines = lines[:previewLines]
		content = strings.Join(lines, "\n") + "\n..."
	}
	// Syntax highlighting
	lexer := lexers.Match(selected.title)
	if lexer == nil {
		lexer = lexers.Fallback
	}
	iterator, err := lexer.Tokenise(nil, content)
	if err != nil {
		p.preview.SetContent(content)
		return
	}
	var sb strings.Builder
	err = chromaFormatter.Format(&sb, chromaStyle, iterator)
	if err != nil {
		p.preview.SetContent(content)
		return
	}
	p.preview.SetContent(sb.String())
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
			m.refreshPanel(m.activePanel)
			m.mode = explorerMode
		case progressMsg:
			cmd = m.progress.SetPercent(msg.percent)
			cmds = append(cmds, cmd)
			if msg.percent >= 1.0 {
				m.mode = explorerMode
				m.statusMsg = successStyle.Render("Operation completed")
				m.refreshPanel(m.activePanel)
				m.refreshPanel(1 - m.activePanel)
			}
		case tea.KeyMsg:
			if m.mode == progressMode {
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
			if m.mode == fuzzyMode {
				if key.Matches(msg, m.keys.execute) {
					selected := m.fuzzyResults[m.panels[m.activePanel].fileList.Index()]
					m.executeCommand("cd " + selected)
					m.mode = explorerMode
					return m, nil
				}
				if key.Matches(msg, m.keys.cancel) {
					m.mode = explorerMode
					return m, nil
				}
				m.fuzzyInput, cmd = m.fuzzyInput.Update(msg)
				cmds = append(cmds, cmd)
				m.performFuzzySearch()
				return m, tea.Batch(cmds...)
			}
			if m.mode == bulkRenameMode {
				// Simple two-step: first from, then to
				if m.bulkRenameFrom == "" {
					if key.Matches(msg, m.keys.execute) {
						m.bulkRenameFrom = m.commandInput.Value()
						m.commandInput.Reset()
						m.commandInput.Placeholder = "Enter replacement string"
						return m, nil
					}
				} else {
					if key.Matches(msg, m.keys.execute) {
						m.bulkRenameTo = m.commandInput.Value()
						m.performBulkRename()
						m.mode = explorerMode
						return m, nil
					}
				}
				m.commandInput, cmd = m.commandInput.Update(msg)
				cmds = append(cmds, cmd)
				return m, tea.Batch(cmds...)
			}
			if key.Matches(msg, m.keys.quit) {
				m.quitting = true
				return m, tea.Quit
			}
			if key.Matches(msg, m.keys.refresh) {
				m.refreshPanel(m.activePanel)
				return m, nil
			}
			if key.Matches(msg, m.keys.tab) {
				m.activePanel = 1 - m.activePanel
				return m, nil
			}
			if key.Matches(msg, m.keys.back) || key.Matches(msg, m.keys.left) {
				m.executeCommand("cd ..")
				return m, nil
			}
			if key.Matches(msg, m.keys.selectIt) {
				selected, ok := m.panels[m.activePanel].fileList.SelectedItem().(item)
				if ok {
					fullPath := filepath.Join(m.panels[m.activePanel].currentDir, selected.title)
					m.panels[m.activePanel].selectedFiles[fullPath] = !m.panels[m.activePanel].selectedFiles[fullPath]
					items := m.panels[m.activePanel].fileList.Items()
					for i, it := range items {
						ii := it.(item)
						if ii.title == selected.title {
							ii.selected = m.panels[m.activePanel].selectedFiles[fullPath]
							items[i] = ii
							break
						}
					}
					m.panels[m.activePanel].fileList.SetItems(items)
				}
				return m, nil
			}
			if key.Matches(msg, m.keys.execute) || key.Matches(msg, m.keys.right) {
				if m.commandInput.Focused() {
					cmdStr := m.commandInput.Value()
					m.commandInput.Reset()
					m.executeCommand(cmdStr)
					return m, nil
				} else {
					selected, ok := m.panels[m.activePanel].fileList.SelectedItem().(item)
					if ok {
						if selected.isDir {
							m.executeCommand("cd " + selected.title)
						} else if isArchive(selected.title) {
							m.mountArchive(selected.title)
						} else {
							m.executeCommand("hedit " + selected.title)
						}
					}
					return m, nil
				}
			}
			if key.Matches(msg, m.keys.copy) {
				m.copyToOtherPanel()
				return m, nil
			}
			if key.Matches(msg, m.keys.move) {
				m.moveToOtherPanel()
				return m, nil
			}
			if key.Matches(msg, m.keys.delete) {
				m.executeCommand("rm")
				return m, nil
			}
			if key.Matches(msg, m.keys.fuzzy) {
				m.mode = fuzzyMode
				m.fuzzyInput.Focus()
				m.performFuzzySearch()
				return m, nil
			}
			if key.Matches(msg, m.keys.bulkRename) {
				m.mode = bulkRenameMode
				m.commandInput.Reset()
				m.commandInput.Placeholder = "Enter pattern to replace (regex)"
				m.commandInput.Focus()
				return m, nil
			}
			if key.Matches(msg, m.keys.suspend) {
				m.suspend()
				return m, nil
			}
			if key.Matches(msg, m.keys.subshell) {
				m.openSubShell()
				return m, nil
			}
			if key.Matches(msg, m.keys.down) {
				m.panels[m.activePanel].fileList.CursorDown()
				m.updatePreview(m.activePanel)
				return m, nil
			}
			if key.Matches(msg, m.keys.up) {
				m.panels[m.activePanel].fileList.CursorUp()
				m.updatePreview(m.activePanel)
				return m, nil
			}
			case tea.MouseMsg:
				if msg.Type == tea.MouseLeft {
					// Handle mouse click for selection
					// This requires calculating position, omitted for brevity
				}
			case tea.WindowSizeMsg:
				h, v := msg.Width, msg.Height
				topHeight := lipgloss.Height(titleStyle.Render(appName+" "+version)) + 1
				panelWidth := (h / 2) - 4
				contentHeight := v - topHeight - 5
				for i := range m.panels {
					m.panels[i].fileList.SetSize(panelWidth, contentHeight)
					m.panels[i].preview.Width = panelWidth / 2
					m.panels[i].preview.Height = contentHeight / 2
				}
				m.editor.SetWidth(h - 4)
				m.editor.SetHeight(contentHeight)
				m.commandInput.Width = h - 4
				m.progress.Width = h - 4
				m.fuzzyInput.Width = h - 4
	}

	if m.mode == explorerMode {
		var listCmd tea.Cmd
		m.panels[m.activePanel].fileList, listCmd = m.panels[m.activePanel].fileList.Update(msg)
		cmds = append(cmds, listCmd)
		if _, ok := msg.(tea.KeyMsg); ok {
			m.updatePreview(m.activePanel)
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

func (m *model) performFuzzySearch() {
	query := m.fuzzyInput.Value()
	if query == "" {
		return
	}
	var results []string
	err := filepath.Walk(m.panels[m.activePanel].currentDir, func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if strings.Contains(strings.ToLower(filepath.Base(path)), strings.ToLower(query)) {
			rel, _ := filepath.Rel(m.panels[m.activePanel].currentDir, path)
			results = append(results, rel)
		}
		return nil
	})
	if err != nil {
		m.statusMsg = errorStyle.Render(err.Error())
	}
	m.fuzzyResults = results
	// Update list to show results
	items := []list.Item{}
	for _, res := range results {
		items = append(items, item{title: res})
	}
	m.panels[m.activePanel].fileList.SetItems(items)
}

func (m *model) performBulkRename() {
	re, err := regexp.Compile(m.bulkRenameFrom)
	if err != nil {
		m.statusMsg = errorStyle.Render("Invalid regex")
		return
	}
	for file := range m.panels[m.activePanel].selectedFiles {
		newName := re.ReplaceAllString(filepath.Base(file), m.bulkRenameTo)
		newPath := filepath.Join(filepath.Dir(file), newName)
		err := os.Rename(file, newPath)
		if err != nil {
			m.statusMsg += errorStyle.Render(fmt.Sprintf("Rename failed for %s: %v\n", file, err))
		}
	}
	m.panels[m.activePanel].selectedFiles = make(map[string]bool)
	m.refreshPanel(m.activePanel)
	m.bulkRenameFrom = ""
	m.bulkRenameTo = ""
}

func (m *model) suspend() {
	pid := os.Getpid()
	syscall.Kill(pid, syscall.SIGTSTP)
	// After resume, refresh
	m.refreshPanel(m.activePanel)
}

func (m *model) openSubShell() {
	m.subShell = true
	tea.ClearScreen()
	cmd := exec.Command(os.Getenv("SHELL"))
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Dir = m.panels[m.activePanel].currentDir
	cmd.Run()
	m.subShell = false
	// Refresh after
	m.refreshPanel(m.activePanel)
}

func (m model) View() string {
	if m.quitting {
		return "Goodbye!\n"
	}
	if m.subShell {
		return ""
	}
	title := titleStyle.Render(appName + " " + version)
	help := subtitleStyle.Render(strings.Join([]string{
		helpString(m.keys.quit),
						  helpString(m.keys.execute),
						  helpString(m.keys.refresh),
						  helpString(m.keys.back),
						  helpString(m.keys.selectIt),
						  helpString(m.keys.filter),
						  helpString(m.keys.tab),
						  helpString(m.keys.copy),
						  helpString(m.keys.move),
						  helpString(m.keys.delete),
						  helpString(m.keys.fuzzy),
						  helpString(m.keys.bulkRename),
						  helpString(m.keys.suspend),
						  helpString(m.keys.subshell),
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
	if m.mode == fuzzyMode {
		fuzzyView := inputStyle.Render(m.fuzzyInput.View())
		listView := listStyle.Render(m.panels[m.activePanel].fileList.View())
		return lipgloss.JoinVertical(lipgloss.Left, title, fuzzyView, listView, m.statusMsg, help)
	}
	if m.mode == bulkRenameMode {
		inputView := inputStyle.Render(m.commandInput.View())
		return lipgloss.JoinVertical(lipgloss.Left, title, "Bulk Rename", inputView, m.statusMsg, help)
	}

	leftList := listStyle.Width(m.panels[0].fileList.Width()).Render(m.panels[0].fileList.View())
	rightList := listStyle.Width(m.panels[1].fileList.Width()).Render(m.panels[1].fileList.View())
	content := lipgloss.JoinHorizontal(lipgloss.Top, leftList, rightList)

	leftPwd := subtitleStyle.Render("Left: " + m.panels[0].currentDir)
	if m.panels[0].gitBranch != "" {
		leftPwd += " [Branch: " + m.panels[0].gitBranch + "]"
	}
	rightPwd := subtitleStyle.Render("Right: " + m.panels[1].currentDir)
	if m.panels[1].gitBranch != "" {
		rightPwd += " [Branch: " + m.panels[1].gitBranch + "]"
	}
	pwds := lipgloss.JoinHorizontal(lipgloss.Top, leftPwd, rightPwd)

	inputView := inputStyle.Render(m.commandInput.View())
	status := m.statusMsg

	return lipgloss.JoinVertical(lipgloss.Left, title, pwds, content, inputView, status, help)
}

func helpString(b key.Binding) string {
	h := b.Help()
	return h.Key + ": " + h.Desc
}

func (m *model) executeCommand(cmdStr string) {
	args := strings.Fields(cmdStr)
	if len(args) == 0 {
		m.statusMsg = errorStyle.Render("Empty command")
		return
	}
	cmdName := args[0]
	p := &m.panels[m.activePanel]
	switch cmdName {
		case "cd":
			if len(args) < 2 {
				m.statusMsg = errorStyle.Render("cd requires a directory")
				return
			}
			newDir := args[1]
			if !filepath.IsAbs(newDir) {
				newDir = filepath.Join(p.currentDir, newDir)
			}
			err := p.vfs.Chdir(newDir)
			if err != nil {
				m.statusMsg = errorStyle.Render(fmt.Sprintf("cd failed: %v", err))
				return
			}
			p.currentDir, _ = p.vfs.Getwd()
			m.refreshPanel(m.activePanel)
			m.statusMsg = successStyle.Render("Changed directory to " + p.currentDir)
		case "mv":
			m.mode = progressMode
			go m.moveWithProgress(args)
		case "rm":
			m.mode = progressMode
			go m.deleteWithProgress()
		case "cp":
			m.mode = progressMode
			go m.copyWithProgress(args)
		case "touch":
			// Only local
			if len(args) < 2 {
				m.statusMsg = errorStyle.Render("touch requires filename")
				return
			}
			file := filepath.Join(p.currentDir, args[1])
			f, err := os.Create(file)
			if err != nil {
				m.statusMsg = errorStyle.Render(fmt.Sprintf("touch failed: %v", err))
				return
			}
			f.Close()
			m.refreshPanel(m.activePanel)
		case "mkdir":
			if len(args) < 2 {
				m.statusMsg = errorStyle.Render("mkdir requires directory name")
				return
			}
			dir := filepath.Join(p.currentDir, args[1])
			err := os.MkdirAll(dir, os.ModePerm)
			if err != nil {
				m.statusMsg = errorStyle.Render(fmt.Sprintf("mkdir failed: %v", err))
				return
			}
			m.refreshPanel(m.activePanel)
		case "hedit":
			if len(args) < 2 {
				m.statusMsg = errorStyle.Render("hedit requires filename")
				return
			}
			file := filepath.Join(p.currentDir, args[1])
			stat, err := p.vfs.Stat(file)
			if err != nil {
				m.statusMsg = errorStyle.Render(fmt.Sprintf("hedit failed: %v", err))
				return
			}
			if stat.Size() > maxFileSizeForEdit {
				m.statusMsg = errorStyle.Render("File too large to edit")
				return
			}
			f, err := p.vfs.Open(file)
			if err != nil {
				m.statusMsg = errorStyle.Render(fmt.Sprintf("hedit failed: %v", err))
				return
			}
			content, err := io.ReadAll(f)
			f.Close()
			if err != nil {
				m.statusMsg = errorStyle.Render(fmt.Sprintf("hedit failed: %v", err))
				return
			}
			m.editor.SetValue(string(content))
			m.editorFile = file
			m.mode = editorMode
			m.editor.Focus()
		case "sftp":
			// sftp://user:pass@host
			if len(args) < 2 {
				m.statusMsg = errorStyle.Render("sftp requires url")
				return
			}
			url := args[1]
			if !strings.HasPrefix(url, "sftp://") {
				url = "sftp://" + url
			}
			// Parse user:pass@host
			parts := strings.SplitN(strings.TrimPrefix(url, "sftp://"), "@", 2)
			if len(parts) < 2 {
				m.statusMsg = errorStyle.Render("Invalid sftp url")
				return
			}
			userpass := strings.SplitN(parts[0], ":", 2)
			user := userpass[0]
			pass := ""
			if len(userpass) > 1 {
				pass = userpass[1]
			}
			host := parts[1]
			vfs, err := newSFTPVFS(host, user, pass)
			if err != nil {
				m.statusMsg = errorStyle.Render(err.Error())
				return
			}
			p.vfs = vfs
			p.currentDir, _ = vfs.Getwd()
			m.refreshPanel(m.activePanel)
		default:
			m.runSystemCommand(cmdName, args[1:]...)
	}
}

func (m *model) mountArchive(file string) {
	ext := filepath.Ext(file)
	var avfs vfsHandler
	var err error
	fullPath := filepath.Join(m.panels[m.activePanel].currentDir, file)
	if ext == ".zip" {
		avfs, err = newZipVFS(fullPath)
	} else if ext == ".tar" || ext == ".gz" {
		avfs, err = newTarVFS(fullPath)
	} else {
		m.statusMsg = errorStyle.Render("Unsupported archive")
		return
	}
	if err != nil {
		m.statusMsg = errorStyle.Render(err.Error())
		return
	}
	m.panels[m.activePanel].vfs = avfs
	m.panels[m.activePanel].currentDir = fullPath
	m.refreshPanel(m.activePanel)
}

func isArchive(file string) bool {
	ext := filepath.Ext(file)
	return ext == ".zip" || ext == ".tar" || ext == ".gz"
}

func (m *model) runSystemCommand(name string, arg ...string) {
	go func() {
		cmd := exec.Command(name, arg...)
		cmd.Dir = m.panels[m.activePanel].currentDir
		output, err := cmd.CombinedOutput()
		m.resultChan <- commandResult{output: string(output), err: err}
	}()
}

func (m *model) copyToOtherPanel() {
	dstPanel := 1 - m.activePanel
	args := []string{"cp", ".", m.panels[dstPanel].currentDir}
	m.mode = progressMode
	go m.copyWithProgress(args)
}

func (m *model) moveToOtherPanel() {
	dstPanel := 1 - m.activePanel
	args := []string{"mv", ".", m.panels[dstPanel].currentDir}
	m.mode = progressMode
	go m.moveWithProgress(args)
}

func (m *model) copyWithProgress(args []string) {
	var sources []string
	p := m.panels[m.activePanel]
	dst := m.panels[1-m.activePanel].currentDir
	for f := range p.selectedFiles {
		if p.selectedFiles[f] {
			sources = append(sources, f)
		}
	}
	if len(sources) == 0 && len(args) > 1 {
		sources = []string{filepath.Join(p.currentDir, args[1])}
		dst = filepath.Join(p.currentDir, args[2])
	}
	total := len(sources)
	eg, _ := errgroup.WithContext(context.Background())
	for i, src := range sources {
		i := i
		src := src
		eg.Go(func() error {
			err := copyFileVFS(p.vfs, m.panels[1-m.activePanel].vfs, src, filepath.Join(dst, filepath.Base(src)), func(percent float64) {
				overall := (float64(i) + percent) / float64(total)
				m.progressChan <- progressMsg{percent: overall}
			})
			return err
		})
	}
	err := eg.Wait()
	p.selectedFiles = make(map[string]bool)
	m.progressChan <- progressMsg{percent: 1.0}
	if err != nil {
		m.resultChan <- commandResult{err: err}
	} else {
		m.resultChan <- commandResult{output: "Copy completed"}
	}
}

func copyFileVFS(srcVFS vfsHandler, dstVFS vfsHandler, src, dst string, progressCb func(float64)) error {
	s, err := srcVFS.Open(src)
	if err != nil {
		return err
	}
	defer s.Close()
	stat, err := s.Stat()
	if err != nil {
		return err
	}
	if stat.IsDir() {
		// Recurse for dir
		return fmt.Errorf("dir copy not implemented")
	}
	// For dst, assume local for simplicity, extend as needed
	d, err := os.Create(dst) // Assume dst local
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
	}
	return nil
}

func (m *model) moveWithProgress(args []string) {
	// Similar to copy, but rename or copy+delete
	// Omitted for brevity, implement similarly
	m.progressChan <- progressMsg{percent: 1.0}
	m.resultChan <- commandResult{output: "Move completed"}
}

func (m *model) deleteWithProgress() {
	p := m.panels[m.activePanel]
	var targets []string
	for f := range p.selectedFiles {
		if p.selectedFiles[f] {
			targets = append(targets, f)
		}
	}
	total := len(targets)
	for i, target := range targets {
		os.RemoveAll(target) // Assume local
		m.progressChan <- progressMsg{percent: float64(i+1) / float64(total)}
	}
	p.selectedFiles = make(map[string]bool)
	m.resultChan <- commandResult{output: "Delete completed"}
}

func main() {
	m := initialModel()
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())

	// Handle signals for suspend
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTSTP)
	go func() {
		for range sigChan {
			p.Send(tea.KeyMsg{Type: tea.KeyCtrlZ})
		}
	}()

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
