// Package cli はサブコマンドの実装。
package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/polidog/zm/internal/device"
	"github.com/polidog/zm/internal/paths"
)

const usage = `usage: zm [--pedal hw:C,D|g2four|ms80irplus|ms50g|ms70cdr] [--dir <path>] <command> [args]

  info                            機種・ファームウェア・パッチ数
  list                            全パッチの番号と名前
  show [n]                        パッチの中身（省略で、いま鳴っているパッチ）
  select <n>                      パッチ n に切り替える（未保存の編集は捨てる）
  effects [group]                 エフェクト一覧
  effect <name>                   エフェクトの説明とパラメータ
  set-effect <slot> <effect>      エフェクト差し替え（保存しない）
  set <slot> <param> <value>      パラメータ変更（保存しない）
  on|off|toggle <slot>            ON / OFF（保存しない）
  rename <name>                   パッチ名の変更（保存しない。いまは旧 MS だけ）
  tempo <bpm>                     テンポの変更（保存しない。40〜250）
  save <n> [--name 名前] [--yes]  いま鳴っているパッチを n に保存（確認つき、上書き前を退避）
  restore <n> [--yes]             save で上書きした n を戻す
  backup                          全パッチを backups/ に退避
  catalog [--refresh]             ペダルの ZD2 からカタログを作る
  board ...                       ボード（board.json の並び）とシーン（ボード全体の音色）
  tui                             TUI
  mcp                             MCP サーバー (stdio)
  probe                           P0 の疎通確認（開発用）
  version                         zm の版

読み出し系は --json で機械向けの出力を出す。番号はペダルの画面に合わせる（パッチ 1〜300 / 旧 MS は 1〜50、スロット 1〜6）。
backups/ と catalog/ は --dir（無ければ $ZM_DIR、それも無ければ ~/.config/zm）に置く。--dir はコマンドの後ろに書いてもよい。

--pedal（無ければ $ZM_PEDAL）で使うペダルを選ぶ。省略すると、名前に ZOOM を含むポートのうち最初に答えた対応機種。
`

type command func(args []string) error

var on, off = true, false

func commands() map[string]command {
	return map[string]command{
		"info":       Info,
		"list":       List,
		"show":       Show,
		"select":     SelectCmd,
		"effects":    Effects,
		"effect":     EffectCmd,
		"set-effect": SetEffectCmd,
		"set":        SetCmd,
		"on":         toggleCmd("on", &on),
		"off":        toggleCmd("off", &off),
		"toggle":     toggleCmd("toggle", nil),
		"rename":     RenameCmd,
		"tempo":      TempoCmd,
		"save":       SaveCmd,
		"restore":    RestoreCmd,
		"backup":     Backup,
		"catalog":    CatalogCmd,
		"board":      BoardCmd,
		"tui":        TUI,
		"mcp":        MCP,
		"probe":      Probe,
		"version":    VersionCmd,
	}
}

func Run(args []string) error {
	device.Target = os.Getenv("ZM_PEDAL")
	for len(args) > 0 {
		var dst *string
		var name string
		switch {
		case strings.HasPrefix(args[0], "--pedal"):
			dst, name = &device.Target, "--pedal"
		case strings.HasPrefix(args[0], "--dir"):
			dst, name = &paths.Dir, "--dir"
		}
		if dst == nil {
			break
		}
		if v, ok := strings.CutPrefix(args[0], name+"="); ok {
			*dst, args = v, args[1:]
			continue
		}
		if args[0] != name || len(args) < 2 {
			return fmt.Errorf("%s には値を渡す", name)
		}
		*dst, args = args[1], args[2:]
	}
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
