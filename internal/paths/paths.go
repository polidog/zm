// Package paths は backups/ と catalog/ を置く場所を決める。
//
// ZOOM_DIR があればそこ、無ければカレントディレクトリ。TypeScript 版はリポジトリ直下に置いていたので、
// リポジトリで実行すれば同じファイルを読み書きする。
package paths

import (
	"os"
	"path/filepath"
)

func Root() string {
	if d := os.Getenv("ZOOM_DIR"); d != "" {
		return d
	}
	if d, err := os.Getwd(); err == nil {
		return d
	}
	return "."
}

func Backups() string { return filepath.Join(Root(), "backups") }
func Catalog() string { return filepath.Join(Root(), "catalog") }
