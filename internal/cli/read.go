package cli

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/polidog/zoom-ai/internal/device"
	"github.com/polidog/zoom-ai/internal/ops"
)

func table() *tabwriter.Writer { return tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0) }

func Info(args []string) error {
	fs := newFlags("info", "info [--json]")
	asJSON := fs.Bool("json", false, "JSON で出す")
	if err := want(parse(fs, args), 0, 0, "info [--json]"); err != nil {
		return err
	}
	id, err := ops.WithZoom(false, func(z *device.Zoom) (ops.Identity, error) { return ops.Identify(z), nil })
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(id)
	}
	w := table()
	fmt.Fprintf(w, "機種\t%s (model %s)\n", id.Device, id.Model)
	fmt.Fprintf(w, "ファームウェア\t%s\n", id.Firmware)
	fmt.Fprintf(w, "パッチ\t%d\n", id.Patches)
	fmt.Fprintf(w, "スロット\t%d\n", id.UserSlots)
	return w.Flush()
}

func List(args []string) error {
	fs := newFlags("list", "list [--json]")
	asJSON := fs.Bool("json", false, "JSON で出す")
	if err := want(parse(fs, args), 0, 0, "list [--json]"); err != nil {
		return err
	}
	list, err := ops.WithZoom(false, ops.ListPatches)
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(list)
	}
	w := table()
	for _, p := range list {
		fmt.Fprintf(w, "%3d\t%s\n", p.Number, p.Name)
	}
	return w.Flush()
}

const showUsage = "show [n] [--file f.syx] [--json] [--raw]"

func Show(args []string) error {
	fs := newFlags("show", showUsage)
	file := fs.String("file", "", "ペダルではなく保存済みの .syx を読む")
	asJSON := fs.Bool("json", false, "JSON で出す（カタログの名前つき）")
	raw := fs.Bool("raw", false, "デコードしたパッチをそのまま JSON で出す（カタログを引かない）")
	pos := parse(fs, args)
	if err := want(pos, 0, 1, showUsage); err != nil {
		return err
	}
	number := 0
	if len(pos) == 1 {
		if *file != "" {
			return fmt.Errorf("--file と番号は同時に使えない")
		}
		n, err := atoi("パッチ番号", pos[0])
		if err != nil {
			return err
		}
		if n < 1 {
			return fmt.Errorf("パッチ番号は 1 から")
		}
		number = n
	}

	var view ops.PatchView
	if *file != "" {
		msg, err := os.ReadFile(*file)
		if err != nil {
			return err
		}
		p, err := device.DecodeSysex(msg)
		if err != nil {
			return fmt.Errorf("%s: %w", *file, err)
		}
		if *raw {
			return printJSON(p)
		}
		cat, err := ops.CatalogFor(device.ModelOfSysex(msg))
		if err != nil {
			return err
		}
		if *asJSON {
			// TypeScript 版の bun run dump <file> --named と同じ形
			return printJSON(cat.Describe(p))
		}
		view = ops.PatchView{Number: *file, NamedPatch: cat.Describe(p)}
	} else if *raw {
		p, err := ops.WithZoom(false, func(z *device.Zoom) (any, error) {
			if number == 0 {
				return z.CurrentPatch()
			}
			n, err := ops.PatchIndex(z.Model, number)
			if err != nil {
				return nil, err
			}
			return z.Patch(n)
		})
		if err != nil {
			return err
		}
		return printJSON(p)
	} else {
		v, err := ops.WithZoom(false, func(z *device.Zoom) (ops.PatchView, error) { return ops.GetPatch(z, number) })
		if err != nil {
			return err
		}
		if *asJSON {
			return printJSON(v)
		}
		view = v
	}
	printPatch(view)
	return nil
}

func printPatch(v ops.PatchView) {
	fmt.Printf("%v  %s    vol %d  tempo %d\n", v.Number, v.Name, v.Volume, v.Tempo)
	if v.Description != "" {
		fmt.Printf("  %s\n", strings.ReplaceAll(strings.TrimSpace(v.Description), "\n", "\n  "))
	}
	w := table()
	for _, s := range v.Slots {
		mark := "○"
		if s.Enabled {
			mark = "●"
		}
		group := ""
		if s.Group != nil {
			group = *s.Group
		}
		fmt.Fprintf(w, "  %d %s\t%s\t%s\t", s.Slot+1, mark, s.Effect, group)
		var ps []string
		for _, p := range s.Params {
			if !p.Hidden {
				ps = append(ps, fmt.Sprintf("%s %d", p.Name, p.Value))
			}
		}
		fmt.Fprintf(w, "%s\n", strings.Join(ps, "  "))
	}
	w.Flush()
}

func Backup(args []string) error {
	fs := newFlags("backup", "backup")
	if err := want(parse(fs, args), 0, 0, "backup"); err != nil {
		return err
	}
	r, err := ops.WithZoom(false, func(z *device.Zoom) (ops.BackupResult, error) {
		return ops.BackupAll(z, func(done int) {
			if done%50 == 0 {
				fmt.Fprintf(os.Stderr, "%d patches\n", done)
			}
		})
	})
	if err != nil {
		return err
	}
	fmt.Printf("saved %d patches to %s\n", r.Saved, r.Dir)
	return nil
}

func Effects(args []string) error {
	const usage = "effects [group] [--json]"
	fs := newFlags("effects", usage)
	asJSON := fs.Bool("json", false, "JSON で出す")
	pos := parse(fs, args)
	if err := want(pos, 0, 1, usage); err != nil {
		return err
	}
	group := ""
	if len(pos) == 1 {
		group = pos[0]
	}
	list, err := ops.ListEffects(group)
	if err != nil {
		return err
	}
	if len(list) == 0 && group != "" {
		cat, _ := ops.Catalog()
		return fmt.Errorf("グループ %q はない。あるのは: %s", group, strings.Join(cat.Groups(), ", "))
	}
	if *asJSON {
		return printJSON(list)
	}
	w := table()
	for _, e := range list {
		fmt.Fprintf(w, "%s\t%s\t%s\n", e.Group, e.Name, strings.Join(e.Params, ", "))
	}
	return w.Flush()
}

func EffectCmd(args []string) error {
	const usage = "effect <name|id> [--json]"
	fs := newFlags("effect", usage)
	asJSON := fs.Bool("json", false, "JSON で出す")
	pos := parse(fs, args)
	if len(pos) == 0 {
		return fmt.Errorf("usage: zoom %s", usage)
	}
	d, err := ops.DescribeEffect(strings.Join(pos, " "))
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(d)
	}
	printEffect(d)
	return nil
}

func printEffect(d ops.EffectDetail) {
	fmt.Printf("%s  (%s, %s)\n", d.Name, d.Group, d.ID)
	if d.Description != "" {
		fmt.Printf("  %s\n", strings.TrimSpace(d.Description))
	}
	w := table()
	for _, p := range d.Params {
		fmt.Fprintf(w, "  %s\t0-%d\t既定 %d\t%s\n", p.Name, p.Max, p.Default, p.Explanation)
	}
	w.Flush()
}
