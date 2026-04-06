package src

import "github.com/charmbracelet/bubbles/key"

type mode int

const (
	explorerMode mode = iota
	editorMode
	progressMode
	fuzzyMode
	bulkRenameMode
	confirmMode
	podmanMode
)

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
	help       key.Binding
	sortCycle  key.Binding
	podman     key.Binding
	duplicate  key.Binding
	props      key.Binding
}

func newKeyMap() keyMap {
	return keyMap{
		quit:       key.NewBinding(key.WithKeys("ctrl+c", "q"), key.WithHelp("q/^C", "quit")),
		execute:    key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open/exec")),
		save:       key.NewBinding(key.WithKeys("ctrl+s"), key.WithHelp("^S", "save")),
		cancel:     key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
		refresh:    key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
		enter:      key.NewBinding(key.WithKeys("enter")),
		back:       key.NewBinding(key.WithKeys("backspace"), key.WithHelp("bs", "cd ..")),
		selectIt:   key.NewBinding(key.WithKeys(" "), key.WithHelp("space", "select")),
		filter:     key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
		tab:        key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "switch panel")),
		down:       key.NewBinding(key.WithKeys("j", "down")),
		up:         key.NewBinding(key.WithKeys("k", "up")),
		left:       key.NewBinding(key.WithKeys("h", "left")),
		right:      key.NewBinding(key.WithKeys("l", "right")),
		copy:       key.NewBinding(key.WithKeys("f5"), key.WithHelp("F5", "copy")),
		move:       key.NewBinding(key.WithKeys("f6"), key.WithHelp("F6", "move")),
		delete:     key.NewBinding(key.WithKeys("f8"), key.WithHelp("F8", "delete")),
		fuzzy:      key.NewBinding(key.WithKeys("ctrl+p"), key.WithHelp("^P", "fuzzy")),
		bulkRename: key.NewBinding(key.WithKeys("ctrl+r"), key.WithHelp("^R", "rename")),
		suspend:    key.NewBinding(key.WithKeys("ctrl+z"), key.WithHelp("^Z", "suspend")),
		subshell:   key.NewBinding(key.WithKeys("ctrl+o"), key.WithHelp("^O", "shell")),
		help:       key.NewBinding(key.WithKeys("f1"), key.WithHelp("F1", "help")),
		sortCycle:  key.NewBinding(key.WithKeys("ctrl+s"), key.WithHelp("^S", "sort")),
		podman:     key.NewBinding(key.WithKeys("ctrl+d"), key.WithHelp("^D", "podman")),
		duplicate:  key.NewBinding(key.WithKeys("ctrl+u"), key.WithHelp("^U", "duplicate")),
		props:      key.NewBinding(key.WithKeys("alt+enter"), key.WithHelp("Alt+Enter", "props")),
	}
}

func helpString(b key.Binding) string {
	h := b.Help()
	return h.Key + ": " + h.Desc
}
