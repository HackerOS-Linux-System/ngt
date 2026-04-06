package src

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func (m Model) View() string {
	if m.quitting {
		return lipgloss.NewStyle().Foreground(lipgloss.Color(colorMuted)).Render("Goodbye from ngt.\n")
	}
	if m.subShell {
		return ""
	}

	w := m.termW
	if w < 40 {
		w = 80
	}

	// ── Title bar ──────────────────────────────────────────────────────────────
	titleBar := titleBarStyle.Width(w).Render(
		appNameStyle.Render("  ngt ") +
		versionStyle.Render(version) +
		lipgloss.NewStyle().Foreground(lipgloss.Color(colorDim)).Render("  │  HackerOS file manager"),
	)

	// ── Function bar ──────────────────────────────────────────────────────────
	fBar := renderFBar(w)

	// ── Progress mode ─────────────────────────────────────────────────────────
	if m.mode == progressMode {
		prog := lipgloss.NewStyle().Padding(1, 2).Width(w).Render(m.progress.View())
		status := statusBarStyle.Width(w).Render(m.statusMsg)
		return lipgloss.JoinVertical(lipgloss.Left, titleBar, prog, status, fBar)
	}

	// ── Editor mode ───────────────────────────────────────────────────────────
	if m.mode == editorMode {
		editorBar := titleBarStyle.Width(w).Render(
			"  ✎ Editing: " + lipgloss.NewStyle().Foreground(lipgloss.Color(colorAccent)).Render(m.editorFile) +
			lipgloss.NewStyle().Foreground(lipgloss.Color(colorMuted)).Render("   Ctrl+S save  •  Esc cancel"),
		)
		editorView := editorStyle.Width(w - 2).Render(m.editor.View())
		status := statusBarStyle.Width(w).Render(m.statusMsg)
		return lipgloss.JoinVertical(lipgloss.Left, titleBar, editorBar, editorView, status, fBar)
	}

	// ── Fuzzy mode ────────────────────────────────────────────────────────────
	if m.mode == fuzzyMode {
		searchBar := inputStyle.Width(w - 2).Render("  🔍 " + m.fuzzyInput.View())
		listView := inactivePanelBorder.Width(w - 2).Render(m.panels[m.activePanel].fileList.View())
		status := statusBarStyle.Width(w).Render(
			m.statusMsg + lipgloss.NewStyle().Foreground(lipgloss.Color(colorMuted)).Render(
				fmt.Sprintf("  %d results  •  Enter: navigate  •  Esc: cancel", len(m.fuzzyResults)),
			),
		)
		return lipgloss.JoinVertical(lipgloss.Left, titleBar, searchBar, listView, status, fBar)
	}

	// ── Bulk rename mode ──────────────────────────────────────────────────────
	if m.mode == bulkRenameMode {
		step := "Step 1/2: regex pattern"
		if m.bulkRenameFrom != "" {
			step = "Step 2/2: replacement string"
		}
		header := titleBarStyle.Width(w).Render("  ✎ Bulk Rename   " +
		lipgloss.NewStyle().Foreground(lipgloss.Color(colorMuted)).Render(step))
		inputView := inputStyle.Width(w - 2).Render(m.commandInput.View())
		status := statusBarStyle.Width(w).Render(m.statusMsg)
		return lipgloss.JoinVertical(lipgloss.Left, titleBar, header, inputView, status, fBar)
	}

	// ── Confirmation mode ─────────────────────────────────────────────────────
	if m.mode == confirmMode {
		dialog := dialogStyle.Width(50).Render(
			errorStyle.Render("⚠  Confirm Action") + "\n\n" +
			m.confirmMsg + "\n\n" +
			lipgloss.NewStyle().Foreground(lipgloss.Color(colorMuted)).Render("Press y to confirm, n to cancel"),
		)
		centered := lipgloss.Place(w, 10, lipgloss.Center, lipgloss.Center, dialog)
		return lipgloss.JoinVertical(lipgloss.Left, titleBar, centered, fBar)
	}

	// ── Podman browser mode ───────────────────────────────────────────────────
	if m.mode == podmanMode {
		header := podmanStyle.Render(" 🐳 Podman Containers ") +
		lipgloss.NewStyle().Foreground(lipgloss.Color(colorMuted)).Render("  Enter: connect  •  Esc: cancel")
		var rows []string
		for i, c := range m.podmanContainers {
			prefix := "  "
			if i == 0 {
				prefix = lipgloss.NewStyle().Foreground(lipgloss.Color(colorAccent)).Render("▶ ")
			}
			rows = append(rows, prefix+c)
		}
		if len(rows) == 0 {
			rows = []string{lipgloss.NewStyle().Foreground(lipgloss.Color(colorMuted)).Render("  No running containers found")}
		}
		box := inactivePanelBorder.Width(w - 2).Render(strings.Join(rows, "\n"))
		return lipgloss.JoinVertical(lipgloss.Left, titleBar, header, box, fBar)
	}

	// ── Explorer mode (main) ──────────────────────────────────────────────────
	halfW := (w / 2) - 3

	// Path bars
	leftPath := m.renderPathBar(0, halfW)
	rightPath := m.renderPathBar(1, halfW)
	pathRow := lipgloss.JoinHorizontal(lipgloss.Top, leftPath, rightPath)

	// File panels
	leftPanel := m.renderPanel(0, halfW)
	rightPanel := m.renderPanel(1, halfW)
	panels := lipgloss.JoinHorizontal(lipgloss.Top, leftPanel, rightPanel)

	// Command input
	cmdLabel := inputLabelStyle.Render("›")
	inputView := inputStyle.Width(w - 6).Render(m.commandInput.View())
	cmdRow := lipgloss.JoinHorizontal(lipgloss.Left, cmdLabel, inputView)

	// Status
	status := statusBarStyle.Width(w).Render(m.statusMsg)

	return lipgloss.JoinVertical(lipgloss.Left,
				     titleBar,
				     pathRow,
				     panels,
				     cmdRow,
				     status,
				     fBar,
	)
}

func (m *Model) renderPathBar(idx, w int) string {
	p := &m.panels[idx]
	vfsTag := vfsTagStyle.Render(p.vfs.VFSName())
	dir := pathStyle.Render(truncatePath(p.currentDir, w-20))
	branch := ""
	if p.gitBranch != "" {
		branch = " " + branchStyle.Render(" "+p.gitBranch+" ")
	}
	sort := sortTagStyle.Render(p.sortMode.String())
	return lipgloss.NewStyle().Width(w + 2).Render(
		lipgloss.JoinHorizontal(lipgloss.Top, vfsTag, " ", dir, branch, " ", sort),
	)
}

func (m *Model) renderPanel(idx, w int) string {
	p := &m.panels[idx]
	listView := p.fileList.View()
	sel := 0
	for _, v := range p.selectedFiles {
		if v {
			sel++
		}
	}
	footer := ""
	if sel > 0 {
		footer = "\n" + warnStyle.Render(fmt.Sprintf("  %d selected", sel))
	}
	content := listView + footer
	if idx == m.activePanel {
		return activePanelBorder.Width(w).Render(content)
	}
	return inactivePanelBorder.Width(w).Render(content)
}

func truncatePath(path string, maxLen int) string {
	if len(path) <= maxLen {
		return path
	}
	return "…" + path[len(path)-maxLen+1:]
}
