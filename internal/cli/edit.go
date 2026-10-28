package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/polidog/zm/internal/device"
	"github.com/polidog/zm/internal/ops"
)

func printSlot(v ops.SlotView, asJSON bool) error {
	if asJSON {
		return printJSON(v)
	}
	mark := "○"
	if v.Enabled {
		mark = "●"
	}
	var ps []string
	for _, p := range v.Params {
		if !p.Hidden {
			ps = append(ps, fmt.Sprintf("%s %d", p.Name, p.Value))
		}
	}
	fmt.Printf("%d %s %s  %s\n", v.Slot, mark, v.Effect, strings.Join(ps, "  "))
	fmt.Println("（保存していない。残すなら zm save <n>）")
	return nil
}

// slotArg と numberArg は数字かどうかだけを見る。範囲は機種で違うので、繋いでから ops が確かめる。
func slotArg(s string) (int, error) { return atoi("スロット番号", s) }

func SetEffectCmd(args []string) error {
	const usage = "set-effect <slot> <effect> [--json]"
	fs := newFlags("set-effect", usage)
	asJSON := fs.Bool("json", false, "JSON で出す")
	pos := parse(fs, args)
	if len(pos) < 2 {
		return fmt.Errorf("usage: zm %s", usage)
	}
	slot, err := slotArg(pos[0])
	if err != nil {
		return err
	}
	v, err := ops.WithZoom(true, func(z *device.Zoom) (ops.SlotView, error) {
		return ops.SetEffect(z, slot, strings.Join(pos[1:], " "))
	})
	if err != nil {
		return err
	}
	return printSlot(v, *asJSON)
}

func RenameCmd(args []string) error {
	const usage = "rename <name> [--json]"
	fs := newFlags("rename", usage)
	asJSON := fs.Bool("json", false, "JSON で出す")
	pos := parse(fs, args)
	if len(pos) == 0 {
		return fmt.Errorf("usage: zm %s", usage)
	}
	r, err := ops.WithZoom(true, func(z *device.Zoom) (ops.RenameResult, error) {
		return ops.Rename(z, strings.Join(pos, " "))
	})
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(r)
	}
	fmt.Printf("名前を %q にした\n（保存していない。残すなら zm save <n>）\n", r.Name)
	return nil
}

func SetCmd(args []string) error {
	const usage = "set <slot> <param> <value> [--json]"
	fs := newFlags("set", usage)
	asJSON := fs.Bool("json", false, "JSON で出す")
	pos := parse(fs, args)
	if len(pos) < 3 {
		return fmt.Errorf("usage: zm %s", usage)
	}
	slot, err := slotArg(pos[0])
	if err != nil {
		return err
	}
	value, err := atoi("値", pos[len(pos)-1])
	if err != nil {
		return err
	}
	param := strings.Join(pos[1:len(pos)-1], " ")
	v, err := ops.WithZoom(true, func(z *device.Zoom) (ops.SlotView, error) {
		return ops.SetParam(z, slot, param, value)
	})
	if err != nil {
		return err
	}
	return printSlot(v, *asJSON)
}

func toggleCmd(name string, enabled *bool) command {
	return func(args []string) error {
		usage := name + " <slot> [--json]"
		fs := newFlags(name, usage)
		asJSON := fs.Bool("json", false, "JSON で出す")
		pos := parse(fs, args)
		if err := want(pos, 1, 1, usage); err != nil {
			return err
		}
		slot, err := slotArg(pos[0])
		if err != nil {
			return err
		}
		v, err := ops.WithZoom(true, func(z *device.Zoom) (ops.SlotView, error) { return ops.Toggle(z, slot, enabled) })
		if err != nil {
			return err
		}
		return printSlot(v, *asJSON)
	}
}

func SelectCmd(args []string) error {
	const usage = "select <n> [--json]"
	fs := newFlags("select", usage)
	asJSON := fs.Bool("json", false, "JSON で出す（show --json と同じ形）")
	pos := parse(fs, args)
	if err := want(pos, 1, 1, usage); err != nil {
		return err
	}
	number, err := atoi("パッチ番号", pos[0])
	if err != nil {
		return err
	}
	v, err := ops.WithZoom(true, func(z *device.Zoom) (ops.PatchView, error) { return ops.Select(z, number) })
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(v)
	}
	printPatch(v)
	return nil
}

