package src

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func (m Model) View() string {
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
						  helpString(m.keys.help),
	}, " • "))
	fBar := fBarStyle.Render(fBarContent)
	if m.mode == progressMode {
		return lipgloss.JoinVertical(lipgloss.Left, title, m.progress.View(), m.statusMsg, fBar, help)
	}
	if m.mode == editorMode {
		help = subtitleStyle.Render(helpString(m.keys.save) + " • " + helpString(m.keys.cancel))
		editorView := editorStyle.Render(m.editor.View())
		status := m.statusMsg
		return lipgloss.JoinVertical(lipgloss.Left, title, subtitleStyle.Render("Editing: "+m.editorFile), editorView, status, fBar, help)
	}
	if m.mode == fuzzyMode {
		fuzzyView := inputStyle.Render(m.fuzzyInput.View())
		listView := listStyle.Render(m.panels[m.activePanel].fileList.View())
		return lipgloss.JoinVertical(lipgloss.Left, title, fuzzyView, listView, m.statusMsg, fBar, help)
	}
	if m.mode == bulkRenameMode {
		inputView := inputStyle.Render(m.commandInput.View())
		return lipgloss.JoinVertical(lipgloss.Left, title, "Bulk Rename", inputView, m.statusMsg, fBar, help)
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
	return lipgloss.JoinVertical(lipgloss.Left, title, pwds, content, inputView, status, fBar, help)
}
