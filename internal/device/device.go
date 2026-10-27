// Package device はペダル 1 台分の操作をまとめる。CLI・TUI・MCP サーバーはこの上に乗る。
//
// 機種の違いは protocol に閉じ込める。いまあるのは 2 つ。
//   - four.go:   G2 FOUR（デバイス ID 6E）。45 / 46 でパッチメモリを直接読み書き、64 20 で編集
//   - legacy.go: 旧 MS-50G（デバイス ID 58）。Program Change で選んで 29 / 28、31 で編集、32 で保存
//
// どちらもパッチは *patch.Patch（Name・Effects）として返す。ops から上は機種を気にしない。
// 番号はすべて 0 始まり。画面の 1 始まりとの変換は ops でやる。
// パッチ数やスロット数は機種 (Model) が持ち、決め打ちにしない。
package device

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/polidog/zoom-ai/internal/codec"
	"github.com/polidog/zoom-ai/internal/midi"
	"github.com/polidog/zoom-ai/internal/patch"
	"github.com/polidog/zoom-ai/internal/sysex"
)

// Model は機種ごとに違う値。
type Model struct {
	Name string
	// Key は機種の短い名前。--pedal で選ぶときと、カタログ・バックアップの置き場に使う。
	Key      string
	DeviceID byte
	// Patches はパッチメモリの数、PatchesPerBank は 1 バンクの数。
	Patches        int
	PatchesPerBank int
	// UserSlots はユーザーが使えるスロット数（G2 FOUR の 7, 8 番目は Output VP とシステム用）。
	UserSlots int
	// BackupDir は backups/ の下の置き場。空なら backups/ の直下（G2 FOUR の既存の置き場）。
	BackupDir string
	// Legacy は旧 MS（Program Change + 29/28 でやりとりし、カタログを埋め込む）か。
	Legacy bool
	// ReadOnly は編集と保存をまだ実機で確かめていない機種。Open(true) を断る。
	ReadOnly bool
}

// G2Four: バンク 30 × 10。
var G2Four = Model{Name: "ZOOM G2 FOUR", Key: "g2four", DeviceID: 0x6e, Patches: 300, PatchesPerBank: 10, UserSlots: 6}

// MS50G: 旧 MS-50G。パッチ 50、エフェクト 6 個まで。
var MS50G = Model{Name: "ZOOM MS-50G", Key: "ms50g", DeviceID: sysex.LegacyDeviceID, Patches: 50, PatchesPerBank: 50, UserSlots: 6, BackupDir: "ms50g", Legacy: true}

// MS70CDR: 旧 MS-70CDR。パッチ形式とメッセージは MS-50G と同じ（midimessage.md）。
// MS-50G で見つけた癖（51 で編集が消える、切り替えで AUTO SAVE、31 はスロット 1〜3 だけ）も同じだった（ファームウェア 2.10）。
var MS70CDR = Model{Name: "ZOOM MS-70CDR", Key: "ms70cdr", DeviceID: sysex.MS70CDRDeviceID, Patches: 50, PatchesPerBank: 50, UserSlots: 6, BackupDir: "ms70cdr", Legacy: true}

// Models は対応している機種。Identity Reply のデバイス ID で選ぶ。
var Models = []Model{G2Four, MS50G, MS70CDR}

// ModelByID はデバイス ID で機種を引く。
func ModelByID(dev byte) (Model, bool) { return modelFor(dev) }

// ModelOfSysex はパッチの SysEx の 4 バイト目（デバイス ID）から機種を引く。知らなければ G2 FOUR。
func ModelOfSysex(msg []byte) Model {
	if len(msg) > 3 {
		if m, ok := modelFor(msg[3]); ok {
			return m
		}
	}
	return G2Four
}

func modelFor(dev byte) (Model, bool) {
	for _, m := range Models {
		if m.DeviceID == dev {
			return m, true
		}
	}
	return Model{}, false
}

// ModelByKey は Key（g2four / ms50g / ms70cdr）か Name で機種を引く。大文字小文字と空白・ハイフンは問わない。
func ModelByKey(s string) (Model, bool) {
	squash := strings.NewReplacer(" ", "", "-", "", "zoom", "")
	q := squash.Replace(strings.ToLower(s))
	for _, m := range Models {
		if q == m.Key || q == squash.Replace(strings.ToLower(m.Name)) {
			return m, true
		}
	}
	return Model{}, false
}

