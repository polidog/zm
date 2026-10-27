// Package ops は CLI・TUI・MCP が共有する操作。
//
// 番号はペダルの画面に合わせる: パッチは 1〜パッチ数（G2 FOUR は 300、旧 MS は 50）、スロットは 1〜6。0 始まりへの変換と範囲の検証はここでやる。
// SetEffect などの編集は「いま鳴っているパッチ」への変更で、保存しない（パッチを選び直せば元に戻る）。
// 保存は Save だけで、上書きされる中身を backups/overwritten/ に退避してから書く。
//
// 返す値は MCP の応答にそのまま使うので、JSON の形は TypeScript 版の server.ts と同じにしてある。
package ops

import (
	"fmt"
	"strings"
	"sync"

	"github.com/polidog/zoom-ai/internal/backups"
	"github.com/polidog/zoom-ai/internal/catalog"
	"github.com/polidog/zoom-ai/internal/device"
)

var (
	catMu  sync.Mutex
	loaded *catalog.Catalog
)

// CatalogFor は機種 m のカタログ。G2 FOUR は catalog/g2four.json を読み（読めたら覚えておく）、旧 MS は埋め込みを使う。
func CatalogFor(m device.Model) (*catalog.Catalog, error) {
	if m.Legacy {
		return catalog.Legacy(m.Key)
	}
	catMu.Lock()
	defer catMu.Unlock()
	if loaded != nil {
		return loaded, nil
	}
	c, err := catalog.Load(catalog.Path())
	if err != nil {
		return nil, err
	}
	loaded = c
	return c, nil
}

// Model は使うペダルの機種。--pedal が機種名ならそれ、そうでなければ繋がっているペダルに聞く。
// ペダルが見つからない（ほかのプロセスが開いている、など）ときは G2 FOUR とみなす。
func Model() device.Model {
	if m, ok := device.TargetModel(); ok {
		return m
	}
	zoomMu.Lock()
	defer zoomMu.Unlock()
	if m, err := device.Identify(); err == nil {
		return m
	}
	return device.G2Four
}

// Catalog は使うペダルの機種のカタログ（ペダルを開かずに使う場面向け）。開いているなら CatalogFor(z.Model) を使う。
func Catalog() (*catalog.Catalog, error) { return CatalogFor(Model()) }

// ResetCatalog は覚えているカタログを捨てる（zm catalog で作り直した後）。
func ResetCatalog() {
	catMu.Lock()
	loaded = nil
	catMu.Unlock()
}

// PatchIndex は画面の番号（G2 FOUR なら 1〜300）を 0 始まりにする。
func PatchIndex(m device.Model, number int) (int, error) {
	if number < 1 || number > m.Patches {
		return 0, fmt.Errorf("パッチ番号は 1〜%d", m.Patches)
	}
	return number - 1, nil
}

// SlotIndex はスロット番号（G2 FOUR なら 1〜6）を 0 始まりにする。
func SlotIndex(m device.Model, slot int) (int, error) {
	if slot < 1 || slot > m.UserSlots {
		return 0, fmt.Errorf("スロット番号は 1〜%d", m.UserSlots)
	}
	return slot - 1, nil
}

var zoomMu sync.Mutex

// WithZoom はペダルを開いて fn を呼び、必ず閉じる。ペダルとのやりとりは 1 本の列に並べる。
// 呼び出しごとに開いて閉じるので、抜き差ししても動く。
func WithZoom[T any](edit bool, fn func(*device.Zoom) (T, error)) (T, error) {
	zoomMu.Lock()
	defer zoomMu.Unlock()
	var zero T
	z, err := device.Open(edit)
	if err != nil {
		return zero, err
	}
	defer z.Close()
	return fn(z)
}

// ---- 読み出し ----

type Identity struct {
	Device       string `json:"device"`
	Manufacturer string `json:"manufacturer"`
	Model        string `json:"model"`
	Firmware     string `json:"firmware"`
	Patches      int    `json:"patches"`
	UserSlots    int    `json:"userSlots"`
}

