package src

import (
	"github.com/charmbracelet/bubbles/key"
)

type mode int

const (
	explorerMode mode = iota
	editorMode
	progressMode
	fuzzyMode
	bulkRenameMode
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
		help:       key.NewBinding(key.WithKeys("f1"), key.WithHelp("F1", "help")),
	}
}

func helpString(b key.Binding) string {
	h := b.Help()
	return h.Key + ": " + h.Desc
}
