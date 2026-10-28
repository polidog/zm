package cli

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/polidog/zm/internal/catalog"
	"github.com/polidog/zm/internal/ops"
)

const boardUsage = `board [--json]                     ボードの並びと各ペダルのいまの音
  board scenes [--json]              保存したシーンの一覧
  board save <name> [--note n] [--force]  各ペダルのいまの音をシーンとして残す（ペダルには書かない）
  board load <name> [--json]         シーンを各ペダルに流し込む（保存しない）
  board knob <name> <つまみ>=<位置>...  アナログのペダルのつまみの位置を書く`

// BoardCmd は board.json のボードと、シーン（ボード全体の音色）を扱う。
func BoardCmd(args []string) error {
	sub := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub, args = args[0], args[1:]
	}
	switch sub {
	case "":
		return boardShow(args)
	case "scenes":
		return boardScenes(args)
	case "save":
		return boardSave(args)
	case "load":
		return boardLoad(args)
	case "knob":
		return boardKnob(args)
	}
	return fmt.Errorf("usage: zm %s", boardUsage)
}

func knobText(knobs map[string]string) string {
	var out []string
	for _, k := range slices.Sorted(maps.Keys(knobs)) {
		out = append(out, k+" "+knobs[k])
	}
	return strings.Join(out, "  ")
}

func slotsText(p catalog.NamedPatch) string {
	var out []string
	for _, s := range p.Slots {
		mark := "○"
		if s.Enabled {
			mark = "●"
		}
		out = append(out, mark+" "+s.Effect)
	}
	return strings.Join(out, " → ")
}

func boardShow(args []string) error {
	fs := newFlags("board", "board [--json]")
	asJSON := fs.Bool("json", false, "JSON で出す")
	if err := want(parse(fs, args), 0, 0, "board [--json]"); err != nil {
		return err
	}
	v, err := ops.GetBoard()
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(v)
	}
	w := table()
	for _, s := range v.Stages {
		switch {
		case s.Error != "":
			fmt.Fprintf(w, "%d\t%s\t（読めない: %s）\n", s.Position, s.Label, strings.ReplaceAll(s.Error, "\n", " / "))
		case s.Patch != nil:
			fmt.Fprintf(w, "%d\t%s\t%s\t%s\n", s.Position, s.Label, s.Patch.Name, slotsText(s.Patch.NamedPatch))
		default:
			fmt.Fprintf(w, "%d\t%s\t（アナログ）\t%s\n", s.Position, s.Label, knobText(s.Knobs))
		}
	}
	return w.Flush()
}

func boardScenes(args []string) error {
	fs := newFlags("board scenes", "board scenes [--json]")
	asJSON := fs.Bool("json", false, "JSON で出す")
	if err := want(parse(fs, args), 0, 0, "board scenes [--json]"); err != nil {
		return err
	}
	list, err := ops.ListScenes()
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(list)
	}
	w := table()
	for _, s := range list {
		fmt.Fprintf(w, "%s\t%s\t%s\n", s.Name, s.SavedAt.Local().Format("2006-01-02 15:04"), s.Note)
	}
	return w.Flush()
}

func boardSave(args []string) error {
	const usage = "board save <name> [--note n] [--force]"
	fs := newFlags("board save", usage)
	note := fs.String("note", "", "狙った音のメモ")
	force := fs.Bool("force", false, "同じ名前のシーンを上書きする")
	pos := parse(fs, args)
	if err := want(pos, 1, 1, usage); err != nil {
		return err
	}
	r, err := ops.SaveScene(pos[0], *note, *force)
	if err != nil {
		return err
	}
	for _, s := range r.Stages {
		if s.Patch != nil {
			fmt.Printf("%s\t%s\n", s.Label(), s.Patch.Name)
		} else {
			fmt.Printf("%s\t%s\n", s.Label(), knobText(s.Knobs))
		}
	}
	fmt.Printf("%s に残した（ペダルには書いていない）\n", r.Path)
	return nil
}

func boardLoad(args []string) error {
	const usage = "board load <name> [--json]"
	fs := newFlags("board load", usage)
	asJSON := fs.Bool("json", false, "JSON で出す")
	pos := parse(fs, args)
	if err := want(pos, 1, 1, usage); err != nil {
		return err
	}
	r, err := ops.LoadScene(pos[0])
	if *asJSON {
		if perr := printJSON(r); perr != nil {
			return perr
		}
		return err
	}
	for _, s := range r.Stages {
		switch {
		case s.Error != "":
			fmt.Printf("%s\t× %s\n", s.Label, strings.ReplaceAll(s.Error, "\n", " / "))
		case s.Loaded:
			fmt.Printf("%s\t%s\n", s.Label, s.Patch)
		default:
			fmt.Printf("%s\tつまみを回す: %s\n", s.Label, knobText(s.Knobs))
		}
	}
	if err == nil {
		fmt.Println("（保存していない。MS-50G / MS-70CDR は AUTO SAVE が ON だと、ペダルで切り替えた時点で保存される）")
	}
	return err
}

func boardKnob(args []string) error {
	const usage = "board knob <name> <つまみ>=<位置>..."
	fs := newFlags("board knob", usage)
	pos := parse(fs, args)
	if len(pos) < 2 {
		return fmt.Errorf("usage: zm %s", usage)
	}
	knobs := map[string]string{}
	for _, kv := range pos[1:] {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || k == "" {
			return fmt.Errorf("つまみは <つまみ>=<位置> で書く: %q", kv)
		}
		knobs[k] = v
	}
	s, err := ops.SetKnobs(pos[0], knobs)
	if err != nil {
		return err
	}
	fmt.Printf("%s\t%s\n", s.Label, knobText(s.Knobs))
	return nil
}