func Identify(z *device.Zoom) Identity {
	return Identity{
		Device:       z.Model.Name,
		Manufacturer: fmt.Sprintf("0x%x", z.Identity.Manufacturer),
		Model:        fmt.Sprintf("0x%x", z.Identity.Model),
		Firmware:     z.Identity.Version,
		Patches:      z.Model.Patches,
		UserSlots:    z.Model.UserSlots,
	}
}

type PatchName struct {
	Number int    `json:"number"`
	Name   string `json:"name"`
}

func ListPatches(z *device.Zoom) ([]PatchName, error) {
	list, err := z.ListPatches()
	if err != nil {
		return nil, err
	}
	out := make([]PatchName, len(list))
	for i, e := range list {
		out[i] = PatchName{Number: e.N + 1, Name: e.Name}
	}
	return out, nil
}

type PatchView struct {
	// Number は 1〜300 か "current"。
	Number any `json:"number"`
	catalog.NamedPatch
}

// GetPatch はパッチの中身を名前つきで返す。number が 0 なら、いま鳴っている（編集中の）パッチ。
func GetPatch(z *device.Zoom, number int) (PatchView, error) {
	cat, err := CatalogFor(z.Model)
	if err != nil {
		return PatchView{}, err
	}
	if number == 0 {
		p, err := z.CurrentPatch()
		if err != nil {
			return PatchView{}, err
		}
		return PatchView{Number: "current", NamedPatch: cat.Describe(p)}, nil
	}
	n, err := PatchIndex(z.Model, number)
	if err != nil {
		return PatchView{}, err
	}
	p, err := z.Patch(n)
	if err != nil {
		return PatchView{}, err
	}
	return PatchView{Number: number, NamedPatch: cat.Describe(p)}, nil
}

type BackupResult struct {
	Saved int    `json:"saved"`
	Dir   string `json:"dir"`
}

func BackupAll(z *device.Zoom, progress func(done int)) (BackupResult, error) {
	dir, err := backups.All(z, progress)
	return BackupResult{Saved: z.Model.Patches, Dir: dir}, err
}

// ---- カタログ ----

type EffectSummary struct {
	Name   string   `json:"name"`
	Group  string   `json:"group"`
	Params []string `json:"params"`
}

// ListEffects はエフェクトの一覧。group が空でなければそのグループだけ（大文字小文字は問わない）。
func ListEffects(group string) ([]EffectSummary, error) { return ListEffectsFor(Model(), group) }

func ListEffectsFor(m device.Model, group string) ([]EffectSummary, error) {
	cat, err := CatalogFor(m)
	if err != nil {
		return nil, err
	}
	out := []EffectSummary{}
	for _, e := range cat.Effects {
		if group != "" && !strings.EqualFold(e.GroupName, group) {
			continue
		}
		s := EffectSummary{Name: e.Name, Group: e.GroupName, Params: []string{}}
		for _, p := range e.VisibleParams() {
			s.Params = append(s.Params, fmt.Sprintf("%s 0-%d", p.Name, p.Max))
		}
		out = append(out, s)
	}
	return out, nil
}

type ParamDetail struct {
	Name        string `json:"name"`
	Max         int    `json:"max"`
	Default     int    `json:"default"`
	Explanation string `json:"explanation"`
}

type EffectDetail struct {
	Name        string        `json:"name"`
	ID          string        `json:"id"`
	Group       string        `json:"group"`
	Description string        `json:"description"`
	Params      []ParamDetail `json:"params"`
}

