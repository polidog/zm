package cli

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/polidog/zm/internal/mcpserver"
)

// Version は zm の版。MCP の serverInfo に出す。
var Version = "0.1.0"

// MCP は stdio で MCP サーバーを動かす。ペダルのポートは呼び出しごとに開いて閉じる。
func MCP(args []string) error {
	fs := newFlags("mcp", "mcp")
	if err := want(parse(fs, args), 0, 0, "mcp"); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return mcpserver.Run(ctx, Version)
}
