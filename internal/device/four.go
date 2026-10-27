package device

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/polidog/zm/internal/codec"
	"github.com/polidog/zm/internal/patch"
	"github.com/polidog/zm/internal/sysex"
)

// four は G2 FOUR（デバイス ID 6E）。
//
// 書き込みは 2 種類。どちらも Open(true) でエディタモードを ON、AUTO SAVE を OFF にしてから使う。
//   - 64 20 による現在パッチの個別編集（保存しない。パッチを選び直せば戻る）
//   - 45 によるパッチメモリへの保存（フラッシュに書く）
type four struct{}

// IsPatchNotice はペダルが自分から送ってくる現在パッチの通知 (64 12 00) か。
var IsPatchNotice = zoomReply(0x64, 0x12, 0x00)

func (four) open(z *Zoom, edit bool) error {
	if !edit {
		return nil
	}
	if _, err := z.expect(sysex.EditorModeOn(z.DeviceID()), 2*time.Second, 0x00, 0x00); err != nil {
		return err
	}
	z.Editable = true
	if _, err := z.expect(sysex.SetAutoSave(z.DeviceID(), false), 2*time.Second, 0x64, 0x20, 0x01); err != nil {
		z.Close()
		return err
	}
	return nil
}

func (four) close(z *Zoom) {
	if z.Editable {
		z.expect(sysex.EditorModeOff(z.DeviceID()), 2*time.Second, 0x00, 0x00)
	}
}

func (four) patchSysex(z *Zoom, n int) ([]byte, error) {
	loc, err := z.Model.Location(n)
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

func (four) keepCurrent(_ *Zoom, fn func() error) error { return fn() }

func (four) currentPatch(z *Zoom) (*patch.Patch, error) {
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

func (four) notice(_ *Zoom, msg []byte) (*patch.Patch, bool) {
	if !IsPatchNotice(msg) {
		return nil, false
	}
	p, err := DecodeSysex(msg)
	return p, err == nil
}

// edit は 64 20 00 で 1 項目変える。値が変わらないときペダルは何も返さないので、
// 受理が来なければ現在パッチを読み直し、値が合っていれば成功とする。
func (f four) edit(z *Zoom, slot, paramNo int, value uint32, applied func(*patch.Patch) bool) error {
	_, err := z.expect(sysex.SetParameter(z.DeviceID(), slot, paramNo, value), time.Second, 0x64, 0x20, 0x01)
	if err == nil {
		return nil
	}
	p, err2 := f.currentPatch(z)
	if err2 == nil && applied(p) {
		return nil
	}
	return err
}

func (f four) setParam(z *Zoom, slot, index, value int) error {
	return f.edit(z, slot, index+2, uint32(value), func(p *patch.Patch) bool {
		e := effectAt(p, slot)
		return e != nil && index < len(e.Params) && e.Params[index] == value
	})
}

func (f four) setEnabled(z *Zoom, slot int, on bool) error {
	v := uint32(0)
	if on {
		v = 1
	}
	return f.edit(z, slot, 0, v, func(p *patch.Patch) bool {
		e := effectAt(p, slot)
		return e != nil && e.Enabled == on
	})
}

// setEffect の応答は 64 20 01 ではなく 64 12 00（差し替え後の現在パッチ全体）なので、それを返す。
func (four) setEffect(z *Zoom, slot int, id uint32) (*patch.Patch, error) {
	reply, err := z.expect(sysex.SetParameter(z.DeviceID(), slot, 1, id), 4*time.Second, 0x64, 0x12, 0x00)
	if err != nil {
		return nil, err
	}
	return DecodeSysex(reply)
}

// setName はまだない。名前は PTCF の NAME にあるが、64 20 で変えられるかは確かめていない
// （28 で丸ごと送るのは G2 FOUR では使えない）。
func (four) setName(*Zoom, string) error {
	return errors.New("G2 FOUR のパッチ名の変更はまだできない")
}

func (f four) savePatch(z *Zoom, n int, p *patch.Patch) error {
	loc, err := z.Model.Location(n)
	if err != nil {
		return err
	}
	buffer, err := patch.Encode(p)
	if err != nil {
		return err
	}
	if err := f.writePatchSysex(z, n, codec.EncodePatchSysex(z.DeviceID(), loc, buffer)); err != nil {
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

func (four) writePatchSysex(z *Zoom, n int, msg []byte) error {
	want, err := z.Model.Location(n)
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