func TempoCmd(args []string) error {
	const usage = "tempo <bpm> [--json]"
	fs := newFlags("tempo", usage)
	asJSON := fs.Bool("json", false, "JSON で出す")
	pos := parse(fs, args)
	if err := want(pos, 1, 1, usage); err != nil {
		return err
	}
	bpm, err := atoi("テンポ", pos[0])
	if err != nil {
		return err
	}
	r, err := ops.WithZoom(true, func(z *device.Zoom) (ops.TempoResult, error) { return ops.SetTempo(z, bpm) })
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(r)
	}
	fmt.Printf("%q のテンポを %d にした\n（保存していない。残すなら zm save <n>）\n", r.Name, r.Tempo)
	return nil
}

// confirm は端末で y を打たせる。端末でなければ断る（--yes を付けさせる）。
func confirm(question string) error {
	if st, err := os.Stdin.Stat(); err != nil || st.Mode()&os.ModeCharDevice == 0 {
		return fmt.Errorf("%s\n端末から実行するか、--yes を付ける", question)
	}
	fmt.Fprintf(os.Stderr, "%s [y/N] ", question)
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	if a := strings.ToLower(strings.TrimSpace(line)); a != "y" && a != "yes" {
		return fmt.Errorf("やめた")
	}
	return nil
}

func numberArg(s string) (int, error) { return atoi("パッチ番号", s) }

func SaveCmd(args []string) error {
	const usage = "save <n> [--yes] [--json]"
	fs := newFlags("save", usage)
	yes := fs.Bool("yes", false, "確認しない")
	asJSON := fs.Bool("json", false, "JSON で出す")
	pos := parse(fs, args)
	if err := want(pos, 1, 1, usage); err != nil {
		return err
	}
	number, err := numberArg(pos[0])
	if err != nil {
		return err
	}
	r, err := ops.WithZoom(true, func(z *device.Zoom) (ops.SaveResult, error) {
		if !*yes {
			n, err := ops.PatchIndex(z.Model, number)
			if err != nil {
				return ops.SaveResult{}, err
			}
			target, err := z.Patch(n)
			if err != nil {
				return ops.SaveResult{}, err
			}
			current, err := z.CurrentPatch()
			if err != nil {
				return ops.SaveResult{}, err
			}
			q := fmt.Sprintf("いま鳴っている %q を、パッチ %d（いまは %q）に保存する。上書き前の中身は backups/overwritten/ に退避する。", current.Name, number, target.Name)
			if err := confirm(q); err != nil {
				return ops.SaveResult{}, err
			}
		}
		return ops.Save(z, number)
	})
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(r)
	}
	fmt.Printf("%d に %q を保存した（上書きした %q は %s）\n", r.Number, r.Saved, r.Overwritten, r.Backup)
	return nil
}

func RestoreCmd(args []string) error {
	const usage = "restore <n> [--yes] [--json]"
	fs := newFlags("restore", usage)
	yes := fs.Bool("yes", false, "確認しない")
	asJSON := fs.Bool("json", false, "JSON で出す")
	pos := parse(fs, args)
	if err := want(pos, 1, 1, usage); err != nil {
		return err
	}
	number, err := numberArg(pos[0])
	if err != nil {
		return err
	}
	r, err := ops.WithZoom(true, func(z *device.Zoom) (ops.RestoreResult, error) {
		if !*yes {
			path, data, err := ops.LatestBackup(z.Model, number)
			if err != nil {
				return ops.RestoreResult{}, err
			}
			p, err := device.DecodeSysex(data)
			if err != nil {
				return ops.RestoreResult{}, fmt.Errorf("%s: %w", path, err)
			}
			if err := confirm(fmt.Sprintf("パッチ %d を %s の %q に戻す。", number, path, p.Name)); err != nil {
				return ops.RestoreResult{}, err
			}
		}
		return ops.Restore(z, number)
	})
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(r)
	}
	fmt.Printf("%d を %q に戻した（%q を置き換え、%s から）\n", r.Number, r.Restored, r.Replaced, r.From)
	return nil
}
