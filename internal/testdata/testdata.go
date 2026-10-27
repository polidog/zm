// Package testdata は実機から取った backups/ と catalog/ をテストから探す。置き場は CLI と同じ（paths.Root）。
//
// どちらも git に入れていないので、無ければテストはスキップする（zm backup / zm catalog で作る）。
package testdata

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/polidog/zm/internal/paths"
)

func Backups() string { return paths.Backups() }
func Catalog() string { return paths.Catalog() }

// LatestAll はいちばん新しい backups/all-*/ の .syx を番号順に返す。無ければスキップ。
func LatestAll(t testing.TB) (string, []string) {
	t.Helper()
	return LatestAllIn(t, "")
}

// LatestAllIn は backups/<sub>/all-*/ で LatestAll と同じことをする（sub は device.Model.BackupDir）。
func LatestAllIn(t testing.TB, sub string) (string, []string) {
	t.Helper()
	root := filepath.Join(Backups(), sub)
	entries, _ := os.ReadDir(root)
	var dirs []string
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), "all-") {
			dirs = append(dirs, e.Name())
		}
	}
	if len(dirs) == 0 {
		t.Skipf("%s/all-* がない（zm backup で作る）", root)
	}
	sort.Strings(dirs)
	dir := filepath.Join(root, dirs[len(dirs)-1])
	files, _ := filepath.Glob(filepath.Join(dir, "*.syx"))
	sort.Strings(files)
	return dir, files
}

// CurrentDumps は backups/*-current-patch.syx（64 12 の応答）。
func CurrentDumps() []string {
	files, _ := filepath.Glob(filepath.Join(Backups(), "*-current-patch.syx"))
	return files
}
