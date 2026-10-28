package device

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/polidog/zm/internal/codec"
	"github.com/polidog/zm/internal/patch"
	"github.com/polidog/zm/internal/sysex"
)

// four は G2 FOUR と MS-80IR+（デバイス ID 6E）。
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

func (four) currentSysex(z *Zoom) ([]byte, error) {
	// 64 12 00 は編集時の通知。要求への応答は 64 12 01
	reply, err := z.Conn.Request(sysex.RequestCurrentPatch(z.DeviceID()), 4*time.Second, zoomReply(0x64, 0x12, 0x01))
	if err != nil {
		return nil, err
	}
	if reply == nil {
		return nil, errors.New("現在パッチのダンプに応答がない")
	}
	return reply, nil
}

func (f four) currentPatch(z *Zoom) (*patch.Patch, error) {
	msg, err := f.currentSysex(z)
	if err != nil {
		return nil, err
	}
	return DecodeSysex(msg)
}

// loadPatch はユーザーのスロットとテンポを、違うところだけ 64 20 で want に合わせる。
// エフェクトを差し替えるとパラメータが既定値（か前回の値）になるので、差し替えてから読み直してパラメータを合わせる。
func (f four) loadPatch(z *Zoom, msg []byte) error {
	want, err := DecodeSysex(msg)
	if err != nil {
		return err
	}
	now, err := f.currentPatch(z)
	if err != nil {
		return err
	}
	slots := min(z.Model.UserSlots, len(want.Effects), len(now.Effects))
	for s := range slots {
		w := want.Effects[s]
		if now.Effects[s].ID != w.ID {
			if now, err = f.setEffect(z, s, w.ID); err != nil {
				return fmt.Errorf("スロット %d: %w", s+1, err)
			}
		}
		cur := now.Effects[s]
		for i, v := range w.Params {
			if i < len(cur.Params) && cur.Params[i] != v {
				if err := f.setParam(z, s, i, v); err != nil {
					return fmt.Errorf("スロット %d のパラメータ %d: %w", s+1, i+1, err)
				}
			}
		}
		if cur.Enabled != w.Enabled {
			if err := f.setEnabled(z, s, w.Enabled); err != nil {
				return fmt.Errorf("スロット %d の ON / OFF: %w", s+1, err)
			}
		}
	}
	if now.Prm2.Tempo != want.Prm2.Tempo {
		if err := f.setTempo(z, want.Prm2.Tempo); err != nil {
			return fmt.Errorf("テンポ: %w", err)
		}
	}
	back, err := f.currentPatch(z)
	if err != nil {
		return err
	}
	for s := range slots {
		x, y := back.Effects[s], want.Effects[s]
		if x.ID != y.ID || x.Enabled != y.Enabled || !slices.Equal(x.Params, y.Params) {
			return fmt.Errorf("スロット %d が流し込んだ中身と一致しない", s+1)
		}
	}
	return nil
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

// setTempo は 64 20 00 64 02。スロットの代わりの 64 はパッチ全体の項目で、AUTO SAVE (64 0F) と同じ並び
// （G2 FOUR のファームウェア 1.00、MS-80IR+ の 1.10 で確かめた）。
func (f four) setTempo(z *Zoom, bpm int) error {
	return f.edit(z, 0x64, 0x02, uint32(bpm), func(p *patch.Patch) bool { return p.Prm2.Tempo == bpm })
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
