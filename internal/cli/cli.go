// Package cli はサブコマンドの実装。
package cli

import (
	"fmt"
	"os"
)

const usage = `usage: zoom <command> [args]

  probe                           P0 の疎通確認（開発用）
`

type command func(args []string) error

func commands() map[string]command {
	return map[string]command{
		"probe": Probe,
	}
}

func Run(args []string) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		fmt.Fprint(os.Stderr, usage)
		return nil
	}
	cmd, ok := commands()[args[0]]
	if !ok {
		fmt.Fprint(os.Stderr, usage)
		return fmt.Errorf("知らないコマンド: %s", args[0])
	}
	return cmd(args[1:])
}
