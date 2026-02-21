package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
	"ngt/src"
)

func main() {
	m := src.InitialModel()
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTSTP)
	go func() {
		for range sigChan {
			p.Send(tea.KeyMsg{Type: tea.KeyCtrlZ})
		}
	}()
	go func() {
		for {
			select {
				case pm := <-m.ProgressChan:
					p.Send(pm)
				case cr := <-m.ResultChan:
					p.Send(cr)
			}
		}
	}()
	if _, err := p.Run(); err != nil {
		fmt.Println("Error running program:", err)
		os.Exit(1)
	}
}
