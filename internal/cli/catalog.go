package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/polidog/zoom-ai/internal/catalog"
	"github.com/polidog/zoom-ai/internal/device"
	"github.com/polidog/zoom-ai/internal/ops"
	"github.com/polidog/zoom-ai/internal/pedalfs"
)

// CatalogCmd はペダルの ZD2 を全部読み出し、catalog/g2four.json を作る。
//
// 読み出した ZD2 は catalog/zd2/ にキャッシュし、2 回目以降はペダルに触らない（--refresh で取り直す）。
// ペダルには書き込まない。
func CatalogCmd(args []string) error {
	const usage = "catalog [--refresh]"
	fs := newFlags("catalog", usage)
	refresh := fs.Bool("refresh", false, "キャッシュがあってもペダルから取り直す")
	if err := want(parse(fs, args), 0, 0, usage); err != nil {
		return err
	}
	cache := catalog.ZD2Dir()
	if err := os.MkdirAll(cache, 0o755); err != nil {
		return err
	}
	cached, _ := filepath.Glob(filepath.Join(cache, "*.ZD2"))
	if *refresh || len(cached) == 0 {
		if _, err := ops.WithZoom(false, func(z *device.Zoom) (struct{}, error) {
			return struct{}{}, download(pedalfs.New(z.Conn, z.DeviceID()), cache, *refresh)
		}); err != nil {
			return err
		}
	}
	c, err := catalog.Build(cache, time.Now(), func(file string, err error) {
		fmt.Fprintf(os.Stderr, "%s: %v\n", file, err)
	})
	if err != nil {
		return err
	}
	if err := c.Write(catalog.Path(), ops.JSON); err != nil {
		return err
	}
	ops.ResetCatalog()
	fmt.Printf("%d effects → %s\n", len(c.Effects), catalog.Path())
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
