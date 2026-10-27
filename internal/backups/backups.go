// Package backups は backups/ への退避と復元。中身はペダルの応答 (45 ...) をそのまま .syx にしたもの。
//
//	backups/all-<時刻>/NNN.syx         全パッチ (NNN は 0 始まり)
//	backups/overwritten/<時刻>-NNN.syx  保存で上書きされる直前の中身
package backups

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/polidog/zoom-ai/internal/device"
	"github.com/polidog/zoom-ai/internal/paths"
)

func Dir() string            { return paths.Backups() }
func overwrittenDir() string { return filepath.Join(Dir(), "overwritten") }

func nnn(n int) string { return fmt.Sprintf("%03d", n) }

// All は全パッチを all-<時刻>/ に退避し、そのディレクトリを返す。
func All(z *device.Zoom, progress func(done int)) (string, error) {
	dir := filepath.Join(Dir(), "all-"+Timestamp(time.Now()))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	for n := range device.PatchCount {
		msg, err := z.PatchSysex(n)
		if err != nil {
			return "", err
		}
		if err := os.WriteFile(filepath.Join(dir, nnn(n)+".syx"), msg, 0o644); err != nil {
			return "", err
		}
		if progress != nil {
			progress(n + 1)
		}
	}
	return dir, nil
}

// BeforeOverwrite はパッチ n の今の中身を overwritten/ に退避し、そのパスを返す。
func BeforeOverwrite(z *device.Zoom, n int) (string, error) {
	if err := os.MkdirAll(overwrittenDir(), 0o755); err != nil {
		return "", err
	}
	msg, err := z.PatchSysex(n)
	if err != nil {
		return "", err
	}
	path := filepath.Join(overwrittenDir(), Timestamp(time.Now())+"-"+nnn(n)+".syx")
	return path, os.WriteFile(path, msg, 0o644)
}

// LatestOverwritten はパッチ n の、いちばん新しい退避。無ければ path が空。
func LatestOverwritten(n int) (path string, data []byte, err error) {
	entries, err := os.ReadDir(overwrittenDir())
	if os.IsNotExist(err) {
		return "", nil, nil
	}
	if err != nil {
		return "", nil, err
	}
	var names []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), "-"+nnn(n)+".syx") {
			names = append(names, e.Name())
		}
	}
	if len(names) == 0 {
		return "", nil, nil
	}
	sort.Strings(names)
	path = filepath.Join(overwrittenDir(), names[len(names)-1])
	data, err = os.ReadFile(path)
	return path, data, err
}
