package catalog

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/polidog/zm/internal/zd2"
)

// Build は dir の ZD2 から device（機種名）のカタログを作る。読めない ZD2 は warn に渡して飛ばす。
func Build(dir, device string, now time.Time, warn func(file string, err error)) (*Catalog, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".ZD2") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)
	c := &Catalog{Device: device, GeneratedAt: now.UTC().Format("2006-01-02T15:04:05.000Z"), Effects: []Effect{}}
	for _, file := range files {
		data, err := os.ReadFile(filepath.Join(dir, file))
		if err != nil {
			return nil, err
		}
		z, err := zd2.Parse(data)
		if err != nil {
			if warn != nil {
				warn(file, err)
			}
			continue
		}
		e := Effect{
			ID:            FormatID(z.ID),
			Name:          z.Name,
			Group:         z.Group,
			GroupName:     z.GroupName,
			Version:       z.Version,
			Description:   z.Description,
			DescriptionJa: z.DescriptionJa,
			Params:        make([]Param, len(z.Params)),
			File:          file,
		}
		for i, p := range z.Params {
			e.Params[i] = Param(p)
		}
		c.Effects = append(c.Effects, e)
	}
	sort.SliceStable(c.Effects, func(i, j int) bool { return c.Effects[i].ID < c.Effects[j].ID })
	return c, nil
}

// Write はカタログを JSON で書く。
func (c *Catalog) Write(path string, marshal func(any) ([]byte, error)) error {
	b, err := marshal(c)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}
