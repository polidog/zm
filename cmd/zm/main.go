// zm は ZOOM G2 FOUR を CLI・TUI・MCP サーバーから操作する。
package main

import (
	"fmt"
	"os"

	"github.com/polidog/zm/internal/cli"
)

func main() {
	if err := cli.Run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "zm:", err)
		os.Exit(1)
	}
}
