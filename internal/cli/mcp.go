package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"
	"syscall"

	"github.com/polidog/zm/internal/mcpserver"
)

// Version は zm の版。MCP の serverInfo と zm version に出す。リリースでは GoReleaser が -X で入れる。
var Version = "0.1.0"

// VersionCmd は版を出す。go install で入れたものはモジュールの版を使う。
func VersionCmd(args []string) error {
	v := Version
	if bi, ok := debug.ReadBuildInfo(); ok && Version == "0.1.0" && strings.HasPrefix(bi.Main.Version, "v") {
		v = strings.TrimPrefix(bi.Main.Version, "v")
	}
	fmt.Println(v)
	return nil
}

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
