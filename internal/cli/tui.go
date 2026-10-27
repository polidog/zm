package cli

import "github.com/polidog/zm/internal/tui"

func TUI(args []string) error {
	fs := newFlags("tui", "tui")
	if err := want(parse(fs, args), 0, 0, "tui"); err != nil {
		return err
	}
	return tui.Run()
}
