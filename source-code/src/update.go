package src

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/lipgloss"
)

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	var cmds []tea.Cmd
	switch msg := msg.(type) {
		case CommandResult:
			if msg.Err != nil {
				m.statusMsg = errorStyle.Render(fmt.Sprintf("Command failed: %v\n%s", msg.Err, msg.Output))
			} else {
				m.statusMsg = successStyle.Render(msg.Output)
			}
			m.refreshPanel(m.activePanel)
			m.mode = explorerMode
		case ProgressMsg:
			cmd = m.progress.SetPercent(msg.Percent)
			cmds = append(cmds, cmd)
			if msg.Percent >= 1.0 {
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
					if len(m.fuzzyResults) > m.panels[m.activePanel].fileList.Index() {
						selected := m.fuzzyResults[m.panels[m.activePanel].fileList.Index()]
						m.executeCommand("cd " + selected)
					}
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
			if key.Matches(msg, m.keys.help) {
				m.statusMsg = "Help: Use keys to navigate, F5 copy, F6 move, etc."
				return m, nil
			}
			if m.commandInput.Focused() && msg.String() == "tab" {
				cmdStr := m.commandInput.Value()
				newStr := m.completeCommand(cmdStr)
				m.commandInput.SetValue(newStr)
				return m, nil
			}
			case tea.MouseMsg:
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
	} else if len(matches) > 1 {
		m.statusMsg = strings.Join(matches, " ")
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