func (m Model) protocol() protocol {
	if m.Legacy {
		return legacy{}
	}
	return four{}
}

// protocol は機種ごとのやりとり。Zoom の同名のメソッドから呼ぶ。
type protocol interface {
	// open は Identity の後に呼ぶ。edit なら書き込めるようにする。
	open(z *Zoom, edit bool) error
	close(z *Zoom)
	patchSysex(z *Zoom, n int) ([]byte, error)
	currentPatch(z *Zoom) (*patch.Patch, error)
	keepCurrent(z *Zoom, fn func() error) error
	setParam(z *Zoom, slot, index, value int) error
	setEnabled(z *Zoom, slot int, on bool) error
	setEffect(z *Zoom, slot int, id uint32) (*patch.Patch, error)
	setName(z *Zoom, name string) error
	savePatch(z *Zoom, n int, p *patch.Patch) error
	writePatchSysex(z *Zoom, n int, msg []byte) error
	notice(z *Zoom, msg []byte) (*patch.Patch, bool)
}

// zoomReply は 5 バイト目以降（F0 52 00 dev の後ろ）が prefix で始まるか。
func zoomReply(prefix ...byte) func([]byte) bool {
	return func(msg []byte) bool { return len(msg) > 4+len(prefix) && bytes.HasPrefix(msg[4:], prefix) }
}

// Location は 0 始まりの通し番号 → バンク / プログラム。
func (m Model) Location(n int) (codec.Location, error) {
	if n < 0 || n >= m.Patches {
		return codec.Location{}, fmt.Errorf("パッチ番号は 0..%d", m.Patches-1)
	}
	return codec.Location{Bank: n / m.PatchesPerBank, Program: n % m.PatchesPerBank}, nil
}

type Zoom struct {
	Conn     *midi.Conn
	Identity sysex.Identity
	Model    Model
	Editable bool

	proto protocol
	// legacy は旧 MS の接続ごとの状態（legacy.go）。
	legacy *legacyState
}

// Target はどのペダルを使うか。空なら名前に ZOOM を含むポートを順に当たり、最初に対応している機種を使う。
// hw:C,D ならそのポート、g2four / ms50g / ms70cdr などの機種名ならその機種のペダル。CLI の --pedal と $ZM_PEDAL で決まる。
var Target string

// Open は Target のペダルを開く。edit なら書き込めるようにする（G2 FOUR はエディタモード ON と AUTO SAVE OFF）。
// 使い終わったら必ず Close する（開いたまま抜けると以降のオープンが EBUSY になる）。
func Open(edit bool) (*Zoom, error) {
	if strings.HasPrefix(Target, "hw:") {
		p, err := midi.ParsePort(Target)
		if err != nil {
			return nil, err
		}
		return OpenPort(p, edit)
	}
	var want *Model
	if Target != "" {
		m, ok := ModelByKey(Target)
		if !ok {
			return nil, fmt.Errorf("--pedal %q は hw:C,D か機種名（%s）", Target, modelKeys())
		}
		want = &m
	}
	ports, err := midi.ListPorts()
	if err != nil {
		return nil, err
	}
	var errs []error
	for _, p := range ports {
		if !midi.IsZoom(p) {
			continue
		}
		z, err := openPort(p, edit, want)
		if err == nil {
			return z, nil
		}
		errs = append(errs, fmt.Errorf("hw:%d,%d %s: %w", p.Card, p.Device, p.Name, err))
	}
	if len(errs) == 0 {
		return nil, errors.New("ZOOM のポートが見つからない")
	}
	return nil, errors.Join(errs...)
}

func modelKeys() string {
	var keys []string
	for _, m := range Models {
		keys = append(keys, m.Key)
	}
	return strings.Join(keys, " / ")
}

// OpenPort はポートを指定して開く。
func OpenPort(port midi.Port, edit bool) (*Zoom, error) { return openPort(port, edit, nil) }

func openPort(port midi.Port, edit bool, want *Model) (*Zoom, error) {
	conn, err := midi.Open(port)
	if err != nil {
		return nil, err
	}
	z, err := open(conn, edit, want)
	if err != nil {
		conn.Close()
		return nil, err
	}
	return z, nil
}

