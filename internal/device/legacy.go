package device

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/polidog/zoom-ai/internal/catalog"
	"github.com/polidog/zoom-ai/internal/legacypatch"
	"github.com/polidog/zoom-ai/internal/patch"
	"github.com/polidog/zoom-ai/internal/sysex"
)

// legacy は旧 MS-50G（デバイス ID 58）。
//
// パッチメモリを番号で読む SysEx が無い。Program Change で選んでから 29 で現在パッチを読む。
//
// 本体の AUTO SAVE が ON だと、編集中に Program Change で切り替えた時点で編集がそのパッチに保存される
// （実機で確かめた）。そこで切り替える前に、編集中の中身を 29 で控えてから 51 → 50 で捨てておき
// （51 は未保存の編集を保存せずに捨て、パッチを読み込み直す）、元のパッチに戻ってから 28 で書き戻す（KeepCurrent）。
//
// 33 と 29 はエディタモード中しか答えないので、読み出しだけでも開くときにエディタモード (50) にする。
// 51 で OFF に戻すと編集が消えるので、閉じても OFF にはしない。
// 編集は 31（1 項目）か 28（パッチ全体）で、保存は 32。どちらも送った後に 29 で読み直して確かめる。
// 31 が効くのはスロット 1〜3 だけ（ファームウェア 3.10 で確かめた）。4〜6 は 28 で全体を送る（31 を送ると名前などが壊れる）。
type legacy struct{}

type legacyState struct {
	// programs は 33 への応答とペダルの操作で届いた Program Change の番号。
	programs chan int
	// keeping は KeepCurrent の入れ子の深さ。
	keeping int
	// last は最後に読んだ現在パッチ。31 の通知をここに当てて、変化後のパッチを作る。
	mu   sync.Mutex
	last []byte
}

func (legacy) open(z *Zoom, edit bool) error {
	st := &legacyState{programs: make(chan int, 16)}
	z.legacy = st
	// Program Change は SysEx ではないので、Conn の生バイトから拾う
	running := false
	z.Conn.SetTap(func(b []byte) {
		for _, c := range b {
			switch {
			case c == 0xc0:
				running = true
			case c >= 0x80:
				running = false
			case running:
				running = false
				select {
				case st.programs <- int(c):
				default:
				}
			}
		}
	})
	if err := z.Conn.Send(sysex.EditorModeOn(z.DeviceID())); err != nil {
		return err
	}
	// 50 には応答がない。zoom-ms-utility も少し待ってから次を送る
	time.Sleep(100 * time.Millisecond)
	z.Editable = edit
	return nil
}

// close はエディタモードを OFF (51) にしない。旧 MS は 51 で未保存の編集を捨て、パッチを読み込み直す
// （実機で確かめた）。zoom-ms-utility も 51 は送らない。エディタモードは電源を切るまで残る。
func (legacy) close(z *Zoom) {
	z.Conn.SetTap(nil)
}

// program は 33 でいま選んでいるパッチ番号（0 始まり）を聞く。
func (legacy) program(z *Zoom) (int, error) {
	st := z.legacy
	for len(st.programs) > 0 {
		<-st.programs
	}
	if err := z.Conn.Send(sysex.GetCurrentProgram(z.DeviceID())); err != nil {
		return 0, err
	}
	select {
	case n := <-st.programs:
		return n, nil
	case <-time.After(2 * time.Second):
		return 0, errors.New("33（現在のパッチ番号）に応答がない")
	}
}

func (legacy) selectProgram(z *Zoom, n int) error {
	return z.Conn.Send(sysex.ProgramChange(n))
}

// discard は現在パッチの未保存の編集を、保存せずに捨てる（51 → 50）。どちらにも応答はない。
func (legacy) discard(z *Zoom) error {
	if err := z.Conn.Send(sysex.EditorModeOff(z.DeviceID())); err != nil {
		return err
	}
	time.Sleep(100 * time.Millisecond)
	if err := z.Conn.Send(sysex.EditorModeOn(z.DeviceID())); err != nil {
		return err
	}
	time.Sleep(100 * time.Millisecond)
	return nil
}

// current は 29 で現在パッチの SysEx (28) を読む。
func (legacy) current(z *Zoom) ([]byte, error) {
	reply, err := z.Conn.Request(sysex.LegacyRequestCurrentPatch(z.DeviceID()), 3*time.Second, zoomReply(0x28))
	if err != nil {
		return nil, err
	}
	if reply == nil {
		return nil, errors.New("29（現在パッチ）に応答がない")
	}
	if err := legacypatch.Check(reply); err != nil {
		return nil, err
	}
	z.legacy.mu.Lock()
	z.legacy.last = bytes.Clone(reply)
	z.legacy.mu.Unlock()
	return reply, nil
}

