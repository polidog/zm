package ops

import (
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"time"

	"github.com/polidog/zm/internal/board"
	"github.com/polidog/zm/internal/device"
)

// ---- ボード（何台かのペダルの並び） ----

// StageView はボードの 1 台といまの中身。繋がっていない・読めないペダルは Error に理由を入れ、ほかの台は読む。
type StageView struct {
	// Position は信号の流れの順（1 始まり、ギター側が 1）。
	Position int               `json:"position"`
	Label    string            `json:"label"`
	Device   string            `json:"device,omitempty"`
	Note     string            `json:"note,omitempty"`
	Knobs    map[string]string `json:"knobs,omitempty"`
	// Patch は zm で触れるペダルのいま鳴っている（編集中の）パッチ。
	Patch *PatchView `json:"patch,omitempty"`
	Error string     `json:"error,omitempty"`
}

type BoardView struct {
	Note   string      `json:"note,omitempty"`
	Stages []StageView `json:"stages"`
}

// GetBoard はボードの並びと、各ペダルのいま鳴っているパッチを返す。
func GetBoard() (BoardView, error) {
	bd, err := board.Load()
	if err != nil {
		return BoardView{}, err
	}
	v := BoardView{Note: bd.Note, Stages: []StageView{}}
	for i, s := range bd.Chain {
		sv := StageView{Position: i + 1, Label: s.Label(), Note: s.Note, Knobs: s.Knobs}
		if m, ok := s.Model(); ok {
			sv.Device = m.Name
			p, err := WithPedal(s.Pedal, false, func(z *device.Zoom) (PatchView, error) { return GetPatch(z, 0) })
			if err != nil {
				sv.Error = err.Error()
			} else {
				sv.Patch = &p
			}
		}
		v.Stages = append(v.Stages, sv)
	}
	return v, nil
}

// SetKnobs はアナログのペダル label のつまみの位置を board.json に書く（ペダルには何も送らない）。
// knobs にあるつまみだけを変え、値が空のつまみは消す。
func SetKnobs(label string, knobs map[string]string) (StageView, error) {
	bd, err := board.Load()
	if err != nil {
		return StageView{}, err
	}
	s, i, err := bd.Find(label)
	if err != nil {
		return StageView{}, err
	}
	if s.Digital() {
		return StageView{}, fmt.Errorf("%s は zm で触れるペダル。set_param などで変える", s.Label())
	}
	if s.Knobs == nil {
		s.Knobs = map[string]string{}
	}
	for k, v := range knobs {
		if v == "" {
			delete(s.Knobs, k)
		} else {
			s.Knobs[k] = v
		}
	}
	if err := bd.Save(); err != nil {
		return StageView{}, err
	}
	return StageView{Position: i + 1, Label: s.Label(), Note: s.Note, Knobs: s.Knobs}, nil
}

// ---- シーン（ボード全体の音色） ----

type SceneResult struct {
	Name string `json:"name"`
	Path string `json:"path"`
	board.Scene
}

// SaveScene は各ペダルのいま鳴っているパッチと、アナログのつまみをシーン name としてファイルに書く。
// ペダルのパッチメモリには書かない。1 台でも読めなければ書かない。
func SaveScene(name, note string, overwrite bool) (SceneResult, error) {
	if _, err := board.ScenePath(name); err != nil {
		return SceneResult{}, err
	}
	bd, err := board.Load()
	if err != nil {
		return SceneResult{}, err
	}
	sc := board.Scene{Name: name, Note: note, SavedAt: time.Now().Truncate(time.Second), Stages: []board.SceneStage{}}
	for _, s := range bd.Chain {
		st := board.SceneStage{Pedal: s.Pedal, Name: s.Name}
		if s.Digital() {
			type read struct {
				msg []byte
				p   PatchView
			}
			r, err := WithPedal(s.Pedal, false, func(z *device.Zoom) (read, error) {
				msg, err := z.CurrentSysex()
				if err != nil {
					return read{}, err
				}
				p, err := DescribeSysex(z.Model, msg)
				return read{msg, p}, err
			})
			if err != nil {
				return SceneResult{}, fmt.Errorf("%s: %w", s.Label(), err)
			}
			st.Sysex = hex.EncodeToString(r.msg)
			st.Patch = &r.p.NamedPatch
		} else {
			st.Knobs = maps.Clone(s.Knobs)
		}
		sc.Stages = append(sc.Stages, st)
	}
	path, err := board.SaveScene(&sc, overwrite)
	if err != nil {
		return SceneResult{}, err
	}
	return SceneResult{Name: name, Path: path, Scene: sc}, nil
}

