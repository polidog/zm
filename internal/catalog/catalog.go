// Package catalog はエフェクトカタログ (L3)。ID → 名前 / パラメータ名 / 範囲 / 説明。
//
// catalog/<機種>.json（g2four.json / ms80irplus.json）は `zm catalog` でペダルの ZD2 から作る（ZOOM の著作物を含むので git には入れない）。
// 旧 MS（MS-50G / MS-70CDR）はペダルからエフェクトの定義を取り出せないので、g200kg/zoom-ms-utility（MIT）の
// エフェクト一覧から作った legacy/<機種>.json を埋め込む（作り方は legacy/gen.mjs）。
package catalog

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/polidog/zm/internal/patch"
	"github.com/polidog/zm/internal/paths"
)

type Param struct {
	Name          string `json:"name"`
	Max           int    `json:"max"`
	Default       int    `json:"default"`
	Explanation   string `json:"explanation"`
	ExplanationJa string `json:"explanationJa"`
	// Hidden は PRME に載っていない（ペダルの画面に出ない）パラメータ。
	Hidden bool `json:"hidden"`
}

type Effect struct {
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	Group         int     `json:"group"`
	GroupName     string  `json:"groupName"`
	Version       string  `json:"version"`
	Description   string  `json:"description"`
	DescriptionJa string  `json:"descriptionJa"`
	Params        []Param `json:"params"`
	File          string  `json:"file"`
}

// VisibleParams は画面に出るパラメータ（Hidden を除く）。添字はパッチの params と同じ並び。
func (e *Effect) VisibleParams() []Param {
	var out []Param
	for _, p := range e.Params {
		if !p.Hidden {
			out = append(out, p)
		}
	}
	return out
}

type Catalog struct {
	Device      string   `json:"device"`
	GeneratedAt string   `json:"generatedAt"`
	Effects     []Effect `json:"effects"`
}

// Path は機種 key（device.Model.Key）のカタログの置き場。
func Path(key string) string { return filepath.Join(paths.Catalog(), key+".json") }

//go:embed legacy/*.json
var legacyFS embed.FS

var (
	legacyMu     sync.Mutex
	legacyLoaded = map[string]*Catalog{}
)

// Legacy は埋め込みの旧 MS のカタログ。key は device.Model.Key（ms50g / ms70cdr）。
func Legacy(key string) (*Catalog, error) {
	legacyMu.Lock()
	defer legacyMu.Unlock()
	if c, ok := legacyLoaded[key]; ok {
		return c, nil
	}
	b, err := legacyFS.ReadFile("legacy/" + key + ".json")
	if err != nil {
		return nil, fmt.Errorf("%s のカタログは埋め込んでいない", key)
	}
	var c Catalog
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("%s.json: %w", key, err)
	}
	legacyLoaded[key] = &c
	return &c, nil
}

// ZD2Dir はペダルから取った ZD2 のキャッシュ。G2 FOUR は前からの置き場の catalog/zd2/、ほかは catalog/zd2-<機種>/。
func ZD2Dir(key string) string {
	if key == "g2four" {
		return filepath.Join(paths.Catalog(), "zd2")
	}
	return filepath.Join(paths.Catalog(), "zd2-"+key)
}

// SystemEffects は ZD2 を持たない、パッチの中だけに現れる ID。
var SystemEffects = map[string]string{
	"0x00000000": "(empty)",
	"0x0b000010": "Output VP",
	"0x1c000010": "(system)",
}

func FormatID(id uint32) string { return fmt.Sprintf("0x%08x", id) }

func Load(path string) (*Catalog, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("%s がない。zm catalog で作る", path)
	}
	if err != nil {
		return nil, err
	}
	var c Catalog
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &c, nil
}

func (c *Catalog) Find(id uint32) *Effect {
	key := FormatID(id)
	for i := range c.Effects {
		if c.Effects[i].ID == key {
			return &c.Effects[i]
		}
	}
	return nil
}

func (c *Catalog) Groups() []string {
	var out []string
	seen := map[string]bool{}
	for _, e := range c.Effects {
		if !seen[e.GroupName] {
			seen[e.GroupName] = true
			out = append(out, e.GroupName)
		}
	}
	return out
}

// Resolve はエフェクトを名前か ID で引く。大文字小文字と空白・ハイフンの違いは問わない。
func (c *Catalog) Resolve(query string) (*Effect, error) {
	q := strings.ToLower(strings.TrimSpace(query))
	squash := strings.NewReplacer(" ", "", "\t", "", "-", "")
	for i, e := range c.Effects {
		if e.ID == q || strings.ToLower(e.Name) == q {
			return &c.Effects[i], nil
		}
	}
	for i, e := range c.Effects {
		if squash.Replace(strings.ToLower(e.Name)) == squash.Replace(q) {
			return &c.Effects[i], nil
		}
	}
	var near []string
	for _, e := range c.Effects {
		if strings.Contains(strings.ToLower(e.Name), q) {
			near = append(near, e.Name)
		}
	}
	if len(near) > 0 {
		return nil, fmt.Errorf("エフェクト %q がカタログにない。候補: %s", query, strings.Join(near, ", "))
	}
	return nil, fmt.Errorf("エフェクト %q がカタログにない。list_effects で探す", query)
}

type NamedParam struct {
	Name   string `json:"name"`
	Value  int    `json:"value"`
	Max    int    `json:"max"`
	Hidden bool   `json:"hidden,omitempty"`
}

type NamedSlot struct {
	// Slot は 0 始まり（TypeScript 版の describePatch と同じ）。
	Slot    int          `json:"slot"`
	ID      string       `json:"id"`
	Effect  string       `json:"effect"`
	Group   *string      `json:"group,omitempty"`
	Enabled bool         `json:"enabled"`
	Params  []NamedParam `json:"params"`
}

type NamedPatch struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Volume      int         `json:"volume"`
	Tempo       int         `json:"tempo"`
	Slots       []NamedSlot `json:"slots"`
}

// Describe はパッチのエフェクトをカタログの名前で読めるようにする。空きスロットは除く。
func (c *Catalog) Describe(p *patch.Patch) NamedPatch {
	d := NamedPatch{
		Name:        p.Name,
		Description: p.DescriptionJa,
		Volume:      p.Prm2.PatchVolume,
		Tempo:       p.Prm2.Tempo,
		Slots:       []NamedSlot{},
	}
	if d.Description == "" {
		d.Description = p.DescriptionEn
	}
	for slot, fx := range p.Effects {
		if fx.ID == 0 {
			continue
		}
		id := FormatID(fx.ID)
		s := NamedSlot{Slot: slot, ID: id, Effect: "(unknown)", Enabled: fx.Enabled, Params: []NamedParam{}}
		if name, ok := SystemEffects[id]; ok {
			s.Effect = name
		}
		if e := c.Find(fx.ID); e != nil {
			s.Effect = e.Name
			g := e.GroupName
			s.Group = &g
			for i, p := range e.Params {
				v := 0
				if i < len(fx.Params) {
					v = fx.Params[i]
				}
				s.Params = append(s.Params, NamedParam{Name: p.Name, Value: v, Max: p.Max, Hidden: p.Hidden})
			}
		}
		d.Slots = append(d.Slots, s)
	}
	return d
}