// put は 28 で現在パッチの中身を msg に置き換え、読み直して確かめる（保存はしない）。
func (l legacy) put(z *Zoom, msg []byte) ([]byte, error) {
	if err := z.Conn.Send(msg); err != nil {
		return nil, err
	}
	back, err := l.current(z)
	if err != nil {
		return nil, err
	}
	return back, nil
}

func (l legacy) patchSysex(z *Zoom, n int) ([]byte, error) {
	if z.legacy.keeping == 0 {
		var msg []byte
		err := l.keepCurrent(z, func() error {
			var err error
			msg, err = l.patchSysex(z, n)
			return err
		})
		return msg, err
	}
	if err := l.selectProgram(z, n); err != nil {
		return nil, err
	}
	msg, err := l.current(z)
	if err != nil {
		return nil, fmt.Errorf("パッチ %d を読めない: %w", n, err)
	}
	return msg, nil
}

func (l legacy) keepCurrent(z *Zoom, fn func() error) error {
	st := z.legacy
	if st.keeping > 0 {
		return fn()
	}
	program, err := l.program(z)
	if err != nil {
		return err
	}
	editing, err := l.current(z)
	if err != nil {
		return err
	}
	if err := l.discard(z); err != nil {
		return err
	}
	st.keeping++
	err = fn()
	st.keeping--
	if rerr := l.restore(z, program, editing); err == nil {
		err = rerr
	}
	return err
}

// restore はパッチ program を選び直し、中身が editing と違えば（編集中だったら）28 で書き戻す。
// 書き戻した編集は保存していない（AUTO SAVE が ON なら、次にペダルで切り替えたときに保存される）。
func (l legacy) restore(z *Zoom, program int, editing []byte) error {
	if err := l.selectProgram(z, program); err != nil {
		return err
	}
	now, err := l.current(z)
	if err != nil {
		return err
	}
	if bytes.Equal(now, editing) {
		return nil
	}
	back, err := l.put(z, editing)
	if err != nil {
		return err
	}
	if !samePatch(back, editing) {
		return fmt.Errorf("パッチ %d の編集中の中身を戻せなかった", program+1)
	}
	return nil
}

func decodeLegacy(msg []byte) (*patch.Patch, error) {
	lp, err := legacypatch.Parse(msg)
	if err != nil {
		return nil, err
	}
	p := &patch.Patch{
		Name:    lp.Name(),
		Effects: make([]patch.Effect, legacypatch.Slots),
		Prm2:    patch.Prm2{EditSlot: lp.EditSlot(), Tempo: lp.Tempo()},
	}
	for s := range p.Effects {
		e := lp.Effect(s)
		p.Effects[s] = patch.Effect{ID: e.ID, Enabled: e.Enabled, Params: e.Params[:]}
	}
	return p, nil
}

// samePatch は名前とエフェクト（ID・ON/OFF・パラメータ）が同じか。画面で選んでいるスロットなどは見ない。
func samePatch(a, b []byte) bool {
	pa, err1 := decodeLegacy(a)
	pb, err2 := decodeLegacy(b)
	return err1 == nil && err2 == nil && sameEffects(pa, pb)
}

func sameEffects(a, b *patch.Patch) bool {
	if a.Name != b.Name || len(a.Effects) != len(b.Effects) {
		return false
	}
	for i := range a.Effects {
		x, y := a.Effects[i], b.Effects[i]
		if x.ID != y.ID || x.Enabled != y.Enabled || len(x.Params) != len(y.Params) {
			return false
		}
		for j := range x.Params {
			if x.Params[j] != y.Params[j] {
				return false
			}
		}
	}
	return true
}

func (l legacy) currentPatch(z *Zoom) (*patch.Patch, error) {
	msg, err := l.current(z)
	if err != nil {
		return nil, err
	}
	return decodeLegacy(msg)
}

