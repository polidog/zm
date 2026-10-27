package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/polidog/zm/internal/catalog"
	"github.com/polidog/zm/internal/device"
	"github.com/polidog/zm/internal/ops"
	"github.com/polidog/zm/internal/pedalfs"
)

// CatalogCmd はペダルの ZD2 を全部読み出し、catalog/<機種>.json（g2four.json / ms80irplus.json）を作る。
//
// 読み出した ZD2 は catalog/zd2/（G2 FOUR）か catalog/zd2-<機種>/ にキャッシュし、2 回目以降はペダルに触らない（--refresh で取り直す）。
// ペダルには書き込まない。
func CatalogCmd(args []string) error {
	const usage = "catalog [--refresh]"
	fs := newFlags("catalog", usage)
	refresh := fs.Bool("refresh", false, "キャッシュがあってもペダルから取り直す")
	if err := want(parse(fs, args), 0, 0, usage); err != nil {
		return err
	}
	m := ops.Model()
	if m.Legacy {
		return fmt.Errorf("%s のカタログは zm に埋め込んである（ペダルから作るものはない）", m.Name)
	}
	cache := catalog.ZD2Dir(m.Key)
	if err := os.MkdirAll(cache, 0o755); err != nil {
		return err
	}
	cached, _ := filepath.Glob(filepath.Join(cache, "*.ZD2"))
	if *refresh || len(cached) == 0 {
		if _, err := ops.WithZoom(false, func(z *device.Zoom) (struct{}, error) {
			if z.Model.Key != m.Key {
				return struct{}{}, fmt.Errorf("繋がっているのは %s で、%s ではない", z.Model.Name, m.Name)
			}
			return struct{}{}, download(pedalfs.New(z.Conn, z.DeviceID()), cache, *refresh)
		}); err != nil {
			return err
		}
	}
	c, err := catalog.Build(cache, m.Name, time.Now(), func(file string, err error) {
		fmt.Fprintf(os.Stderr, "%s: %v\n", file, err)
	})
	if err != nil {
		return err
	}
	if err := c.Write(catalog.Path(m.Key), ops.JSON); err != nil {
		return err
	}
	ops.ResetCatalog()
	fmt.Printf("%d effects → %s\n", len(c.Effects), catalog.Path(m.Key))
	return nil
}

func download(files *pedalfs.Files, cache string, refresh bool) (err error) {
	if err := files.PCModeOn(); err != nil {
		return err
	}
	defer func() {
		if perr := files.PCModeOff(); err == nil {
			err = perr
		}
	}()
	all, err := files.List()
	if err != nil {
		return err
	}
	var names []string
	for _, n := range all {
		if strings.HasSuffix(n, ".ZD2") {
			names = append(names, n)
		}
	}
	fmt.Fprintf(os.Stderr, "ペダルに %d ファイル（うち ZD2 %d）\n", len(all), len(names))
	for i, name := range names {
		path := filepath.Join(cache, name)
		if _, err := os.Stat(path); err == nil && !refresh {
			continue
		}
		data, err := files.Download(name)
		if err != nil {
			return err
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "[%d/%d] %s\n", i+1, len(names), name)
	}
	return nil
}
