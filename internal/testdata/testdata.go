// Package testdata は実機から取った backups/ と catalog/ をテストから探す。
//
// どちらも git に入れていないので、無ければテストはスキップする（zoom backup / zoom catalog で作る）。
package testdata

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// Root はリポジトリの直下。
func Root() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..")
}

func Backups() string { return filepath.Join(Root(), "backups") }
func Catalog() string { return filepath.Join(Root(), "catalog") }

// LatestAll はいちばん新しい backups/all-*/ の .syx を番号順に返す。無ければスキップ。
func LatestAll(t testing.TB) (string, []string) {
	t.Helper()
	entries, _ := os.ReadDir(Backups())
	var dirs []string
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), "all-") {
			dirs = append(dirs, e.Name())
		}
	}
	if len(dirs) == 0 {
		t.Skip("backups/all-* がない（zoom backup で作る）")
	}
	sort.Strings(dirs)
	dir := filepath.Join(Backups(), dirs[len(dirs)-1])
	files, _ := filepath.Glob(filepath.Join(dir, "*.syx"))
	sort.Strings(files)
	return dir, files
}

// CurrentDumps は backups/*-current-patch.syx（64 12 の応答）。
func CurrentDumps() []string {
	files, _ := filepath.Glob(filepath.Join(Backups(), "*-current-patch.syx"))
	return files
}
