package board

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/polidog/zm/internal/paths"
)

func useTempDir(t *testing.T) {
	t.Helper()
	old := paths.Dir
	paths.Dir = t.TempDir()
	t.Cleanup(func() { paths.Dir = old })
}

func myBoard() *Board {
	return &Board{Chain: []Stage{
		{Pedal: "ms50g"},
		{Name: "BOSS DS-1W", Knobs: map[string]string{"Dist": "10時"}},
		{Pedal: "ms70cdr"},
		{Pedal: "ms80irplus"},
	}}
}

func TestCheck(t *testing.T) {
	if err := myBoard().Check(); err != nil {
		t.Fatal(err)
	}
	cases := map[string]*Board{
		"chain が空":     {},
		"知らない機種":       {Chain: []Stage{{Pedal: "ms60b"}}},
		"2 回出てくる":      {Chain: []Stage{{Pedal: "ms50g"}, {Pedal: "MS50G"}}},
		"pedal か name": {Chain: []Stage{{Note: "?"}}},
		"knobs は書かない":  {Chain: []Stage{{Pedal: "ms50g", Knobs: map[string]string{"x": "1"}}}},
	}
	for want, b := range cases {
		err := b.Check()
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v", want, err)
		}
	}
}

func TestLoadSave(t *testing.T) {
	useTempDir(t)
	if _, err := Load(); err != ErrNoBoard {
		t.Fatalf("board.json が無いのに %v", err)
	}
	if err := myBoard().Save(); err != nil {
		t.Fatal(err)
	}
	b, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	s, i, err := b.Find("boss ds-1w")
	if err != nil || i != 1 || s.Knobs["Dist"] != "10時" {
		t.Fatalf("Find: %v %d %v", s, i, err)
	}
	if _, _, err := b.Find("DS-2"); err == nil {
		t.Fatal("無いペダルが見つかった")
	}
	var keys []string
	for _, m := range b.Models() {
		keys = append(keys, m.Key)
	}
	if strings.Join(keys, ",") != "ms50g,ms70cdr,ms80irplus" {
		t.Fatalf("Models: %v", keys)
	}
}

func TestScenes(t *testing.T) {
	useTempDir(t)
	for _, bad := range []string{"", "..", "a/b", `a\b`} {
		if _, err := ScenePath(bad); err == nil {
			t.Errorf("%q が通った", bad)
		}
	}
	sc := &Scene{Name: "lead", Note: "ソロ", SavedAt: time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC), Stages: []SceneStage{
		{Pedal: "ms50g", Sysex: "f05200"},
		{Name: "BOSS DS-1W", Knobs: map[string]string{"Dist": "2時"}},
	}}
	path, err := SaveScene(sc, false)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(path) != ScenesDir() {
		t.Fatalf("置き場: %s", path)
	}
	if _, err := SaveScene(sc, false); err == nil {
		t.Fatal("上書きできてしまった")
	}
	if _, err := SaveScene(sc, true); err != nil {
		t.Fatal(err)
	}
	back, err := LoadScene("lead")
	if err != nil {
		t.Fatal(err)
	}
	if back.Stages[0].Sysex != "f05200" || back.Stages[1].Label() != "BOSS DS-1W" || back.Stages[1].Knobs["Dist"] != "2時" {
		t.Fatalf("読み直し: %+v", back)
	}
	os.WriteFile(filepath.Join(ScenesDir(), "clean.json"), []byte(`{"name":"clean","stages":[]}`), 0o644)
	list, err := ListScenes()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].Name != "clean" || list[1].Note != "ソロ" {
		t.Fatalf("一覧: %+v", list)
	}
}