func identify(conn *midi.Conn) (sysex.Identity, Model, error) {
	reply, err := conn.Request(sysex.IdentityRequest, 2*time.Second, sysex.IsIdentityReply)
	if err != nil {
		return sysex.Identity{}, Model{}, err
	}
	if reply == nil {
		return sysex.Identity{}, Model{}, errors.New("Identity Request に応答がない")
	}
	ident, err := sysex.ParseIdentityReply(reply)
	if err != nil {
		return sysex.Identity{}, Model{}, err
	}
	model, ok := modelFor(ident.DeviceID)
	if !ok {
		return ident, Model{}, fmt.Errorf("対応していない機種（デバイス ID %02X, ファームウェア %s）", ident.DeviceID, ident.Version)
	}
	return ident, model, nil
}

func open(conn *midi.Conn, edit bool, want *Model) (*Zoom, error) {
	ident, model, err := identify(conn)
	if err != nil {
		return nil, err
	}
	if want != nil && model.Key != want.Key {
		return nil, fmt.Errorf("%s ではない（%s）", want.Name, model.Name)
	}
	if edit && model.ReadOnly {
		return nil, fmt.Errorf("%s の編集と保存は、実機で確かめるまで止めてある（読み出しはできる）", model.Name)
	}
	z := &Zoom{Conn: conn, Identity: ident, Model: model, proto: model.protocol()}
	if err := z.proto.open(z, edit); err != nil {
		return nil, err
	}
	return z, nil
}

// TargetModel はペダルを開かずに分かる範囲で、Target の機種を返す（Target が機種名のときだけ ok）。
func TargetModel() (Model, bool) {
	if Target == "" || strings.HasPrefix(Target, "hw:") {
		return Model{}, false
	}
	return ModelByKey(Target)
}

// Identify は Target のペダルの機種を、Identity Request だけで調べる（エディタモードにはしない）。
func Identify() (Model, error) {
	if m, ok := TargetModel(); ok {
		return m, nil
	}
	var ports []midi.Port
	if strings.HasPrefix(Target, "hw:") {
		p, err := midi.ParsePort(Target)
		if err != nil {
			return Model{}, err
		}
		ports = []midi.Port{p}
	} else {
		all, err := midi.ListPorts()
		if err != nil {
			return Model{}, err
		}
		for _, p := range all {
			if midi.IsZoom(p) {
				ports = append(ports, p)
			}
		}
	}
	var errs []error
	for _, p := range ports {
		conn, err := midi.Open(p)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		_, m, err := identify(conn)
		conn.Close()
		if err == nil {
			return m, nil
		}
		errs = append(errs, err)
	}
	if len(errs) == 0 {
		return Model{}, errors.New("ZOOM のポートが見つからない")
	}
	return Model{}, errors.Join(errs...)
}

func (z *Zoom) DeviceID() byte { return z.Identity.DeviceID }

// expect は送って、5 バイト目以降が prefix で始まる応答を待つ。
// 編集するとペダルは自分から通知を送ってくるので、それ以外の応答は読み飛ばす。
func (z *Zoom) expect(msg []byte, timeout time.Duration, prefix ...byte) ([]byte, error) {
	reply, err := z.Conn.Request(msg, timeout, zoomReply(prefix...))
	if err != nil {
		return nil, err
	}
	if reply == nil {
		return nil, fmt.Errorf("%s… が受理されない", sysex.ToHex(msg[:min(12, len(msg))]))
	}
	return reply, nil
}

func (z *Zoom) assertEditable() error {
	if !z.Editable {
		return errors.New("書き込みには device.Open(true) が要る")
	}
	return nil
}

// Close は編集用に開いたとき、エディタモード OFF の応答まで待ってから閉じる
// （残った応答が次の接続に混ざらないように）。
func (z *Zoom) Close() error {
	z.proto.close(z)
	z.Editable = false
	return z.Conn.Close()
}

// PatchSysex は生の SysEx を返す。バックアップ用（G2 FOUR は 45 応答、旧 MS は 28）。
// 旧 MS はパッチを選び直して読むので、いま鳴っているパッチ（編集中の中身も）は読んだ後に戻す。
func (z *Zoom) PatchSysex(n int) ([]byte, error) {
	if _, err := z.Model.Location(n); err != nil {
		return nil, err
	}
	return z.proto.patchSysex(z, n)
}

