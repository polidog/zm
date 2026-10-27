// Package paths は backups/ と catalog/ を置く場所を決める。
//
// --dir、$ZM_DIR、$XDG_CONFIG_HOME/zm（無ければ ~/.config/zm）の順に見る。
// どこで実行しても同じ場所を読み書きするよう、カレントディレクトリは見ない。ディレクトリは書くときに作る。
package paths

import (
	"os"
	"path/filepath"
)

// Dir は --dir で渡された場所。空なら環境変数と既定の場所を見る。
var Dir string

func Root() string {
	if Dir != "" {
		return Dir
	}
	if d := os.Getenv("ZM_DIR"); d != "" {
		return d
	}
	if d, err := os.UserConfigDir(); err == nil {
		return filepath.Join(d, "zm")
	}
	return "."
}

func Backups() string { return filepath.Join(Root(), "backups") }
func Catalog() string { return filepath.Join(Root(), "catalog") }
