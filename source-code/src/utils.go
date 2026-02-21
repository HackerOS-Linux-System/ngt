package src

import (
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"

	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/charmbracelet/bubbles/list"
)

func (m *Model) refreshPanel(idx int) {
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

func (m *Model) getGitStatus(p *panel) map[string]string {
	statusMap := make(map[string]string)
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

func (m *Model) updateGitBranch(idx int) {
	p := &m.panels[idx]
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

func (m *Model) updatePreview(idx int) {
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
	buf := make([]byte, 512)
	_, err = f.Read(buf)
	if err != nil && err != io.EOF {
		p.preview.SetContent("Error reading for MIME")
		return
	}
	mimeType := http.DetectContentType(buf)
	if mimeType == "application/octet-stream" {
		if _, ok := p.vfs.(localVFS); ok {
			cmd := exec.Command("file", "-b", "--mime-type", filePath)
			out, err := cmd.Output()
			if err == nil {
				mimeType = strings.TrimSpace(string(out))
			}
		}
	}
	if s, ok := f.(io.Seeker); ok {
		s.Seek(0, io.SeekStart)
	} else {
		f.Close()
		f, err = p.vfs.Open(filePath)
		if err != nil {
			p.preview.SetContent("Error reopening file")
			return
		}
		defer f.Close()
	}
	if strings.HasPrefix(mimeType, "image/") {
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

func (m *Model) performFuzzySearch() {
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
	items := []list.Item{}
	for _, res := range results {
		items = append(items, item{title: res})
	}
	m.panels[m.activePanel].fileList.SetItems(items)
}

func (m *Model) performBulkRename() {
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

func (m *Model) suspend() {
	pid := os.Getpid()
	syscall.Kill(pid, syscall.SIGTSTP)
	m.refreshPanel(m.activePanel)
}

func (m *Model) openSubShell() {
	m.subShell = true
	cmd := exec.Command(os.Getenv("SHELL"))
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Dir = m.panels[m.activePanel].currentDir
	cmd.Run()
	m.subShell = false
	m.refreshPanel(m.activePanel)
}
