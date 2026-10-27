// Package device はペダル 1 台分の操作をまとめる。CLI・TUI・MCP サーバーはこの上に乗る。
//
// 書き込みは 2 種類。どちらも Open(true) でエディタモードを ON、AUTO SAVE を OFF にしてから使う。
//   - 64 20 による現在パッチの個別編集（保存しない。パッチを選び直せば戻る）
//   - 45 によるパッチメモリへの保存（フラッシュに書く）
//
// 番号はすべて 0 始まり。画面の 1 始まりとの変換は ops でやる。
package device

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/polidog/zoom-ai/internal/codec"
	"github.com/polidog/zoom-ai/internal/midi"
	"github.com/polidog/zoom-ai/internal/patch"
	"github.com/polidog/zoom-ai/internal/sysex"
)

// G2 FOUR: バンク 30 × 10。
const (
	PatchesPerBank = 10
	PatchCount     = 300
)

// zoomReply は 5 バイト目以降（F0 52 00 dev の後ろ）が prefix で始まるか。
func zoomReply(prefix ...byte) func([]byte) bool {
	return func(msg []byte) bool { return len(msg) > 4+len(prefix) && bytes.HasPrefix(msg[4:], prefix) }
}

// IsPatchNotice はペダルが自分から送ってくる現在パッチの通知 (64 12 00) か。
var IsPatchNotice = zoomReply(0x64, 0x12, 0x00)

// Location は 0 始まりの通し番号 → バンク / プログラム。
func Location(n int) (codec.Location, error) {
	if n < 0 || n >= PatchCount {
		return codec.Location{}, fmt.Errorf("パッチ番号は 0..%d", PatchCount-1)
	}
	return codec.Location{Bank: n / PatchesPerBank, Program: n % PatchesPerBank}, nil
}

type Zoom struct {
	Conn     *midi.Conn
	Identity sysex.Identity
	Editable bool
}

// Open は ZOOM のポートを開く。edit ならエディタモードを ON、AUTO SAVE を OFF にする。
// 使い終わったら必ず Close する（開いたまま抜けると以降のオープンが EBUSY になる）。
func Open(edit bool) (*Zoom, error) {
	port, ok := midi.FindZoomPort()
	if !ok {
		return nil, errors.New("ZOOM のポートが見つからない")
	}
	conn, err := midi.Open(port)
	if err != nil {
		return nil, err
	}
	z, err := open(conn, edit)
	if err != nil {
		conn.Close()
		return nil, err
	}
	return z, nil
}

func open(conn *midi.Conn, edit bool) (*Zoom, error) {
	reply, err := conn.Request(sysex.IdentityRequest, 2*time.Second, sysex.IsIdentityReply)
	if err != nil {
		return nil, err
	}
	if reply == nil {
		return nil, errors.New("Identity Request に応答がない")
	}
	ident, err := sysex.ParseIdentityReply(reply)
	if err != nil {
		return nil, err
	}
	z := &Zoom{Conn: conn, Identity: ident}
	if edit {
		if _, err := z.expect(sysex.EditorModeOn(z.DeviceID()), 2*time.Second, 0x00, 0x00); err != nil {
			return nil, err
		}
		z.Editable = true
		if _, err := z.expect(sysex.SetAutoSave(z.DeviceID(), false), 2*time.Second, 0x64, 0x20, 0x01); err != nil {
			z.Close()
			return nil, err
		}
	}
	return z, nil
}

func (z *Zoom) DeviceID() byte { return z.Identity.DeviceID }

// expect は送って、5 バイト目以降が prefix で始まる応答を待つ。
// 編集するとペダルは 64 12 00 (現在パッチの通知) を自分から送ってくるので、それ以外の応答は読み飛ばす。
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
	if z.Editable {
		z.expect(sysex.EditorModeOff(z.DeviceID()), 2*time.Second, 0x00, 0x00)
		z.Editable = false
	}
	return z.Conn.Close()
}

// PatchSysex は生の SysEx（45 応答）を返す。バックアップ用。
func (z *Zoom) PatchSysex(n int) ([]byte, error) {
	loc, err := Location(n)
	if err != nil {
		return nil, err
	}
	reply, err := z.Conn.Request(sysex.RequestPatch(z.DeviceID(), loc.Bank, loc.Program), 3*time.Second, zoomReply(0x45))
	if err != nil {
		return nil, err
	}
	if reply == nil {
		return nil, fmt.Errorf("パッチ %d を読めない", n)
	}
	return reply, nil
}

