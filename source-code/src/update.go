package src

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/progress"
)

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	var cmds []tea.Cmd

	switch msg := msg.(type) {

		// ── Async results ──────────────────────────────────────────────────────────
		case CommandResult:
			if msg.Err != nil {
				m.statusMsg = errorStyle.Render(fmt.Sprintf("✗ %v %s", msg.Err, msg.Output))
			} else {
				m.statusMsg = successStyle.Render("✓ " + msg.Output)
			}
			m.refreshPanel(m.activePanel)
			m.refreshPanel(1 - m.activePanel)
			m.mode = explorerMode

		case ProgressMsg:
			cmd = m.progress.SetPercent(msg.Percent)
			cmds = append(cmds, cmd)
			if msg.Percent >= 1.0 {
				m.mode = explorerMode
				m.refreshPanel(m.activePanel)
				m.refreshPanel(1 - m.activePanel)
			}

			// ── Keyboard ───────────────────────────────────────────────────────────────
		case tea.KeyMsg:
			// Progress mode – block all input
			if m.mode == progressMode {
				return m, nil
			}

			// Confirmation mode
			if m.mode == confirmMode {
				switch msg.String() {
					case "y", "Y":
						if m.confirmAction != nil {
							m.confirmAction()
						}
						m.mode = explorerMode
					case "n", "N", "esc":
						m.mode = explorerMode
						m.statusMsg = warnStyle.Render("Cancelled")
				}
				return m, nil
			}

			// Podman browser mode
			if m.mode == podmanMode {
				switch {
					case key.Matches(msg, m.keys.cancel):
						m.mode = explorerMode
					case key.Matches(msg, m.keys.execute):
						if len(m.podmanContainers) > 0 {
							fields := strings.Fields(m.podmanContainers[0])
							if len(fields) > 0 {
								m.connectPodman(fields[0])
							}
						}
						m.mode = explorerMode
				}
				return m, nil
			}

			// Editor mode
			if m.mode == editorMode {
				if key.Matches(msg, m.keys.save) {
					err := os.WriteFile(m.editorFile, []byte(m.editor.Value()), 0644)
					if err != nil {
						m.statusMsg = errorStyle.Render(fmt.Sprintf("Save error: %v", err))
					} else {
						m.statusMsg = successStyle.Render("Saved: " + m.editorFile)
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

			// Fuzzy mode
			if m.mode == fuzzyMode {
				if key.Matches(msg, m.keys.execute) {
					if len(m.fuzzyResults) > m.panels[m.activePanel].fileList.Index() {
						selected := m.fuzzyResults[m.panels[m.activePanel].fileList.Index()]
						m.executeCommand("cd " + selected)
					}
					m.mode = explorerMode
					return m, nil
				}
				if key.Matches(msg, m.keys.cancel) {
					m.mode = explorerMode
					m.refreshPanel(m.activePanel)
					return m, nil
				}
				m.fuzzyInput, cmd = m.fuzzyInput.Update(msg)
				cmds = append(cmds, cmd)
				m.performFuzzySearch()
				return m, tea.Batch(cmds...)
			}

			// Bulk rename mode
			if m.mode == bulkRenameMode {
				if m.bulkRenameFrom == "" {
					if key.Matches(msg, m.keys.execute) {
						m.bulkRenameFrom = m.commandInput.Value()
						m.commandInput.Reset()
						m.commandInput.Placeholder = "Replacement string (use $1 for groups)"
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
				if key.Matches(msg, m.keys.cancel) {
					m.mode = explorerMode
					m.bulkRenameFrom = ""
					return m, nil
				}
				m.commandInput, cmd = m.commandInput.Update(msg)
				cmds = append(cmds, cmd)
				return m, tea.Batch(cmds...)
			}

			// ── Explorer mode shortcuts ──────────────────────────────────────────
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
				// Update list titles to reflect active state
				m.panels[0].fileList.Title = "○ Left"
				m.panels[1].fileList.Title = "○ Right"
				if m.activePanel == 0 {
					m.panels[0].fileList.Title = "● Left"
				} else {
					m.panels[1].fileList.Title = "● Right"
				}
				return m, nil
			}
			if key.Matches(msg, m.keys.back) || key.Matches(msg, m.keys.left) {
				m.executeCommand("cd ..")
				return m, nil
			}
			if key.Matches(msg, m.keys.selectIt) {
				if !m.commandInput.Focused() {
					selected, ok := m.panels[m.activePanel].fileList.SelectedItem().(item)
					if ok {
						fullPath := filepath.Join(m.panels[m.activePanel].currentDir, selected.title)
						m.panels[m.activePanel].selectedFiles[fullPath] = !m.panels[m.activePanel].selectedFiles[fullPath]
						m.syncSelectionToList(m.activePanel)
					}
					return m, nil
				}
			}
			if key.Matches(msg, m.keys.execute) || key.Matches(msg, m.keys.right) {
				if m.commandInput.Focused() {
					cmdStr := m.commandInput.Value()
					m.commandInput.Reset()
					m.executeCommand(cmdStr)
					return m, nil
				}
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
			if key.Matches(msg, m.keys.copy) {
				m.copyToOtherPanel()
				return m, nil
			}
			if key.Matches(msg, m.keys.move) {
				m.moveToOtherPanel()
				return m, nil
			}
			if key.Matches(msg, m.keys.delete) {
				m.promptConfirmDelete()
				return m, nil
			}
			if key.Matches(msg, m.keys.fuzzy) {
				m.mode = fuzzyMode
				m.fuzzyInput.Reset()
				m.fuzzyInput.Focus()
				m.performFuzzySearch()
				return m, nil
			}
			if key.Matches(msg, m.keys.bulkRename) {
				m.mode = bulkRenameMode
				m.commandInput.Reset()
				m.commandInput.Placeholder = "Regex pattern to replace"
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
			if key.Matches(msg, m.keys.sortCycle) {
				if !m.commandInput.Focused() {
					m.cycleSortMode()
					return m, nil
				}
			}
			if key.Matches(msg, m.keys.podman) {
				m.listPodmanContainers()
				return m, nil
			}
			if key.Matches(msg, m.keys.duplicate) {
				if !m.commandInput.Focused() {
					m.duplicateSelected()
					return m, nil
				}
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
			if key.Matches(msg, m.keys.help) {
				m.showHelp()
				return m, nil
			}
			// Tab completion in command input
			if m.commandInput.Focused() && msg.String() == "tab" {
				newStr := m.completeCommand(m.commandInput.Value())
				m.commandInput.SetValue(newStr)
				return m, nil
			}

			// ── Mouse ──────────────────────────────────────────────────────────────────
			case tea.MouseMsg:
				// reserved for future mouse handling

				// ── Window resize ──────────────────────────────────────────────────────────
			case tea.WindowSizeMsg:
				m.termW = msg.Width
				m.termH = msg.Height
				m.applyLayout()
	}

	// Pass through to components
	if m.mode == explorerMode {
		m.panels[m.activePanel].fileList, cmd = m.panels[m.activePanel].fileList.Update(msg)
		cmds = append(cmds, cmd)
		if _, ok := msg.(tea.KeyMsg); ok {
			m.updatePreview(m.activePanel)
		}
		m.commandInput, cmd = m.commandInput.Update(msg)
		cmds = append(cmds, cmd)
	} else if m.mode == editorMode {
		m.editor, cmd = m.editor.Update(msg)
		cmds = append(cmds, cmd)
	} else if m.mode == progressMode {
		updated, cmd := m.progress.Update(msg)
		m.progress = updated.(progress.Model)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

func (m *Model) applyLayout() {
	w, h := m.termW, m.termH
	if w == 0 || h == 0 {
		return
	}
	topH := 3 // title + path bar
	botH := 3 // status + fbar
	contentH := h - topH - botH
	if contentH < 5 {
		contentH = 5
	}
	halfW := (w / 2) - 4

	for i := range m.panels {
		m.panels[i].fileList.SetSize(halfW, contentH-2)
		m.panels[i].preview.Width = halfW
		m.panels[i].preview.Height = contentH / 2
	}
	m.editor.SetWidth(w - 4)
	m.editor.SetHeight(contentH)
	m.commandInput.Width = w - 6
	m.progress.Width = w - 4
	m.fuzzyInput.Width = w - 4
}

func (m *Model) syncSelectionToList(idx int) {
	p := &m.panels[idx]
	items := p.fileList.Items()
	for i, it := range items {
		ii := it.(item)
		fp := filepath.Join(p.currentDir, ii.title)
		ii.selected = p.selectedFiles[fp]
		items[i] = ii
	}
	p.fileList.SetItems(items)
}

func (m *Model) showHelp() {
	help := []string{
		"ngt keybindings:",
		"  Tab       – switch panel",
		"  Enter/l   – open dir/file/archive",
		"  Backspace – cd ..",
		"  Space     – select/deselect",
		"  F5        – copy to other panel",
		"  F6        – move to other panel",
		"  F8        – delete (with confirmation)",
		"  Ctrl+P    – fuzzy search",
		"  Ctrl+R    – bulk rename (regex)",
		"  Ctrl+S    – cycle sort mode",
		"  Ctrl+D    – podman container browser",
		"  Ctrl+U    – duplicate file",
		"  Ctrl+O    – open sub-shell",
		"  Ctrl+Z    – suspend",
		"  r         – refresh panel",
		"  q/Ctrl+C  – quit",
		"Commands: cd, cp, mv, rm, mkdir, touch, hedit, sftp, podman, podmanls",
	}
	m.statusMsg = successStyle.Render(strings.Join(help, "\n"))
}

// ─── Completion ───────────────────────────────────────────────────────────────

func (m *Model) completeCommand(input string) string {
	args := strings.Fields(input)
	if len(args) == 0 {
		return input
	}
	last := args[len(args)-1]
	dir, prefix := filepath.Dir(last), filepath.Base(last)
	if dir == "." {
		dir = ""
	}
	fullDir := filepath.Join(m.panels[m.activePanel].currentDir, dir)
	files, err := m.panels[m.activePanel].vfs.ReadDir(fullDir)
	if err != nil {
		return input
	}
	var matches []string
	for _, f := range files {
		if strings.HasPrefix(f.Name(), prefix) {
			matches = append(matches, f.Name())
		}
	}
	if len(matches) == 0 {
		return input
	}
	common := commonPrefix(matches)
	if common != "" {
		newLast := filepath.Join(dir, common)
		if len(matches) == 1 {
			info, _ := files[0].Info()
			if info.IsDir() {
				newLast += "/"
			}
		}
		newInput := strings.Join(args[:len(args)-1], " ")
		if newInput != "" {
			newInput += " "
		}
		return newInput + newLast
	}
	if len(matches) > 1 {
		m.statusMsg = strings.Join(matches, "  ")
	}
	return input
}

func commonPrefix(strs []string) string {
	if len(strs) == 0 {
		return ""
	}
	prefix := strs[0]
	for _, s := range strs[1:] {
		i := 0
		for ; i < len(prefix) && i < len(s) && prefix[i] == s[i]; i++ {
		}
		prefix = prefix[:i]
		if prefix == "" {
			return ""
		}
	}
	return prefix
}

// ─── Suspend / sub-shell ──────────────────────────────────────────────────────

func (m *Model) suspend() {
	pid := os.Getpid()
	// Send SIGTSTP to suspend
	_ = sendSIGTSTP(pid)
	m.refreshPanel(m.activePanel)
}

func (m *Model) openSubShell() {
	m.subShell = true
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	c := buildCmd(shell)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	c.Dir = m.panels[m.activePanel].currentDir
	_ = c.Run()
	m.subShell = false
	m.refreshPanel(m.activePanel)
}
