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

	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/charmbracelet/bubbles/list"
)

// ─── Panel refresh ────────────────────────────────────────────────────────────

func (m *Model) refreshPanel(idx int) {
	p := &m.panels[idx]
	files, err := p.vfs.ReadDir(p.currentDir)
	if err != nil {
		m.statusMsg = errorStyle.Render(fmt.Sprintf("ReadDir: %v", err))
		return
	}
	gitStatus := m.getGitStatus(p)

	// Sort
	sortFiles(files, p.sortMode)

	var items []list.Item
	for _, file := range files {
		info, _ := file.Info()
		if info == nil {
			continue
		}
		desc := "file"
		if file.IsDir() {
			desc = "dir"
		}
		fp := filepath.Join(p.currentDir, file.Name())
		items = append(items, item{
			title:    file.Name(),
			       desc:     desc,
			       status:   gitStatus[file.Name()],
			       isDir:    file.IsDir(),
			       size:     info.Size(),
			       modTime:  info.ModTime(),
			       selected: p.selectedFiles[fp],
		})
	}
	p.fileList.SetItems(items)
	m.updatePreview(idx)
	m.updateGitBranch(idx)
}

func sortFiles(files []fs.DirEntry, mode SortMode) {
	sort.Slice(files, func(i, j int) bool {
		a, b := files[i], files[j]
		// Dirs first
		if a.IsDir() != b.IsDir() {
			return a.IsDir()
		}
		switch mode {
			case SortBySize:
				ai, _ := a.Info()
				bi, _ := b.Info()
				if ai != nil && bi != nil {
					return ai.Size() < bi.Size()
				}
			case SortByDate:
				ai, _ := a.Info()
				bi, _ := b.Info()
				if ai != nil && bi != nil {
					return ai.ModTime().After(bi.ModTime())
				}
			case SortByExt:
				return filepath.Ext(a.Name()) < filepath.Ext(b.Name())
		}
		return strings.ToLower(a.Name()) < strings.ToLower(b.Name())
	})
}

// ─── Git ──────────────────────────────────────────────────────────────────────

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
	for _, line := range strings.Split(string(output), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		statusMap[fields[1]] = line[:2]
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

// ─── Preview ──────────────────────────────────────────────────────────────────

func (m *Model) updatePreview(idx int) {
	p := &m.panels[idx]
	selected, ok := p.fileList.SelectedItem().(item)
	if !ok {
		p.preview.SetContent("")
		return
	}
	if selected.isDir {
		// Show directory listing summary
		entries, err := p.vfs.ReadDir(filepath.Join(p.currentDir, selected.title))
		if err != nil {
			p.preview.SetContent("Directory (unreadable)")
			return
		}
		dirs, files := 0, 0
		for _, e := range entries {
			if e.IsDir() {
				dirs++
			} else {
				files++
			}
		}
		p.preview.SetContent(fmt.Sprintf("📁 Directory\n\n%d subdirectories\n%d files", dirs, files))
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

	// MIME detection
	buf := make([]byte, 512)
	_, err = f.Read(buf)
	if err != nil && err != io.EOF {
		p.preview.SetContent("Error reading file")
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

	// Rewind
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
		p.preview.SetContent(fmt.Sprintf("🖼  Image file\nMIME: %s\nSize: %s", mimeType, humanSize(stat.Size())))
		return
	}
	if !strings.HasPrefix(mimeType, "text/") {
		p.preview.SetContent(fmt.Sprintf("Binary file\nMIME: %s\nSize: %s", mimeType, humanSize(stat.Size())))
		return
	}
	if stat.Size() > maxFileSizeForEdit {
		p.preview.SetContent(fmt.Sprintf("File too large for preview\nSize: %s", humanSize(stat.Size())))
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
		content = strings.Join(lines, "\n") + "\n\n… (truncated)"
	}

	// Syntax highlight
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
	if err = chromaFormatter.Format(&sb, chromaStyle, iterator); err != nil {
		p.preview.SetContent(content)
		return
	}
	p.preview.SetContent(sb.String())
}

// ─── Fuzzy search ─────────────────────────────────────────────────────────────

func (m *Model) performFuzzySearch() {
	query := m.fuzzyInput.Value()
	m.fuzzyResults = nil
	if query == "" {
		m.panels[m.activePanel].fileList.SetItems(nil)
		return
	}
	var results []string
	_ = filepath.Walk(m.panels[m.activePanel].currentDir, func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if strings.Contains(strings.ToLower(filepath.Base(path)), strings.ToLower(query)) {
			rel, _ := filepath.Rel(m.panels[m.activePanel].currentDir, path)
			results = append(results, rel)
		}
		return nil
	})
	m.fuzzyResults = results
	var items []list.Item
	for _, res := range results {
		items = append(items, item{title: res})
	}
	m.panels[m.activePanel].fileList.SetItems(items)
}

// ─── Bulk rename ──────────────────────────────────────────────────────────────

func (m *Model) performBulkRename() {
	re, err := regexp.Compile(m.bulkRenameFrom)
	if err != nil {
		m.statusMsg = errorStyle.Render("Invalid regex: " + err.Error())
		return
	}
	renamed := 0
	for file := range m.panels[m.activePanel].selectedFiles {
		newName := re.ReplaceAllString(filepath.Base(file), m.bulkRenameTo)
		newPath := filepath.Join(filepath.Dir(file), newName)
		if err := os.Rename(file, newPath); err != nil {
			m.statusMsg += errorStyle.Render(fmt.Sprintf(" %s: %v", filepath.Base(file), err))
		} else {
			renamed++
		}
	}
	m.panels[m.activePanel].selectedFiles = make(map[string]bool)
	m.refreshPanel(m.activePanel)
	m.bulkRenameFrom = ""
	m.bulkRenameTo = ""
	m.statusMsg = successStyle.Render(fmt.Sprintf("Renamed %d file(s)", renamed))
}
