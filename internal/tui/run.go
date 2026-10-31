package tui

import (
	"context"
	"errors"

	tea "github.com/charmbracelet/bubbletea"
)

// Run shows the results browser until the user quits. Cancelling ctx closes
// it too. The terminal is restored on every exit path.
func Run(ctx context.Context, in Input, act Actions) error {
	p := tea.NewProgram(New(in, act),
		tea.WithAltScreen(),
		tea.WithMouseAllMotion(), // hover highlighting needs motion events
		tea.WithContext(ctx),
	)
	_, err := p.Run()
	if errors.Is(err, tea.ErrProgramKilled) && ctx.Err() != nil {
		return nil
	}
	return err
}
