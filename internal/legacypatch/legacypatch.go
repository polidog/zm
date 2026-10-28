// Package legacypatch は旧 MS（MS-50G, デバイス ID 58）のパッチ形式。
//
// パッチは 28 で始まる 146 バイトの SysEx。7bit パックはされているが、ビットの並びが入り組んでいるので
// 展開せず、SysEx のバイト上のビット位置で読み書きする。位置は g200kg/zoom-ms-utility の apatch.js（MIT）から。
// エフェクトは 6 個まで、1 個につき ON/OFF・エフェクト ID・パラメータ 9 個。
package legacypatch

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
)

// Size は MS-50G のパッチ SysEx の長さ。
const Size = 146

const (
	Slots     = 6
	MaxParams = 9
)

// field は 1 つの値を作るビットの断片。at バイト目の mask を shift だけずらして値に足す（負なら右へ）。
//
// apatch.js はパラメータ 8 が 0 でなければキャビネットの印 (cabAt) の 0x40 も立てるが、
// 実機のパッチではアンプ以外で立っていないので、ここには入れず SetEffect で扱う。
type field struct {
	at    int
	mask  byte
	shift int
}

func f(at int, mask byte, shift int) field { return field{at: at, mask: mask, shift: shift} }

// bits[slot][k]: k = 0 が ON/OFF、1 がエフェクト ID、2.. がパラメータ 1..9。
var bits = [Slots][2 + MaxParams][]field{
	{
		{f(6, 0x01, 0)},
		{f(5, 0x40, 24), f(6, 0x7e, 16), f(7, 0x07, 8), f(9, 0x1f, 0)},
		{f(9, 0x60, -5), f(5, 0x08, -1), f(10, 0x7f, 3), f(5, 0x04, 8), f(11, 0x01, 11)},
		{f(11, 0x7c, -2), f(5, 0x02, 4), f(12, 0x1f, 6)},
		{f(5, 0x01, 0), f(14, 0x7f, 1), f(13, 0x40, 2), f(15, 0x03, 9)},
		{f(15, 0x70, -4), f(13, 0x20, -2), f(16, 0x0f, 4)},
		{f(16, 0x70, -4), f(13, 0x10, -1), f(17, 0x0f, 4)},
		{f(17, 0x70, -4), f(13, 0x08, 0), f(18, 0x0f, 4)},
		{f(18, 0x70, -4), f(13, 0x04, 1), f(19, 0x0f, 4)},
		{f(19, 0x70, -4), f(13, 0x02, 2), f(20, 0x1f, 4)},
		{f(24, 0x7f, 0), f(21, 0x10, 3)},
	},
	{
		{f(26, 0x01, 0)},
		{f(21, 0x04, 28), f(26, 0x7e, 16), f(27, 0x07, 8), f(30, 0x1f, 0)},
		{f(30, 0x60, -5), f(29, 0x40, -4), f(31, 0x7f, 3), f(29, 0x20, 5), f(32, 0x01, 11)},
		{f(32, 0x7c, -2), f(29, 0x10, 1), f(33, 0x1f, 6)},
		{f(29, 0x08, -3), f(34, 0x7f, 1), f(29, 0x04, 6), f(35, 0x03, 9)},
		{f(35, 0x70, -4), f(29, 0x02, 2), f(36, 0x0f, 4)},
		{f(36, 0x70, -4), f(29, 0x01, 3), f(38, 0x0f, 4)},
		{f(38, 0x70, -4), f(37, 0x40, -3), f(39, 0x0f, 4)},
		{f(39, 0x70, -4), f(37, 0x20, -2), f(40, 0x0f, 4)},
		{f(40, 0x70, -4), f(37, 0x10, -1), f(41, 0x1f, 4)},
		{f(44, 0x7f, 0), f(37, 0x01, 7)},
	},
	{
		{f(47, 0x01, 0)},
		{f(45, 0x20, 25), f(47, 0x7e, 16), f(48, 0x07, 8), f(50, 0x1f, 0)},
		{f(50, 0x60, -5), f(45, 0x04, 0), f(51, 0x7f, 3), f(45, 0x02, 9), f(52, 0x01, 11)},
		{f(52, 0x7c, -2), f(45, 0x01, 5), f(54, 0x1f, 6)},
		{f(53, 0x40, -6), f(55, 0x7f, 1), f(53, 0x20, 3), f(56, 0x03, 9)},
		{f(56, 0x70, -4), f(53, 0x10, -1), f(57, 0x0f, 4)},
		{f(57, 0x70, -4), f(53, 0x08, 0), f(58, 0x0f, 4)},
		{f(58, 0x70, -4), f(53, 0x04, 1), f(59, 0x0f, 4)},
		{f(59, 0x70, -4), f(53, 0x02, 2), f(60, 0x0f, 4)},
		{f(60, 0x70, -4), f(53, 0x01, 3), f(62, 0x1f, 4)},
		{f(65, 0x7f, 0), f(61, 0x08, 4)},
	},
	{
		{f(67, 0x01, 0)},
		{f(61, 0x02, 29), f(67, 0x7e, 16), f(68, 0x07, 8), f(71, 0x1f, 0)},
		{f(71, 0x60, -5), f(69, 0x20, -3), f(72, 0x7f, 3), f(69, 0x10, 6), f(73, 0x01, 11)},
		{f(73, 0x7c, -2), f(69, 0x08, 2), f(74, 0x1f, 6)},
		{f(69, 0x04, -2), f(75, 0x7f, 1), f(69, 0x02, 7), f(76, 0x03, 9)},
		{f(76, 0x70, -4), f(69, 0x01, 3), f(78, 0x0f, 4)},
		{f(78, 0x70, -4), f(77, 0x40, -3), f(79, 0x0f, 4)},
		{f(79, 0x70, -4), f(77, 0x20, -2), f(80, 0x0f, 4)},
		{f(80, 0x70, -4), f(77, 0x10, -1), f(81, 0x0f, 4)},
		{f(81, 0x70, -4), f(77, 0x08, 0), f(82, 0x1f, 4)},
		{f(86, 0x7f, 0), f(85, 0x40, 1)},
	},
	{
		{f(88, 0x01, 0)},
		{f(85, 0x10, 26), f(88, 0x7e, 16), f(89, 0x07, 8), f(91, 0x1f, 0)},
		{f(91, 0x60, -5), f(85, 0x02, 1), f(92, 0x7f, 3), f(85, 0x01, 10), f(94, 0x01, 11)},
		{f(94, 0x7c, -2), f(93, 0x40, -1), f(95, 0x1f, 6)},
		{f(93, 0x20, -5), f(96, 0x7f, 1), f(93, 0x10, 4), f(97, 0x03, 9)},
		{f(97, 0x70, -4), f(93, 0x08, 0), f(98, 0x0f, 4)},
		{f(98, 0x70, -4), f(93, 0x04, 1), f(99, 0x0f, 4)},
		{f(99, 0x70, -4), f(93, 0x02, 2), f(100, 0x0f, 4)},
		{f(100, 0x70, -4), f(93, 0x01, 3), f(102, 0x0f, 4)},
		{f(102, 0x70, -4), f(101, 0x40, -3), f(103, 0x1f, 4)},
		{f(106, 0x7f, 0), f(106, 0x04, 5)},
	},
	{
		{f(108, 0x01, 0)},
		{f(101, 0x01, 30), f(108, 0x7e, 16), f(110, 0x07, 8), f(112, 0x1f, 0)},
		{f(112, 0x60, -5), f(109, 0x10, -2), f(113, 0x7f, 3), f(109, 0x08, 7), f(114, 0x01, 11)},
		{f(114, 0x7c, -2), f(109, 0x04, 3), f(115, 0x1f, 6)},
		{f(109, 0x02, -1), f(116, 0x7f, 1), f(109, 0x01, 8), f(118, 0x03, 9)},
		{f(118, 0x70, -4), f(117, 0x40, -3), f(119, 0x0f, 4)},
		{f(119, 0x70, -4), f(117, 0x20, -2), f(120, 0x0f, 4)},
		{f(120, 0x70, -4), f(117, 0x10, -1), f(121, 0x0f, 4)},
		{f(121, 0x70, -4), f(117, 0x08, 0), f(122, 0x0f, 4)},
		{f(122, 0x70, -4), f(117, 0x04, 1), f(123, 0x1f, 4)},
		{f(127, 0x7f, 0), f(125, 0x20, 2)},
	},
}