// KeepCurrent は fn の間にパッチを何件読んでも、いま鳴っているパッチを fn の後に 1 度だけ戻す。
// G2 FOUR は読み出しでパッチが変わらないので、そのまま fn を呼ぶ。
func (z *Zoom) KeepCurrent(fn func() error) error { return z.proto.keepCurrent(z, fn) }

// DecodeSysex はパッチの SysEx（G2 FOUR の 45 / 64 12、旧 MS の 28）を読む。
func DecodeSysex(msg []byte) (*patch.Patch, error) {
	if len(msg) > 4 {
		if m, ok := modelFor(msg[3]); ok && m.Legacy {
			return decodeLegacy(msg)
		}
	}
	_, buffer, err := codec.DecodePatchSysex(msg)
	if err != nil {
		return nil, err
	}
	return patch.Decode(buffer)
}

func (z *Zoom) Patch(n int) (*patch.Patch, error) {
	msg, err := z.PatchSysex(n)
	if err != nil {
		return nil, err
	}
	return DecodeSysex(msg)
}

func (z *Zoom) CurrentPatch() (*patch.Patch, error) { return z.proto.currentPatch(z) }

// Notice はペダルが自分から送ってきたメッセージが現在パッチの変化なら、変化後のパッチを返す。
// ペダルとはやりとりしない（受信の途中で呼ばれるので）。
func (z *Zoom) Notice(msg []byte) (*patch.Patch, bool) { return z.proto.notice(z, msg) }

type Entry struct {
	N    int    `json:"n"`
	Name string `json:"name"`
}

func (z *Zoom) ListPatches() ([]Entry, error) {
	out := make([]Entry, 0, z.Model.Patches)
	err := z.KeepCurrent(func() error {
		for n := range z.Model.Patches {
			p, err := z.Patch(n)
			if err != nil {
				return err
			}
			out = append(out, Entry{N: n, Name: p.Name})
		}
		return nil
	})
	return out, err
}

// ---- 書き込み（現在パッチのみ。保存しない） ----

func effectAt(p *patch.Patch, slot int) *patch.Effect {
	if slot < 0 || slot >= len(p.Effects) {
		return nil
	}
	return &p.Effects[slot]
}

// SetParam は param を変える。index は 0 始まり（Effects[slot].Params の添字）。
func (z *Zoom) SetParam(slot, index, value int) error {
	if err := z.assertEditable(); err != nil {
		return err
	}
	return z.proto.setParam(z, slot, index, value)
}

func (z *Zoom) SetEnabled(slot int, on bool) error {
	if err := z.assertEditable(); err != nil {
		return err
	}
	return z.proto.setEnabled(z, slot, on)
}

// SetEffect はエフェクトを差し替え、差し替え後の現在パッチを返す。
// パラメータは G2 FOUR なら既定値か同じ電源投入中に前回使った値、旧 MS ならカタログの既定値。
func (z *Zoom) SetEffect(slot int, id uint32) (*patch.Patch, error) {
	if err := z.assertEditable(); err != nil {
		return nil, err
	}
	return z.proto.setEffect(z, slot, id)
}

// SetName は現在パッチの名前を変える（保存しない）。いまは旧 MS だけ。
func (z *Zoom) SetName(name string) error {
	if err := z.assertEditable(); err != nil {
		return err
	}
	return z.proto.setName(z, name)
}

// ---- 保存（フラッシュに書く） ----

// SavePatch は p（いま鳴っているパッチ）をパッチメモリ n に保存する。送った後に読み直し、中身が一致しなければエラー。
// 呼ぶ側で、上書きされる中身を PatchSysex で退避しておくこと。
func (z *Zoom) SavePatch(n int, p *patch.Patch) error {
	if err := z.assertEditable(); err != nil {
		return err
	}
	if _, err := z.Model.Location(n); err != nil {
		return err
	}
	return z.proto.savePatch(z, n, p)
}

// WritePatchSysex は PatchSysex で取った SysEx をそのまま書き戻す（バックアップからの復元用）。
func (z *Zoom) WritePatchSysex(n int, msg []byte) error {
	if err := z.assertEditable(); err != nil {
		return err
	}
	if _, err := z.Model.Location(n); err != nil {
		return err
	}
	return z.proto.writePatchSysex(z, n, msg)
}
