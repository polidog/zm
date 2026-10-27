// カタログ (catalog/g2four.json) を実機パッチと突き合わせる。
//
// catalog/ と backups/ は git に入れていないので、無ければスキップする（zm catalog / zm backup で作る）。
package catalog_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/polidog/zoom-ai/internal/catalog"
	"github.com/polidog/zoom-ai/internal/device"
	"github.com/polidog/zoom-ai/internal/patch"
	"github.com/polidog/zoom-ai/internal/testdata"
)

func load(t *testing.T) (*catalog.Catalog, []*patch.Patch) {
	t.Helper()
	path := filepath.Join(testdata.Catalog(), "g2four.json")
	if _, err := os.Stat(path); err != nil {
		t.Skip("catalog/g2four.json がない（zm catalog で作る）")
	}
	c, err := catalog.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	_, files := testdata.LatestAll(t)
	var patches []*patch.Patch
	for _, f := range files {
		raw, _ := os.ReadFile(f)
		p, err := device.DecodeSysex(raw)
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		patches = append(patches, p)
	}
	return c, patches
}

func TestIDsAreKnown(t *testing.T) {
	c, patches := load(t)
	unknown := map[string]bool{}
	for _, p := range patches {
		for _, e := range p.Effects {
			id := catalog.FormatID(e.ID)
			if _, ok := catalog.SystemEffects[id]; !ok && c.Find(e.ID) == nil {
				unknown[id] = true
			}
		}
	}
	if len(unknown) > 0 {
		t.Errorf("カタログにない ID: %v", unknown)
	}
}

func TestParamsWithinMax(t *testing.T) {
	c, patches := load(t)
	var bad []string
	for _, p := range patches {
		for _, fx := range p.Effects {
			e := c.Find(fx.ID)
			if e == nil {
				continue
			}
			for i, v := range fx.Params {
				max := 0
				if i < len(e.Params) {
					max = e.Params[i].Max
				}
				if v > max {
					bad = append(bad, fmt.Sprintf("%s / %s param%d=%d > %d", p.Name, e.Name, i+1, v, max))
				}
			}
		}
	}
	if len(bad) > 0 {
		t.Error(bad)
	}
}

func TestDefaultsWithinMax(t *testing.T) {
	c, _ := load(t)
	for _, e := range c.Effects {
		for _, p := range e.Params {
			if p.Default > p.Max {
				t.Errorf("%s.%s", e.Name, p.Name)
			}
		}
	}
}

func TestDescribe(t *testing.T) {
	c, patches := load(t)
	d := c.Describe(patches[0])
	for _, s := range d.Slots {
		if s.Effect == "(unknown)" {
			t.Errorf("スロット %d が引けない", s.Slot)
		}
	}
	if len(d.Slots) == 0 || len(d.Slots[0].Params) == 0 {
		t.Error("パラメータがない")
	}
}

// ZD2 のキャッシュから作り直したカタログが、今の g2four.json と（generatedAt を除いて）一致する。
func TestBuildMatchesCatalog(t *testing.T) {
	c, _ := load(t)
	dir := filepath.Join(testdata.Catalog(), "zd2")
	if _, err := os.Stat(dir); err != nil {
		t.Skip("catalog/zd2 がない")
	}
	built, err := catalog.Build(dir, time.Now(), func(file string, err error) { t.Logf("%s: %v", file, err) })
	if err != nil {
		t.Fatal(err)
	}
	built.GeneratedAt = c.GeneratedAt
	a, _ := json.Marshal(c)
	b, _ := json.Marshal(built)
	if !slices.Equal(a, b) {
		t.Error("作り直したカタログが一致しない")
	}
}

// 埋め込みの旧 MS のカタログ（legacy/<機種>.json）。
func TestLegacy(t *testing.T) {
	for _, m := range []device.Model{device.MS50G, device.MS70CDR} {
		t.Run(m.Key, func(t *testing.T) { testLegacy(t, m) })
	}
}

func testLegacy(t *testing.T, m device.Model) {
	c, err := catalog.Legacy(m.Key)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Effects) == 0 || c.Device != m.Name {
		t.Fatalf("%s: %d エフェクト", c.Device, len(c.Effects))
	}
	for _, e := range c.Effects {
		if len(e.Params) > 9 {
			t.Errorf("%s: パラメータが %d 個（旧 MS は 9 個まで）", e.Name, len(e.Params))
		}
	}

	// 実機の全パッチのエフェクトがカタログにあり、値が範囲に収まるか（backups/<機種> が無ければスキップ）
	_, files := testdata.LatestAllIn(t, m.BackupDir)
	for _, f := range files {
		raw, _ := os.ReadFile(f)
		p, err := device.DecodeSysex(raw)
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		for slot, fx := range p.Effects {
			if fx.ID == 0 {
				continue
			}
			e := c.Find(fx.ID)
			if e == nil {
				t.Errorf("%s slot %d: カタログにない ID %s", filepath.Base(f), slot+1, catalog.FormatID(fx.ID))
				continue
			}
			for i, prm := range e.Params {
				if fx.Params[i] > prm.Max {
					t.Errorf("%s slot %d %s %s: %d > %d", filepath.Base(f), slot+1, e.Name, prm.Name, fx.Params[i], prm.Max)
				}
			}
		}
	}
}

func TestLegacyFD(t *testing.T) {
	c, _ := catalog.Legacy(device.MS50G.Key)
	if e, err := c.Resolve("fd combo"); err != nil || e.ID != "0x00100008" {
		t.Errorf("FD COMBO: %v %v", e, err)
	}
	if _, err := catalog.Legacy("ms60b"); err == nil {
		t.Error("ms60b のカタログは無いはず")
	}
}