// nameAt は名前 10 文字のバイト位置（間にパックの上位ビットのバイトが挟まる）。
var nameAt = [10]int{132, 134, 135, 136, 137, 138, 139, 140, 142, 143}

// cabAt はアンプのキャビネットの印、v2At は B-AMP の ver2 の印のバイト。
// v2 の印はスロット 1〜4 にしかない（apatch.js でも 4 つ）。
var (
	cabAt = [Slots]int{23, 43, 64, 84, 105, 126}
	v2At  = [Slots]int{8, 28, 49, 70, -1, -1}
)

// maxFxAt はエフェクトの使用数 (n0-n2)、dspAt は DSP の使用状況のバイト。
const (
	maxFxAt = 130
	dspAt   = 129
)

func get(msg []byte, fs []field) int {
	v := 0
	for _, x := range fs {
		b := int(msg[x.at] & x.mask)
		if x.shift >= 0 {
			v |= b << x.shift
		} else {
			v |= b >> -x.shift
		}
	}
	return v
}

func set(msg []byte, fs []field, v int) {
	for _, x := range fs {
		var b int
		if x.shift >= 0 {
			b = v >> x.shift
		} else {
			b = v << -x.shift
		}
		msg[x.at] = msg[x.at]&^x.mask | byte(b)&x.mask
	}
}