func DecodeSysex(msg []byte) (*patch.Patch, error) {
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

func (z *Zoom) CurrentPatch() (*patch.Patch, error) {
	// 64 12 00 は編集時の通知。要求への応答は 64 12 01
	reply, err := z.Conn.Request(sysex.RequestCurrentPatch(z.DeviceID()), 4*time.Second, zoomReply(0x64, 0x12, 0x01))
	if err != nil {
		return nil, err
	}
	if reply == nil {
		return nil, errors.New("現在パッチのダンプに応答がない")
	}
	return DecodeSysex(reply)
}

type Entry struct {
	N    int    `json:"n"`
	Name string `json:"name"`
}

func (z *Zoom) ListPatches() ([]Entry, error) {
	out := make([]Entry, 0, PatchCount)
	for n := range PatchCount {
		p, err := z.Patch(n)
		if err != nil {
			return nil, err
		}
		out = append(out, Entry{N: n, Name: p.Name})
	}
	return out, nil
}

// ---- 書き込み（現在パッチのみ。保存しない） ----

// edit は 64 20 00 で 1 項目変える。値が変わらないときペダルは何も返さないので、
// 受理が来なければ現在パッチを読み直し、値が合っていれば成功とする。
func (z *Zoom) edit(slot, paramNo int, value uint32, applied func(*patch.Patch) bool) error {
	if err := z.assertEditable(); err != nil {
		return err
	}
	_, err := z.expect(sysex.SetParameter(z.DeviceID(), slot, paramNo, value), time.Second, 0x64, 0x20, 0x01)
	if err == nil {
		return nil
	}
	p, err2 := z.CurrentPatch()
	if err2 == nil && applied(p) {
		return nil
	}
	return err
}

func effectAt(p *patch.Patch, slot int) *patch.Effect {
	if slot < 0 || slot >= len(p.Effects) {
		return nil
	}
	return &p.Effects[slot]
}

// SetParam は param を変える。index は 0 始まり（Effects[slot].Params の添字）。
func (z *Zoom) SetParam(slot, index, value int) error {
	return z.edit(slot, index+2, uint32(value), func(p *patch.Patch) bool {
		e := effectAt(p, slot)
		return e != nil && index < len(e.Params) && e.Params[index] == value
	})
}

func (z *Zoom) SetEnabled(slot int, on bool) error {
	v := uint32(0)
	if on {
		v = 1
	}
	return z.edit(slot, 0, v, func(p *patch.Patch) bool {
		e := effectAt(p, slot)
		return e != nil && e.Enabled == on
	})
}

// SetEffect はエフェクトを差し替える。応答は 64 20 01 ではなく 64 12 00（差し替え後の現在パッチ全体）なので、それを返す。
// パラメータは初めて使うエフェクトなら既定値、同じ電源投入中に使ったことがあればそのときの値になる。
func (z *Zoom) SetEffect(slot int, id uint32) (*patch.Patch, error) {
	if err := z.assertEditable(); err != nil {
		return nil, err
	}
	reply, err := z.expect(sysex.SetParameter(z.DeviceID(), slot, 1, id), 4*time.Second, 0x64, 0x12, 0x00)
	if err != nil {
		return nil, err
	}
	return DecodeSysex(reply)
}

// ---- 保存（フラッシュに書く） ----

// SavePatch はパッチメモリ n に保存する。送った後に読み直し、中身が一致しなければエラー。
// 呼ぶ側で、上書きされる中身を PatchSysex で退避しておくこと。
func (z *Zoom) SavePatch(n int, p *patch.Patch) error {
	if err := z.assertEditable(); err != nil {
		return err
	}
	loc, err := Location(n)
	if err != nil {
		return err
	}
	buffer, err := patch.Encode(p)
	if err != nil {
		return err
	}
	if err := z.WritePatchSysex(n, codec.EncodePatchSysex(z.DeviceID(), loc, buffer)); err != nil {
		return err
	}
	back, err := z.Patch(n)
	if err != nil {
		return err
	}
	body := func(p *patch.Patch) string {
		c := p.Clone()
		c.Trailer = ""
		b, _ := json.Marshal(c)
		return string(b)
	}
	if body(back) != body(p) {
		return fmt.Errorf("パッチ %d に保存した中身が読み直しと一致しない", n)
	}
	return nil
}

// WritePatchSysex は PatchSysex で取った 45 をそのまま書き戻す（バックアップからの復元用）。
func (z *Zoom) WritePatchSysex(n int, msg []byte) error {
	if err := z.assertEditable(); err != nil {
		return err
	}
	want, err := Location(n)
	if err != nil {
		return err
	}
	at, _, err := codec.DecodePatchSysex(msg)
	if err != nil {
		return err
	}
	if at != want {
		return fmt.Errorf("パッチ %d 宛ての SysEx ではない", n)
	}
	_, err = z.expect(msg, 5*time.Second, 0x00, 0x00)
	return err
}
