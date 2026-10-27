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
  backup                          全パッチを backups/ に退避
  probe                           P0 の疎通確認（開発用）

読み出し系は --json で機械向けの出力を出す。番号はペダルの画面に合わせる（パッチ 1〜300、スロット 1〜6）。
backups/ と catalog/ は $ZOOM_DIR（無ければカレントディレクトリ）に置く。
`

type command func(args []string) error

func commands() map[string]command {
	return map[string]command{
		"info":    Info,
		"list":    List,
		"show":    Show,
		"effects": Effects,
		"effect":  EffectCmd,
		"backup":  Backup,
		"probe":   Probe,
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