type Effect struct {
	ID      uint32
	Enabled bool
	Params  [MaxParams]int
}

// Patch は SysEx を持ったまま、読み書きをビット位置で行う。
type Patch struct {
	Raw []byte
}

// Check は旧 MS のパッチ SysEx (F0 52 00 58 28 … F7, 146 バイト) か。
func Check(msg []byte) error {
	if len(msg) != Size || msg[0] != 0xf0 || msg[1] != 0x52 || msg[4] != 0x28 || msg[Size-1] != 0xf7 {
		head := msg[:min(6, len(msg))]
		return fmt.Errorf("旧 MS のパッチ (F0 52 00 58 28, %d バイト) ではない: %d バイト, % x…", Size, len(msg), head)
	}
	return nil
}

func Parse(msg []byte) (*Patch, error) {
	if err := Check(msg); err != nil {
		return nil, err
	}
	return &Patch{Raw: bytes.Clone(msg)}, nil
}

func (p *Patch) Name() string {
	var b strings.Builder
	for _, at := range nameAt {
		if c := p.Raw[at]; c != 0 {
			b.WriteByte(c)
		}
	}
	return strings.TrimRight(b.String(), " ")
}

func (p *Patch) SetName(name string) error {
	if len(name) > len(nameAt) {
		return fmt.Errorf("名前は %d 文字まで", len(nameAt))
	}
	for i, at := range nameAt {
		c := byte(' ')
		if i < len(name) {
			c = name[i]
		}
		if c >= 0x80 {
			return errors.New("名前は ASCII だけ")
		}
		p.Raw[at] = c
	}
	return nil
}

func (p *Patch) Effect(slot int) Effect {
	e := Effect{
		Enabled: get(p.Raw, bits[slot][0][:]) == 1,
		ID:      uint32(get(p.Raw, bits[slot][1][:])),
	}
	for i := range e.Params {
		e.Params[i] = get(p.Raw, bits[slot][2+i][:])
	}
	return e
}

func (p *Patch) SetEnabled(slot int, on bool) {
	v := 0
	if on {
		v = 1
	}
	set(p.Raw, bits[slot][0], v)
}

func (p *Patch) SetParam(slot, index, value int) { set(p.Raw, bits[slot][2+index], value) }

// Amp はアンプのエフェクトの、キャビネットの印の付け方（apatch.js の MakeBin と同じ）。
type Amp struct {
	// V2 は B-AMP の ver2（version の上位 4bit が 2）。
	V2 bool
	// GuitarCab はキャビネットの印が ON/OFF だけの G-AMP（version の下位 4bit が 0 でない）。
	GuitarCab bool
}

// SetEffect はスロットのエフェクトを差し替え、パラメータを params にする。amp はアンプのときだけ渡す。
// エフェクトの使用数 (n0-n2) は、最後の空でないスロットに合わせて付け直す。
func (p *Patch) SetEffect(slot int, id uint32, params [MaxParams]int, amp *Amp) {
	set(p.Raw, bits[slot][1], int(id))
	for i, v := range params {
		p.SetParam(slot, i, v)
	}
	if v2 := v2At[slot]; v2 >= 0 {
		p.Raw[v2] = 0
		if amp != nil && amp.V2 {
			p.Raw[v2] = 0x20
		}
	}
	cab := params[7]
	if amp != nil {
		switch {
		case amp.GuitarCab && cab != 0:
			p.Raw[cabAt[slot]] = 0x40
		case amp.GuitarCab || cab == 0:
			p.Raw[cabAt[slot]] = 0
		case cab == 16 || cab == 32 || cab == 48 || cab == 96 || cab == 112 || cab == 192:
			p.Raw[cabAt[slot]] = 0x50
		default:
			p.Raw[cabAt[slot]] = 0x51
		}
	} else {
		p.Raw[cabAt[slot]] = 0
	}
	used := 1
	for s := Slots - 1; s > 0; s-- {
		if p.Effect(s).ID != 0 {
			used = s + 1
			break
		}
	}
	p.Raw[maxFxAt] = p.Raw[maxFxAt]&^0x1c | byte(used<<2)
}

// UsedSlots はエフェクトの使用数（ペダルの画面で並んでいる数）。
func (p *Patch) UsedSlots() int { return int(p.Raw[maxFxAt]&0x1c) >> 2 }

// EditSlot は画面で選んでいるスロット（0 始まり）。
func (p *Patch) EditSlot() int {
	d := p.Raw
	return 5 - (int(d[130]&1)<<2 + int(d[125]&8)>>2 + int(d[129]&0x40)>>6)
}

// tempoBits はタップテンポ（BPM）のビット。
var tempoBits = []field{f(125, 0x04, -2), f(130, 0x60, -4), f(131, 0x1f, 3)}

// Tempo はタップテンポ（BPM）。
func (p *Patch) Tempo() int { return get(p.Raw, tempoBits) }

func (p *Patch) SetTempo(bpm int) { set(p.Raw, tempoBits, bpm) }
