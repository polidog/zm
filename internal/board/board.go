// Package board はエフェクターボード（何台かのペダルをつないだ並び）と、その音色（シーン）をファイルで扱う。
//
// ボードは board.json に、信号の流れの順でペダルを並べる。zm で触れるペダルは pedal に機種名を書き、
// MIDI の無いペダル（アナログの歪みなど）は name とつまみの位置 (knobs) を書く。
// つまみは zm からは回せないので、いまの位置を人が書いておき、AI が前後の音作りに使う。
//
// シーンはボード全体の音色 1 つ。zm で触れるペダルは現在パッチの SysEx を丸ごと持ち、
// 呼び出すと各ペダルの現在パッチに流し込む（ペダルのパッチメモリには保存しない）。
// アナログのつまみは書いておくだけで、呼び出したときに人が回す。
//
// どちらも paths.Root() の下（board.json、scenes/<名前>.json）に置く。
package board

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/polidog/zm/internal/catalog"
	"github.com/polidog/zm/internal/device"
	"github.com/polidog/zm/internal/paths"
)

// Stage はボードの 1 台。Pedal か Name のどちらかを書く。
type Stage struct {
	// Pedal は zm で触れるペダルの機種名（ms50g / ms70cdr / ms80irplus / g2four）。
	Pedal string `json:"pedal,omitempty"`
	// Name は zm で触れないペダルの名前（BOSS DS-1W など）。Pedal のときは書かなくてよい。
	Name string `json:"name,omitempty"`
	// Note はこのペダルの役目や音のくせ。AI が読む。
	Note string `json:"note,omitempty"`
	// Knobs は zm で触れないペダルのつまみ（いまの位置）。値は "12時" "7" "Custom" など書きやすい形でよい。
	Knobs map[string]string `json:"knobs,omitempty"`
}

// Digital は zm で触れるペダルか。
func (s Stage) Digital() bool { return s.Pedal != "" }

// Label は表示と指定に使う名前。zm で触れるペダルは機種名。
func (s Stage) Label() string {
	if s.Digital() {
		return s.Pedal
	}
	return s.Name
}

// Model は zm で触れるペダルの機種。
func (s Stage) Model() (device.Model, bool) {
	if !s.Digital() {
		return device.Model{}, false
	}
	return device.ModelByKey(s.Pedal)
}

type Board struct {
	// Note はボード全体の説明（アンプ、ギター、好みの音など）。AI が読む。
	Note string `json:"note,omitempty"`
	// Chain は信号の流れの順（ギター側が先）。
	Chain []Stage `json:"chain"`
}

func Path() string { return filepath.Join(paths.Root(), "board.json") }

// ErrNoBoard は board.json が無いとき。
var ErrNoBoard = errors.New("board.json が無い（README の「ボード」を見て ~/.config/zm/board.json を書く）")

func Load() (*Board, error) {
	b, err := os.ReadFile(Path())
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNoBoard
	}
	if err != nil {
		return nil, err
	}
	var bd Board
	if err := json.Unmarshal(b, &bd); err != nil {
		return nil, fmt.Errorf("%s: %w", Path(), err)
	}
	return &bd, bd.Check()
}

// Check は並びが使えるかを確かめる。機種名が分かること、名前が重ならないこと。
func (b *Board) Check() error {
	if len(b.Chain) == 0 {
		return errors.New("board.json の chain が空")
	}
	seen := map[string]bool{}
	for i, s := range b.Chain {
		if s.Digital() && len(s.Knobs) > 0 {
			return fmt.Errorf("chain[%d]: zm で触れるペダルに knobs は書かない", i)
		}
		if s.Label() == "" {
			return fmt.Errorf("chain[%d]: pedal か name を書く", i)
		}
		if s.Digital() {
			if _, ok := s.Model(); !ok {
				return fmt.Errorf("chain[%d]: 知らない機種 %q", i, s.Pedal)
			}
		}
		key := strings.ToLower(s.Label())
		if seen[key] {
			return fmt.Errorf("chain[%d]: %s が 2 回出てくる", i, s.Label())
		}
		seen[key] = true
	}
	return nil
}