func orElse(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func DescribeEffect(query string) (EffectDetail, error) { return DescribeEffectFor(Model(), query) }

func DescribeEffectFor(m device.Model, query string) (EffectDetail, error) {
	cat, err := CatalogFor(m)
	if err != nil {
		return EffectDetail{}, err
	}
	e, err := cat.Resolve(query)
	if err != nil {
		return EffectDetail{}, err
	}
	return Detail(e), nil
}

// Detail はエフェクトの説明（日本語があれば日本語）と、画面に出るパラメータ。
func Detail(e *catalog.Effect) EffectDetail {
	d := EffectDetail{Name: e.Name, ID: e.ID, Group: e.GroupName, Description: orElse(e.DescriptionJa, e.Description), Params: []ParamDetail{}}
	for _, p := range e.VisibleParams() {
		d.Params = append(d.Params, ParamDetail{Name: p.Name, Max: p.Max, Default: p.Default, Explanation: orElse(p.ExplanationJa, p.Explanation)})
	}
	return d
}

// ---- 編集（いま鳴っているパッチ。保存しない） ----

// SlotView は編集後のスロット。Slot は 1 始まり。空なら {slot, effect: "(empty)"} だけ。
type SlotView struct {
	catalog.NamedSlot
	empty bool
}

func (s SlotView) MarshalJSON() ([]byte, error) {
	if s.empty {
		return CompactJSON(struct {
			Slot   int    `json:"slot"`
			Effect string `json:"effect"`
		}{s.Slot, s.Effect})
	}
	return CompactJSON(s.NamedSlot)
}

func slotView(z *device.Zoom, slot int) (SlotView, error) {
	cat, err := CatalogFor(z.Model)
	if err != nil {
		return SlotView{}, err
	}
	p, err := z.CurrentPatch()
	if err != nil {
		return SlotView{}, err
	}
	for _, s := range cat.Describe(p).Slots {
		if s.Slot == slot-1 {
			s.Slot = slot
			return SlotView{NamedSlot: s}, nil
		}
	}
	return SlotView{NamedSlot: catalog.NamedSlot{Slot: slot, Effect: "(empty)"}, empty: true}, nil
}

// SetEffect はスロットのエフェクトを差し替える。パラメータは既定値か、同じ電源投入中に前回そのエフェクトで使った値になる。
func SetEffect(z *device.Zoom, slot int, effect string) (SlotView, error) {
	i, err := SlotIndex(z.Model, slot)
	if err != nil {
		return SlotView{}, err
	}
	cat, err := CatalogFor(z.Model)
	if err != nil {
		return SlotView{}, err
	}
	e, err := cat.Resolve(effect)
	if err != nil {
		return SlotView{}, err
	}
	var id uint32
	if _, err := fmt.Sscanf(e.ID, "0x%x", &id); err != nil {
		return SlotView{}, fmt.Errorf("カタログの ID が読めない: %s", e.ID)
	}
	if _, err := z.SetEffect(i, id); err != nil {
		return SlotView{}, err
	}
	return slotView(z, slot)
}

// ResolveParam はスロットのエフェクトのパラメータを名前で引き、値の範囲を検証する。返すのは params の添字。
func ResolveParam(e *catalog.Effect, param string, value int) (int, error) {
	for i, p := range e.Params {
		if p.Hidden || !strings.EqualFold(p.Name, param) {
			continue
		}
		if value < 0 || value > p.Max {
			return 0, fmt.Errorf("%s の %s は 0〜%d", e.Name, p.Name, p.Max)
		}
		return i, nil
	}
	var names []string
	for _, p := range e.VisibleParams() {
		names = append(names, p.Name)
	}
	return 0, fmt.Errorf("%s にパラメータ %q はない。あるのは: %s", e.Name, param, strings.Join(names, ", "))
}

func slotEffect(z *device.Zoom, slot int) (*catalog.Effect, int, error) {
	i, err := SlotIndex(z.Model, slot)
	if err != nil {
		return nil, 0, err
	}
	cat, err := CatalogFor(z.Model)
	if err != nil {
		return nil, 0, err
	}
	p, err := z.CurrentPatch()
	if err != nil {
		return nil, 0, err
	}
	fx := p.Effects[i]
	e := cat.Find(fx.ID)
	if e == nil {
		return nil, 0, fmt.Errorf("スロット %d は空か、カタログにないエフェクト (%s)", slot, catalog.FormatID(fx.ID))
	}
	return e, i, nil
}

// SetParam はスロットのパラメータを名前で変える。値はカタログの範囲で検証する。
func SetParam(z *device.Zoom, slot int, param string, value int) (SlotView, error) {
	e, i, err := slotEffect(z, slot)
	if err != nil {
		return SlotView{}, err
	}
	index, err := ResolveParam(e, param, value)
	if err != nil {
		return SlotView{}, err
	}
	if err := z.SetParam(i, index, value); err != nil {
		return SlotView{}, err
	}
	return slotView(z, slot)
}

// Toggle はスロットの ON / OFF を変える。enabled が nil なら反転。
func Toggle(z *device.Zoom, slot int, enabled *bool) (SlotView, error) {
	i, err := SlotIndex(z.Model, slot)
	if err != nil {
		return SlotView{}, err
	}
	p, err := z.CurrentPatch()
	if err != nil {
		return SlotView{}, err
	}
	fx := p.Effects[i]
	if fx.ID == 0 {
		return SlotView{}, fmt.Errorf("スロット %d は空", slot)
	}
	on := !fx.Enabled
	if enabled != nil {
		on = *enabled
	}
	if err := z.SetEnabled(i, on); err != nil {
		return SlotView{}, err
	}
	return slotView(z, slot)
}

// RenameResult は名前を変えた後の現在パッチ。
type RenameResult struct {
	Name string `json:"name"`
}

// Rename はいま鳴っているパッチの名前を変える（保存しない）。旧 MS は 10 文字までの ASCII。
func Rename(z *device.Zoom, name string) (RenameResult, error) {
	if err := z.SetName(name); err != nil {
		return RenameResult{}, err
	}
	p, err := z.CurrentPatch()
	if err != nil {
		return RenameResult{}, err
	}
	return RenameResult{Name: p.Name}, nil
}

// ---- 保存（フラッシュに書く） ----

type SaveResult struct {
	Number      int    `json:"number"`
	Saved       string `json:"saved"`
	Overwritten string `json:"overwritten"`
	Backup      string `json:"backup"`
}

// Save はいま鳴っているパッチをパッチメモリ number に保存する。上書きされる中身は先に退避する。
// 旧 MS は保存の後、number を選んだ状態になる。
func Save(z *device.Zoom, number int) (SaveResult, error) {
	n, err := PatchIndex(z.Model, number)
	if err != nil {
		return SaveResult{}, err
	}
	before, err := z.Patch(n)
	if err != nil {
		return SaveResult{}, err
	}
	backup, err := backups.BeforeOverwrite(z, n)
	if err != nil {
		return SaveResult{}, err
	}
	p, err := z.CurrentPatch()
	if err != nil {
		return SaveResult{}, err
	}
	if err := z.SavePatch(n, p); err != nil {
		return SaveResult{}, err
	}
	return SaveResult{Number: number, Saved: p.Name, Overwritten: before.Name, Backup: backup}, nil
}

type RestoreResult struct {
	Number   int    `json:"number"`
	Restored string `json:"restored"`
	Replaced string `json:"replaced"`
	From     string `json:"from"`
}

// LatestBackup は Restore で使う退避のパス。無ければエラー。
func LatestBackup(m device.Model, number int) (string, []byte, error) {
	n, err := PatchIndex(m, number)
	if err != nil {
		return "", nil, err
	}
	path, data, err := backups.LatestOverwritten(m, n)
	if err != nil {
		return "", nil, err
	}
	if path == "" {
		return "", nil, fmt.Errorf("パッチ %d の退避がない", number)
	}
	return path, data, nil
}

// Restore は Save で上書きしたパッチ number を、上書き直前の中身に戻す（いちばん新しい退避を使う）。
func Restore(z *device.Zoom, number int) (RestoreResult, error) {
	n, _ := PatchIndex(z.Model, number)
	path, data, err := LatestBackup(z.Model, number)
	if err != nil {
		return RestoreResult{}, err
	}
	replaced, err := z.Patch(n)
	if err != nil {
		return RestoreResult{}, err
	}
	if err := z.WritePatchSysex(n, data); err != nil {
		return RestoreResult{}, err
	}
	restored, err := z.Patch(n)
	if err != nil {
		return RestoreResult{}, err
	}
	return RestoreResult{Number: number, Restored: restored.Name, Replaced: replaced.Name, From: path}, nil
}
