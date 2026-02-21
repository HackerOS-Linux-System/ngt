package src

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/sync/errgroup"
)

func (m *Model) executeCommand(cmdStr string) {
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
			if len(args) < 2 {
				m.statusMsg = errorStyle.Render("sftp requires url")
				return
			}
			url := args[1]
			if !strings.HasPrefix(url, "sftp://") {
				url = "sftp://" + url
			}
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

func (m *Model) mountArchive(file string) {
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
	return ext == ".zip" || ext == ".tar" || strings.HasSuffix(file, ".tar.gz")
}

func (m *Model) runSystemCommand(name string, arg ...string) {
	go func() {
		cmd := exec.Command(name, arg...)
		cmd.Dir = m.panels[m.activePanel].currentDir
		output, err := cmd.CombinedOutput()
		m.ResultChan <- CommandResult{Output: string(output), Err: err}
	}()
}

func (m *Model) copyToOtherPanel() {
	dstPanel := 1 - m.activePanel
	args := []string{"cp", ".", m.panels[dstPanel].currentDir}
	m.mode = progressMode
	go m.copyWithProgress(args)
}

func (m *Model) moveToOtherPanel() {
	dstPanel := 1 - m.activePanel
	args := []string{"mv", ".", m.panels[dstPanel].currentDir}
	m.mode = progressMode
	go m.moveWithProgress(args)
}

func (m *Model) copyWithProgress(args []string) {
	var sources []string
	p := m.panels[m.activePanel]
	dst := m.panels[1-m.activePanel].currentDir
	for f := range p.selectedFiles {
		if p.selectedFiles[f] {
			sources = append(sources, f)
		}
	}
	if len(sources) == 0 && len(args) > 2 {
		sources = []string{filepath.Join(p.currentDir, args[1])}
		dst = args[2]
	}
	total := len(sources)
	eg := errgroup.Group{}
	for i, src := range sources {
		i, src := i, src
		eg.Go(func() error {
			err := copyFileVFS(p.vfs, m.panels[1-m.activePanel].vfs, src, filepath.Join(dst, filepath.Base(src)), func(percent float64) {
				overall := (float64(i) + percent) / float64(total)
				m.ProgressChan <- ProgressMsg{Percent: overall}
			})
			return err
		})
	}
	err := eg.Wait()
	p.selectedFiles = make(map[string]bool)
	m.ProgressChan <- ProgressMsg{Percent: 1.0}
	if err != nil {
		m.ResultChan <- CommandResult{Err: err}
	} else {
		m.ResultChan <- CommandResult{Output: "Copy completed"}
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
		return fmt.Errorf("dir copy not implemented")
	}
	d, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer d.Close()
	totalSize := stat.Size()
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
		progressCb(float64(written) / float64(totalSize))
	}
	return nil
}

func (m *Model) moveWithProgress(args []string) {
	var sources []string
	p := m.panels[m.activePanel]
	dst := m.panels[1-m.activePanel].currentDir
	for f := range p.selectedFiles {
		if p.selectedFiles[f] {
			sources = append(sources, f)
		}
	}
	if len(sources) == 0 && len(args) > 2 {
		sources = []string{filepath.Join(p.currentDir, args[1])}
		dst = args[2]
	}
	total := len(sources)
	for i, src := range sources {
		newPath := filepath.Join(dst, filepath.Base(src))
		err := os.Rename(src, newPath)
		if err != nil {
			m.ResultChan <- CommandResult{Err: err}
			return
		}
		m.ProgressChan <- ProgressMsg{Percent: float64(i+1) / float64(total)}
	}
	p.selectedFiles = make(map[string]bool)
	m.ProgressChan <- ProgressMsg{Percent: 1.0}
	m.ResultChan <- CommandResult{Output: "Move completed"}
}

func (m *Model) deleteWithProgress() {
	p := m.panels[m.activePanel]
	var targets []string
	for f := range p.selectedFiles {
		if p.selectedFiles[f] {
			targets = append(targets, f)
		}
	}
	total := len(targets)
	for i, target := range targets {
		os.RemoveAll(target)
		m.ProgressChan <- ProgressMsg{Percent: float64(i+1) / float64(total)}
	}
	p.selectedFiles = make(map[string]bool)
	m.ResultChan <- CommandResult{Output: "Delete completed"}
}
