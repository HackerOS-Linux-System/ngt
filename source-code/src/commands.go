package src

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"golang.org/x/sync/errgroup"
)

// ─── Command Dispatcher ───────────────────────────────────────────────────────

func (m *Model) executeCommand(cmdStr string) {
	args := strings.Fields(cmdStr)
	if len(args) == 0 {
		m.statusMsg = errorStyle.Render("Empty command")
		return
	}
	p := &m.panels[m.activePanel]
	switch args[0] {
		case "cd":
			if len(args) < 2 {
				m.statusMsg = errorStyle.Render("cd requires a directory")
				return
			}
			newDir := args[1]
			if !filepath.IsAbs(newDir) {
				newDir = filepath.Join(p.currentDir, newDir)
			}
			if err := p.vfs.Chdir(newDir); err != nil {
				m.statusMsg = errorStyle.Render(fmt.Sprintf("cd: %v", err))
				return
			}
			p.currentDir, _ = p.vfs.Getwd()
			m.refreshPanel(m.activePanel)

		case "mv":
			m.mode = progressMode
			go m.moveWithProgress(args)

		case "rm":
			m.promptConfirmDelete()

		case "cp":
			m.mode = progressMode
			go m.copyWithProgress(args)

		case "touch":
			if len(args) < 2 {
				m.statusMsg = errorStyle.Render("touch requires filename")
				return
			}
			dst := filepath.Join(p.currentDir, args[1])
			w, err := p.vfs.Create(dst)
			if err != nil {
				m.statusMsg = errorStyle.Render(fmt.Sprintf("touch: %v", err))
				return
			}
			w.Close()
			m.refreshPanel(m.activePanel)

		case "mkdir":
			if len(args) < 2 {
				m.statusMsg = errorStyle.Render("mkdir requires name")
				return
			}
			dir := filepath.Join(p.currentDir, args[1])
			if err := p.vfs.MkdirAll(dir, 0755); err != nil {
				m.statusMsg = errorStyle.Render(fmt.Sprintf("mkdir: %v", err))
				return
			}
			m.refreshPanel(m.activePanel)

		case "hedit":
			if len(args) < 2 {
				m.statusMsg = errorStyle.Render("hedit requires filename")
				return
			}
			m.openEditor(filepath.Join(p.currentDir, args[1]))

		case "sftp":
			if len(args) < 2 {
				m.statusMsg = errorStyle.Render("sftp requires url sftp://user:pass@host")
				return
			}
			m.connectSFTP(args[1])

		case "podman":
			if len(args) < 2 {
				m.statusMsg = errorStyle.Render("podman requires container id/name")
				return
			}
			m.connectPodman(args[1])

		case "podmanls":
			m.listPodmanContainers()

		default:
			m.runSystemCommand(args[0], args[1:]...)
	}
}

// ─── Podman ───────────────────────────────────────────────────────────────────

func (m *Model) connectPodman(containerID string) {
	vfs, err := newPodmanVFS(containerID)
	if err != nil {
		m.statusMsg = errorStyle.Render(fmt.Sprintf("podman: %v", err))
		return
	}
	p := &m.panels[m.activePanel]
	p.vfs = vfs
	p.currentDir, _ = vfs.Getwd()
	m.refreshPanel(m.activePanel)
	m.statusMsg = successStyle.Render("Connected to container " + vfs.containerName)
}

func (m *Model) listPodmanContainers() {
	containers, err := ListPodmanContainers()
	if err != nil {
		m.statusMsg = errorStyle.Render("podman ps failed: " + err.Error())
		return
	}
	m.podmanContainers = containers
	m.mode = podmanMode
}

// ─── SFTP ─────────────────────────────────────────────────────────────────────