// notice は 31（ペダルのつまみを回したとき）を最後に読んだ現在パッチに当てる。28 はそのまま読む。
func (legacy) notice(z *Zoom, msg []byte) (*patch.Patch, bool) {
	if !zoomReply()(msg) || msg[3] != z.DeviceID() {
		return nil, false
	}
	st := z.legacy
	st.mu.Lock()
	defer st.mu.Unlock()
	switch {
	case msg[4] == 0x28 && legacypatch.Check(msg) == nil:
		st.last = bytes.Clone(msg)
	case msg[4] == 0x31 && len(msg) == 10 && st.last != nil:
		slot, no, v := int(msg[5]), int(msg[6]), sysex.Join14(msg[7], msg[8])
		if slot >= legacypatch.Slots {
			return nil, false
		}
		lp := &legacypatch.Patch{Raw: st.last}
		switch {
		case no == 0:
			lp.SetEnabled(slot, v != 0)
		case no >= 2 && no-2 < legacypatch.MaxParams:
			lp.SetParam(slot, no-2, v)
		default:
			return nil, false
		}
	default:
		return nil, false
	}
	p, err := decodeLegacy(st.last)
	return p, err == nil
}

// directSlots は 31 が効くスロットの数。4〜6 に 31 を送るとパラメータには効かず、
// 別の場所（パッチ名など）が書き換わる（ファームウェア 3.10 で、スロット 5 の Time で名前が化けた）。
const directSlots = 3

// edit は 1 項目変える。スロット 1〜3 は 31、4〜6 は 28 でパッチ全体を送る。31 には応答がないので読み直し、
// 変えた項目のほかに変わったところがあれば（31 が効かなかった、ほかを壊した）、28 で正しい中身を送り直す。
func (l legacy) edit(z *Zoom, slot, paramNo, value int, apply func(*legacypatch.Patch)) error {
	if value < 0 || value >= 1<<14 {
		return fmt.Errorf("値は 0〜%d", 1<<14-1)
	}
	before, err := l.current(z)
	if err != nil {
		return err
	}
	want, err := legacypatch.Parse(before)
	if err != nil {
		return err
	}
	apply(want)
	if slot < directSlots {
		if err := z.Conn.Send(sysex.LegacySetParameter(z.DeviceID(), slot, paramNo, value)); err != nil {
			return err
		}
		back, err := l.current(z)
		if err != nil {
			return err
		}
		if samePatch(back, want.Raw) {
			return nil
		}
	}
	back, err := l.put(z, want.Raw)
	if err != nil {
		return err
	}
	if !samePatch(back, want.Raw) {
		return fmt.Errorf("スロット %d の変更をペダルが受け付けない", slot+1)
	}
	return nil
}

func (l legacy) setParam(z *Zoom, slot, index, value int) error {
	if index < 0 || index >= legacypatch.MaxParams {
		return fmt.Errorf("パラメータは %d 個まで", legacypatch.MaxParams)
	}
	return l.edit(z, slot, index+2, value, func(lp *legacypatch.Patch) { lp.SetParam(slot, index, value) })
}

func (l legacy) setEnabled(z *Zoom, slot int, on bool) error {
	v := 0
	if on {
		v = 1
	}
	return l.edit(z, slot, 0, v, func(lp *legacypatch.Patch) { lp.SetEnabled(slot, on) })
}

// setEffect はエフェクトを差し替えたパッチ全体を 28 で送る（旧 MS には ID を 1 つ変えるメッセージがない）。
// パラメータはカタログの既定値。空いていたスロットに入れたときは ON にする。
// ペダルに入っていないエフェクトや、DSP が足りないときはペダルが元に戻すので、読み直して確かめる。
func (l legacy) setEffect(z *Zoom, slot int, id uint32) (*patch.Patch, error) {
	if slot < 0 || slot >= legacypatch.Slots {
		return nil, fmt.Errorf("スロットは 0..%d", legacypatch.Slots-1)
	}
	cat, err := catalog.MS50G()
	if err != nil {
		return nil, err
	}
	e := cat.Find(id)
	if e == nil {
		return nil, fmt.Errorf("エフェクト %s が MS-50G のカタログにない", catalog.FormatID(id))
	}
	var params [legacypatch.MaxParams]int
	for i, p := range e.Params {
		if i < len(params) {
			params[i] = p.Default
		}
	}
	var amp *legacypatch.Amp
	if e.GroupName == "AMP" {
		ver, err := strconv.ParseUint(e.Version, 0, 16)
		if err != nil {
			return nil, fmt.Errorf("%s の version が読めない: %q", e.Name, e.Version)
		}
		amp = &legacypatch.Amp{V2: ver&0xf0 == 0x20, GuitarCab: ver&0xf != 0}
	}
	msg, err := l.current(z)
	if err != nil {
		return nil, err
	}
	lp, err := legacypatch.Parse(msg)
	if err != nil {
		return nil, err
	}
	wasEmpty := lp.Effect(slot).ID == 0
	lp.SetEffect(slot, id, params, amp)
	if wasEmpty {
		lp.SetEnabled(slot, true)
	}
	back, err := l.put(z, lp.Raw)
	if err != nil {
		return nil, err
	}
	p, err := decodeLegacy(back)
	if err != nil {
		return nil, err
	}
	if p.Effects[slot].ID != id {
		return nil, fmt.Errorf("ペダルが %s を受け付けない（ペダルに入っていないか、DSP が足りない）", e.Name)
	}
	return p, nil
}