// DescribeSysex は現在パッチの SysEx を名前つきで読む。
func DescribeSysex(m device.Model, msg []byte) (PatchView, error) {
	cat, err := CatalogFor(m)
	if err != nil {
		return PatchView{}, err
	}
	p, err := device.DecodeSysex(msg)
	if err != nil {
		return PatchView{}, err
	}
	return PatchView{Number: "current", NamedPatch: cat.Describe(p)}, nil
}

func GetScene(name string) (*board.Scene, error) { return board.LoadScene(name) }

func ListScenes() ([]board.SceneEntry, error) { return board.ListScenes() }

// LoadedStage はシーンを呼び出した結果の 1 台。
type LoadedStage struct {
	Label string `json:"label"`
	// Loaded は現在パッチに流し込めたか（zm で触れるペダルだけ）。
	Loaded bool   `json:"loaded,omitempty"`
	Patch  string `json:"patch,omitempty"`
	// Knobs は人が回すつまみ（アナログのペダルだけ）。board.json のいまの位置もこれにする。
	Knobs map[string]string `json:"knobs,omitempty"`
	Error string            `json:"error,omitempty"`
}

type LoadResult struct {
	Name   string        `json:"name"`
	Note   string        `json:"note,omitempty"`
	Stages []LoadedStage `json:"stages"`
}

// LoadScene はシーン name を各ペダルの現在パッチに流し込む（保存しない）。
// 繋がっていないペダルは Error に理由を入れ、ほかの台は続ける。どれか失敗すればエラーも返す。
func LoadScene(name string) (LoadResult, error) {
	sc, err := board.LoadScene(name)
	if err != nil {
		return LoadResult{}, err
	}
	bd, err := board.Load()
	if err != nil {
		return LoadResult{}, err
	}
	res := LoadResult{Name: name, Note: sc.Note, Stages: []LoadedStage{}}
	var errs []error
	knobsChanged := false
	for _, st := range sc.Stages {
		ls := LoadedStage{Label: st.Label()}
		if st.Pedal != "" {
			if st.Patch != nil {
				ls.Patch = st.Patch.Name
			}
			err := loadStage(st)
			if err != nil {
				ls.Error = err.Error()
				errs = append(errs, fmt.Errorf("%s: %w", ls.Label, err))
			} else {
				ls.Loaded = true
			}
		} else {
			ls.Knobs = st.Knobs
			if s, _, err := bd.Find(st.Name); err == nil && !s.Digital() {
				s.Knobs = maps.Clone(st.Knobs)
				knobsChanged = true
			}
		}
		res.Stages = append(res.Stages, ls)
	}
	if knobsChanged {
		if err := bd.Save(); err != nil {
			errs = append(errs, err)
		}
	}
	return res, errors.Join(errs...)
}

func loadStage(st board.SceneStage) error {
	msg, err := hex.DecodeString(st.Sysex)
	if err != nil {
		return fmt.Errorf("sysex が読めない: %w", err)
	}
	_, err = WithPedal(st.Pedal, true, func(z *device.Zoom) (struct{}, error) { return struct{}{}, z.LoadPatch(msg) })
	return err
}
