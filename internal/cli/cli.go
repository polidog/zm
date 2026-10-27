// Package cli はサブコマンドの実装。
package cli

import (
	"fmt"
	"os"
)

const usage = `usage: zoom <command> [args]

  info                            機種・ファームウェア・パッチ数
  list                            全パッチの番号と名前
  show [n]                        パッチの中身（省略で、いま鳴っているパッチ）
  effects [group]                 エフェクト一覧
  effect <name>                   エフェクトの説明とパラメータ
  set-effect <slot> <effect>      エフェクト差し替え（保存しない）
  set <slot> <param> <value>      パラメータ変更（保存しない）
  on|off|toggle <slot>            ON / OFF（保存しない）
  save <n> [--yes]                いま鳴っているパッチを n に保存（確認つき、上書き前を退避）
  restore <n> [--yes]             save で上書きした n を戻す
  backup                          全パッチを backups/ に退避
  catalog [--refresh]             ペダルの ZD2 からカタログを作る
  tui                             TUI
  mcp                             MCP サーバー (stdio)
  probe                           P0 の疎通確認（開発用）

読み出し系は --json で機械向けの出力を出す。番号はペダルの画面に合わせる（パッチ 1〜300、スロット 1〜6）。
backups/ と catalog/ は $ZOOM_DIR（無ければカレントディレクトリ）に置く。
`

type command func(args []string) error

var on, off = true, false

func commands() map[string]command {
	return map[string]command{
		"info":       Info,
		"list":       List,
		"show":       Show,
		"effects":    Effects,
		"effect":     EffectCmd,
		"set-effect": SetEffectCmd,
		"set":        SetCmd,
		"on":         toggleCmd("on", &on),
		"off":        toggleCmd("off", &off),
		"toggle":     toggleCmd("toggle", nil),
		"save":       SaveCmd,
		"restore":    RestoreCmd,
		"backup":     Backup,
		"catalog":    CatalogCmd,
		"tui":        TUI,
		"mcp":        MCP,
		"probe":      Probe,
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