func (b *Board) Save() error {
	if err := b.Check(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(paths.Root(), 0o755); err != nil {
		return err
	}
	return os.WriteFile(Path(), append(data, '\n'), 0o644)
}

// Find は名前（機種名かアナログの name。大文字小文字は問わない）で 1 台を引く。i は Chain の添字。
func (b *Board) Find(label string) (s *Stage, i int, err error) {
	for i := range b.Chain {
		if strings.EqualFold(b.Chain[i].Label(), label) {
			return &b.Chain[i], i, nil
		}
	}
	var names []string
	for _, s := range b.Chain {
		names = append(names, s.Label())
	}
	return nil, 0, fmt.Errorf("ボードに %q は無い。あるのは: %s", label, strings.Join(names, ", "))
}

// Models はボードにある zm で触れるペダルの機種（並びの順）。
func (b *Board) Models() []device.Model {
	var out []device.Model
	for _, s := range b.Chain {
		if m, ok := s.Model(); ok {
			out = append(out, m)
		}
	}
	return out
}

// ---- シーン ----

// SceneStage はシーンの中の 1 台。
type SceneStage struct {
	Pedal string `json:"pedal,omitempty"`
	Name  string `json:"name,omitempty"`
	// Sysex は現在パッチの SysEx（hex）。呼び出すときはこれだけを使う。
	Sysex string `json:"sysex,omitempty"`
	// Patch は Sysex を読める形にしたもの（人と AI が読む用。呼び出しには使わない）。
	Patch *catalog.NamedPatch `json:"patch,omitempty"`
	Knobs map[string]string   `json:"knobs,omitempty"`
}

func (s SceneStage) Label() string { return Stage{Pedal: s.Pedal, Name: s.Name}.Label() }

type Scene struct {
	Name string `json:"name"`
	// Note は狙った音（曲名、「80 年代っぽいクリーン」など）。
	Note    string       `json:"note,omitempty"`
	SavedAt time.Time    `json:"savedAt"`
	Stages  []SceneStage `json:"stages"`
}

func ScenesDir() string { return filepath.Join(paths.Root(), "scenes") }

// ScenePath は名前のシーンの置き場。名前にパスの区切りは使えない。
func ScenePath(name string) (string, error) {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) {
		return "", fmt.Errorf("シーン名 %q は使えない", name)
	}
	return filepath.Join(ScenesDir(), name+".json"), nil
}

func LoadScene(name string) (*Scene, error) {
	path, err := ScenePath(name)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("シーン %q は無い", name)
	}
	if err != nil {
		return nil, err
	}
	var sc Scene
	if err := json.Unmarshal(b, &sc); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &sc, nil
}

// SaveScene は sc を書く。同じ名前があれば overwrite のときだけ上書きする。
func SaveScene(sc *Scene, overwrite bool) (string, error) {
	path, err := ScenePath(sc.Name)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(path); err == nil && !overwrite {
		return "", fmt.Errorf("シーン %q はもうある（上書きするなら overwrite）", sc.Name)
	}
	data, err := json.MarshalIndent(sc, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(ScenesDir(), 0o755); err != nil {
		return "", err
	}
	return path, os.WriteFile(path, append(data, '\n'), 0o644)
}

// SceneEntry はシーン一覧の 1 行。
type SceneEntry struct {
	Name    string    `json:"name"`
	Note    string    `json:"note,omitempty"`
	SavedAt time.Time `json:"savedAt"`
}

func ListScenes() ([]SceneEntry, error) {
	files, err := filepath.Glob(filepath.Join(ScenesDir(), "*.json"))
	if err != nil {
		return nil, err
	}
	out := []SceneEntry{}
	for _, f := range files {
		name := strings.TrimSuffix(filepath.Base(f), ".json")
		sc, err := LoadScene(name)
		if err != nil {
			return nil, err
		}
		out = append(out, SceneEntry{Name: name, Note: sc.Note, SavedAt: sc.SavedAt})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