func (m *Model) connectSFTP(url string) {
	if !strings.HasPrefix(url, "sftp://") {
		url = "sftp://" + url
	}
	parts := strings.SplitN(strings.TrimPrefix(url, "sftp://"), "@", 2)
	if len(parts) < 2 {
		m.statusMsg = errorStyle.Render("invalid sftp url, use sftp://user:pass@host")
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
	p := &m.panels[m.activePanel]
	p.vfs = vfs
	p.currentDir, _ = vfs.Getwd()
	m.refreshPanel(m.activePanel)
}

// ─── Editor ───────────────────────────────────────────────────────────────────

func (m *Model) openEditor(file string) {
	p := &m.panels[m.activePanel]
	stat, err := p.vfs.Stat(file)
	if err != nil {
		m.statusMsg = errorStyle.Render(fmt.Sprintf("hedit: %v", err))
		return
	}
	if stat.Size() > maxFileSizeForEdit {
		m.statusMsg = errorStyle.Render("File too large to edit (>10MB)")
		return
	}
	f, err := p.vfs.Open(file)
	if err != nil {
		m.statusMsg = errorStyle.Render(fmt.Sprintf("hedit: %v", err))
		return
	}
	content, err := io.ReadAll(f)
	f.Close()
	if err != nil {
		m.statusMsg = errorStyle.Render(fmt.Sprintf("hedit: %v", err))
		return
	}
	m.editor.SetValue(string(content))
	m.editorFile = file
	m.mode = editorMode
	m.editor.Focus()
}

// ─── Archive mount ────────────────────────────────────────────────────────────

func (m *Model) mountArchive(file string) {
	fullPath := filepath.Join(m.panels[m.activePanel].currentDir, file)
	ext := filepath.Ext(file)
	var avfs vfsHandler
	var err error
	if ext == ".zip" {
		avfs, err = newZipVFS(fullPath)
	} else if ext == ".tar" || strings.HasSuffix(file, ".tar.gz") {
		avfs, err = newTarVFS(fullPath)
	} else {
		m.statusMsg = errorStyle.Render("Unsupported archive format")
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
	return ext == ".zip" || ext == ".tar" || strings.HasSuffix(file, ".tar.gz")
}

// ─── System command ───────────────────────────────────────────────────────────

func (m *Model) runSystemCommand(name string, arg ...string) {
	go func() {
		cmd := buildCmd(name, arg...)
		cmd.Dir = m.panels[m.activePanel].currentDir
		output, err := cmd.CombinedOutput()
		m.ResultChan <- CommandResult{Output: string(output), Err: err}
	}()
}

// ─── Confirmation dialog ──────────────────────────────────────────────────────

func (m *Model) promptConfirmDelete() {
	p := &m.panels[m.activePanel]
	count := 0
	for _, v := range p.selectedFiles {
		if v {
			count++
		}
	}
	if count == 0 {
		// delete current item
		sel, ok := p.fileList.SelectedItem().(item)
		if !ok {
			m.statusMsg = errorStyle.Render("No file selected")
			return
		}
		m.confirmMsg = fmt.Sprintf("Delete '%s'? (y/n)", sel.title)
	} else {
		m.confirmMsg = fmt.Sprintf("Delete %d selected files? (y/n)", count)
	}
	m.confirmAction = func() {
		m.mode = progressMode
		go m.deleteWithProgress()
	}
	m.mode = confirmMode
}

// ─── Cross-VFS copy ───────────────────────────────────────────────────────────

func (m *Model) copyToOtherPanel() {
	dst := m.panels[1-m.activePanel].currentDir
	args := []string{"cp", ".", dst}
	m.mode = progressMode
	go m.copyWithProgress(args)
}

func (m *Model) moveToOtherPanel() {
	dst := m.panels[1-m.activePanel].currentDir
	args := []string{"mv", ".", dst}
	m.mode = progressMode
	go m.moveWithProgress(args)
}

func (m *Model) copyWithProgress(args []string) {
	p := &m.panels[m.activePanel]
	dst := m.panels[1-m.activePanel].currentDir
	dstVFS := m.panels[1-m.activePanel].vfs

	var sources []string
	for f, sel := range p.selectedFiles {
		if sel {
			sources = append(sources, f)
		}
	}
	if len(sources) == 0 && len(args) > 2 {
		sources = []string{filepath.Join(p.currentDir, args[1])}
		dst = args[2]
	}
	if len(sources) == 0 {
		if sel, ok := p.fileList.SelectedItem().(item); ok {
			sources = []string{filepath.Join(p.currentDir, sel.title)}
		}
	}

	total := len(sources)
	eg := errgroup.Group{}
	for i, src := range sources {
		i, src := i, src
		eg.Go(func() error {
			return copyFileVFS(p.vfs, dstVFS, src, filepath.Join(dst, filepath.Base(src)), func(pct float64) {
				overall := (float64(i) + pct) / float64(total)
				m.ProgressChan <- ProgressMsg{Percent: overall}
			})
		})
	}
	err := eg.Wait()
	p.selectedFiles = make(map[string]bool)
	m.ProgressChan <- ProgressMsg{Percent: 1.0}
	if err != nil {
		m.ResultChan <- CommandResult{Err: err}
	} else {
		m.ResultChan <- CommandResult{Output: fmt.Sprintf("Copied %d file(s)", total)}
	}
}

// copyFileVFS copies a single file between any two VFS implementations
func copyFileVFS(srcVFS, dstVFS vfsHandler, src, dst string, progressCb func(float64)) error {
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
		return fmt.Errorf("directory copy not implemented")
	}
	d, err := dstVFS.Create(dst)
	if err != nil {
		return err
	}
	defer d.Close()
	totalSize := stat.Size()
	if totalSize == 0 {
		totalSize = 1
	}
	var written int64
	buf := make([]byte, 32*1024)
	for {
		n, rerr := s.Read(buf)
		if n > 0 {
			if _, werr := d.Write(buf[:n]); werr != nil {
				return werr
			}
			written += int64(n)
			progressCb(float64(written) / float64(totalSize))
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return rerr
		}
	}
	return nil
}

func (m *Model) moveWithProgress(args []string) {
	p := &m.panels[m.activePanel]
	dst := m.panels[1-m.activePanel].currentDir

	var sources []string
	for f, sel := range p.selectedFiles {
		if sel {
			sources = append(sources, f)
		}
	}
	if len(sources) == 0 && len(args) > 2 {
		sources = []string{filepath.Join(p.currentDir, args[1])}
		dst = args[2]
	}
	if len(sources) == 0 {
		if sel, ok := p.fileList.SelectedItem().(item); ok {
			sources = []string{filepath.Join(p.currentDir, sel.title)}
		}
	}

	total := len(sources)
	for i, src := range sources {
		newPath := filepath.Join(dst, filepath.Base(src))
		if err := p.vfs.Rename(src, newPath); err != nil {
			m.ResultChan <- CommandResult{Err: fmt.Errorf("move %s: %w", filepath.Base(src), err)}
			return
		}
		m.ProgressChan <- ProgressMsg{Percent: float64(i+1) / float64(total)}
	}
	p.selectedFiles = make(map[string]bool)
	m.ProgressChan <- ProgressMsg{Percent: 1.0}
	m.ResultChan <- CommandResult{Output: fmt.Sprintf("Moved %d file(s)", total)}
}

func (m *Model) deleteWithProgress() {
	p := &m.panels[m.activePanel]
	var targets []string
	for f, sel := range p.selectedFiles {
		if sel {
			targets = append(targets, f)
		}
	}
	if len(targets) == 0 {
		if sel, ok := p.fileList.SelectedItem().(item); ok {
			targets = []string{filepath.Join(p.currentDir, sel.title)}
		}
	}
	total := len(targets)
	for i, target := range targets {
		p.vfs.Remove(target)
		m.ProgressChan <- ProgressMsg{Percent: float64(i+1) / float64(total)}
	}
	p.selectedFiles = make(map[string]bool)
	m.ResultChan <- CommandResult{Output: fmt.Sprintf("Deleted %d file(s)", total)}
}

// ─── Duplicate ────────────────────────────────────────────────────────────────

func (m *Model) duplicateSelected() {
	p := &m.panels[m.activePanel]
	sel, ok := p.fileList.SelectedItem().(item)
	if !ok {
		m.statusMsg = errorStyle.Render("No file selected")
		return
	}
	src := filepath.Join(p.currentDir, sel.title)
	ext := filepath.Ext(sel.title)
	base := strings.TrimSuffix(sel.title, ext)
	dst := filepath.Join(p.currentDir, base+"_copy"+ext)
	m.mode = progressMode
	go func() {
		err := copyFileVFS(p.vfs, p.vfs, src, dst, func(pct float64) {
			m.ProgressChan <- ProgressMsg{Percent: pct}
		})
		if err != nil {
			m.ResultChan <- CommandResult{Err: err}
		} else {
			m.ResultChan <- CommandResult{Output: "Duplicated: " + filepath.Base(dst)}
		}
	}()
}

// ─── Sort ─────────────────────────────────────────────────────────────────────

func (m *Model) cycleSortMode() {
	p := &m.panels[m.activePanel]
	p.sortMode = (p.sortMode + 1) % 4
	m.refreshPanel(m.activePanel)
	m.statusMsg = warnStyle.Render(fmt.Sprintf("Sort: %s", p.sortMode))
}