// setName は名前を変えたパッチ全体を 28 で送る（名前だけを変えるメッセージはない）。
func (l legacy) setName(z *Zoom, name string) error {
	msg, err := l.current(z)
	if err != nil {
		return err
	}
	lp, err := legacypatch.Parse(msg)
	if err != nil {
		return err
	}
	if err := lp.SetName(name); err != nil {
		return err
	}
	back, err := l.put(z, lp.Raw)
	if err != nil {
		return err
	}
	if !samePatch(back, lp.Raw) {
		return errors.New("名前の変更をペダルが受け付けない")
	}
	return nil
}

// store は現在パッチを n に保存し、n を選び直して（パッチメモリから読み込み直して）want と同じか確かめる。
func (l legacy) store(z *Zoom, n int, want []byte) error {
	if _, err := z.expect(sysex.LegacyStore(z.DeviceID(), n), 5*time.Second, 0x00); err != nil {
		return err
	}
	if err := l.selectProgram(z, n); err != nil {
		return err
	}
	back, err := l.current(z)
	if err != nil {
		return err
	}
	if !samePatch(back, want) {
		return fmt.Errorf("パッチ %d に保存した中身が読み直しと一致しない", n+1)
	}
	return nil
}

// savePatch は編集中のパッチを n に保存する。いま選んでいるのが n でなければ、編集を捨ててから n を選び
// （元のパッチに AUTO SAVE で保存されないように）、中身を 28 で入れて保存する。保存の後は n を選んだ状態になる。
func (l legacy) savePatch(z *Zoom, n int, p *patch.Patch) error {
	editing, err := l.current(z)
	if err != nil {
		return err
	}
	now, err := decodeLegacy(editing)
	if err != nil {
		return err
	}
	if !sameEffects(now, p) {
		return errors.New("保存しようとしたパッチが、いま鳴っているパッチと違う")
	}
	program, err := l.program(z)
	if err != nil {
		return err
	}
	if program != n {
		if err := l.discard(z); err != nil {
			return err
		}
		if err := l.selectProgram(z, n); err != nil {
			return err
		}
		back, err := l.put(z, editing)
		if err != nil {
			return err
		}
		if !samePatch(back, editing) {
			return fmt.Errorf("パッチ %d に中身を入れられなかった", n+1)
		}
	}
	return l.store(z, n, editing)
}

// writePatchSysex は退避した 28 を n に書き戻す。n を選び、28 で中身を入れて保存する。
// いま選んでいるのが n でなければ、書き戻した後に元のパッチと編集中の中身に戻す（KeepCurrent）。
// n なら、n の編集中の中身は捨てる（書き戻した中身が正になる）。
func (l legacy) writePatchSysex(z *Zoom, n int, msg []byte) error {
	if err := legacypatch.Check(msg); err != nil {
		return err
	}
	if msg[3] != z.DeviceID() {
		return fmt.Errorf("デバイス ID %02X の SysEx は書き戻せない", msg[3])
	}
	program, err := l.program(z)
	if err != nil {
		return err
	}
	if program == n {
		if err := l.discard(z); err != nil {
			return err
		}
		return l.write(z, n, msg)
	}
	return l.keepCurrent(z, func() error { return l.write(z, n, msg) })
}

// write は n を選び、msg を 28 で入れて保存する。今のパッチに未保存の編集が無いこと（discard の後）。
func (l legacy) write(z *Zoom, n int, msg []byte) error {
	if err := l.selectProgram(z, n); err != nil {
		return err
	}
	back, err := l.put(z, msg)
	if err != nil {
		return err
	}
	if !samePatch(back, msg) {
		return fmt.Errorf("パッチ %d に中身を入れられなかった", n+1)
	}
	return l.store(z, n, msg)
}
