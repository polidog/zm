package device

import (
	"errors"
	"fmt"
	"time"

	"github.com/polidog/zm/internal/sysex"
)

// programTap は受信の生バイトから Bank Select (CC0 / CC32) と Program Change を拾い、選ばれたパッチの通し番号（0 始まり）を送る。
// 33（いま選んでいるパッチ）の応答は SysEx ではなくこの形（b0 00 mm b0 20 ll c0 pp）で返るので、Conn の生バイトから拾う。
// ペダルで切り替えたときも届く。feed は受信の goroutine から呼ばれる。
type programTap struct {
	ch      chan int
	perBank int
	status  byte
	data    []byte
	// bank は CC0（上位）と CC32（下位）。
	bank [2]int
}

func newProgramTap(perBank int) *programTap {
	return &programTap{ch: make(chan int, 16), perBank: perBank}
}

func (t *programTap) feed(b []byte) {
	for _, c := range b {
		switch {
		case c >= 0xf8: // リアルタイムは間に挟まっても流れを変えない
		case c >= 0x80:
			t.status, t.data = c&0xf0, t.data[:0]
			if c >= 0xf0 {
				t.status = c // SysEx の中身は読み飛ばす
			}
		case t.status == 0xb0:
			if t.data = append(t.data, c); len(t.data) == 2 {
				switch t.data[0] {
				case 0x00:
					t.bank[0] = int(t.data[1])
				case 0x20:
					t.bank[1] = int(t.data[1])
				}
				t.data = t.data[:0] // ランニングステータス
			}
		case t.status == 0xc0:
			n := (t.bank[0]<<7|t.bank[1])*t.perBank + int(c)
			select {
			case t.ch <- n:
			default:
			}
		}
	}
}

// Program はいま選んでいるパッチの番号（0 始まり）を 33 で聞く。
// G2 FOUR 系はエディタモードでなくても答える（MS-80IR+ のファームウェア 1.10 で確かめた）。旧 MS はエディタモード中だけ。
func (z *Zoom) Program() (int, error) {
	t := z.programs
	for len(t.ch) > 0 {
		<-t.ch
	}
	if err := z.Conn.Send(sysex.GetCurrentProgram(z.DeviceID())); err != nil {
		return 0, err
	}
	select {
	case n := <-t.ch:
		if n >= z.Model.Patches {
			return 0, fmt.Errorf("33 の応答のパッチ番号 %d が範囲の外", n+1)
		}
		return n, nil
	case <-time.After(2 * time.Second):
		return 0, errors.New("33（いま選んでいるパッチ）に応答がない")
	}
}

// SelectPatch はパッチ n に切り替え、33 で切り替わったか確かめる。いま鳴っているパッチの未保存の編集は捨てる
// （G2 FOUR 系は Open(true) で AUTO SAVE を OFF にしてあり、旧 MS は切り替える前に捨てる）。
func (z *Zoom) SelectPatch(n int) error {
	if err := z.assertEditable(); err != nil {
		return err
	}
	if _, err := z.Model.Location(n); err != nil {
		return err
	}
	if err := z.proto.selectPatch(z, n); err != nil {
		return err
	}
	// 切り替えた直後の 33 には前の番号が返るので、変わるまで聞き直す
	deadline := time.Now().Add(3 * time.Second)
	for {
		got, err := z.Program()
		if err != nil {
			return err
		}
		if got == n {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("パッチ %d に切り替わらない（いまは %d）", n+1, got+1)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
