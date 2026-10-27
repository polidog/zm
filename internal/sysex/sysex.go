// Package sysex は SysEx メッセージの組み立てと、Identity Reply の解釈。
//
// バイト列は MS+ / Four 系で確認されているもの（thammer/zoom-explorer, mungewell/zoom-zt2）。
package sysex

import (
	"fmt"
	"strconv"
	"strings"
)

const ZoomManufacturer = 0x52

// DefaultDeviceID は MS-50G+ / G1 FOUR で共通の機種 ID。実機の Identity Reply から取れればそちらを優先する。
const DefaultDeviceID = 0x6e

var IdentityRequest = []byte{0xf0, 0x7e, 0x7f, 0x06, 0x01, 0xf7}

func Zoom(dev byte, body ...byte) []byte {
	out := []byte{0xf0, ZoomManufacturer, 0x00, dev}
	out = append(out, body...)
	return append(out, 0xf7)
}

func EditorModeOn(dev byte) []byte      { return Zoom(dev, 0x50) }
func EditorModeOff(dev byte) []byte     { return Zoom(dev, 0x51) }
func GetCurrentProgram(dev byte) []byte { return Zoom(dev, 0x33) }

// RequestCurrentPatch の応答は 64 12 01 で始まる。
func RequestCurrentPatch(dev byte) []byte { return Zoom(dev, 0x64, 0x13) }

func Split14(v int) [2]byte { return [2]byte{byte(v & 0x7f), byte((v >> 7) & 0x7f)} }

func Join14(l, m byte) int { return int(l) | int(m)<<7 }

func RequestPatch(dev byte, bank, program int) []byte {
	b, p := Split14(bank), Split14(program)
	return Zoom(dev, 0x46, 0x00, 0x00, b[0], b[1], p[0], p[1])
}

// ---- 書き込み系（thammer/zoom-explorer の ZoomDevice.ts に合わせた） ----

// Split35 は値を 7bit × 5 に分ける。
func Split35(v uint32) []byte {
	out := make([]byte, 5)
	for i := range out {
		out[i] = byte((v >> (7 * i)) & 0x7f)
	}
	return out
}

// SetParameter は現在パッチのパラメータを 1 つ変える（保存しない）。応答は 64 20 01 ...
// paramNo: 0 = ON/OFF, 1 = エフェクト ID（ペダル側で既定値が入る）, 2.. = param1..
func SetParameter(dev byte, slot, paramNo int, value uint32) []byte {
	return Zoom(dev, append([]byte{0x64, 0x20, 0x00, byte(slot), byte(paramNo)}, Split35(value)...)...)
}

// SetAutoSave は本体設定の AUTO SAVE。OFF にしておかないと編集がそのまま保存される。
func SetAutoSave(dev byte, on bool) []byte {
	v := uint32(0)
	if on {
		v = 1
	}
	return Zoom(dev, append([]byte{0x64, 0x20, 0x00, 0x64, 0x0f}, Split35(v)...)...)
}

// ---- 旧 MS（MS-50G など、デバイス ID 58）----

// LegacyDeviceID は MS+ より前の MS シリーズの MS-50G の機種 ID。MS-70CDR は MS70CDRDeviceID。
// メッセージはどちらも同じで、4 バイト目の機種 ID だけが違う（midimessage.md）。
const (
	LegacyDeviceID  = 0x58
	MS70CDRDeviceID = 0x61
)

// 旧 MS のメッセージは g200kg/zoom-ms-utility の midimessage.md と、MS-50G (ファームウェア 3.10) の実機で確かめたもの。
// 50 / 51（エディタモード）には応答がない。33 と 29 はエディタモード中しか答えない。

// LegacyRequestCurrentPatch は現在パッチ（編集中の中身）の要求。応答は 28 で始まる 146 バイト。
// 応答と同じ形の 28 を送ると、現在パッチの中身がそれに置き換わる（保存はしない）。
func LegacyRequestCurrentPatch(dev byte) []byte { return Zoom(dev, 0x29) }

// LegacySetParameter は現在パッチのパラメータを 1 つ変える（保存しない）。応答はない。
// paramNo: 0 = ON/OFF, 2.. = パラメータ 1..9。値は 14bit。
func LegacySetParameter(dev byte, slot, paramNo, value int) []byte {
	v := Split14(value)
	return Zoom(dev, 0x31, byte(slot), byte(paramNo), v[0], v[1])
}

// LegacyStore は現在パッチをパッチメモリ n（0 始まり）に保存する。応答は 00 で始まる。
func LegacyStore(dev byte, n int) []byte {
	return Zoom(dev, 0x32, 0x01, 0x00, 0x00, byte(n), 0x00, 0x00, 0x00, 0x00, 0x00)
}

// ProgramChange はパッチを選ぶ（チャンネル 1）。旧 MS の 33 にもこの形で返ってくる（前に CC0 / CC32 が付く）。
func ProgramChange(n int) []byte { return []byte{0xc0, byte(n & 0x7f)} }

// RequestPatchInfo の応答 43: パッチ数・パッチ長・バンク数など。
func RequestPatchInfo(dev byte) []byte { return Zoom(dev, 0x44) }

// 28（現在パッチの丸ごと送信）は作らない。G2 FOUR ではエフェクトが DSP に読み込まれず、
// パラメータが 0 のまま 64 20 での変更も受け付けなくなる。

type Identity struct {
	Manufacturer int
	Family       int
	Model        int
	Version      string
	// DeviceID は Zoom 独自メッセージの 4 バイト目に入れる機種 ID（family の下位バイト）。
	DeviceID byte
}

func ToHex(data []byte) string {
	parts := make([]string, len(data))
	for i, b := range data {
		parts[i] = fmt.Sprintf("%02x", b)
	}
	return strings.Join(parts, " ")
}

func FromHex(s string) ([]byte, error) {
	fields := strings.Fields(s)
	out := make([]byte, len(fields))
	for i, f := range fields {
		v, err := strconv.ParseUint(f, 16, 8)
		if err != nil {
			return nil, err
		}
		out[i] = byte(v)
	}
	return out, nil
}

func IsIdentityReply(msg []byte) bool {
	return len(msg) >= 15 && msg[0] == 0xf0 && msg[1] == 0x7e && msg[3] == 0x06 && msg[4] == 0x02
}

func ParseIdentityReply(msg []byte) (Identity, error) {
	// F0 7E <ch> 06 02 <mfr> <fam L> <fam M> <model L> <model M> <ver x4> F7
	if !IsIdentityReply(msg) {
		return Identity{}, fmt.Errorf("Identity Reply ではない: %s", ToHex(msg))
	}
	family := Join14(msg[6], msg[7])
	return Identity{
		Manufacturer: int(msg[5]),
		Family:       family,
		Model:        Join14(msg[8], msg[9]),
		Version:      string(msg[10:14]),
		DeviceID:     byte(family & 0x7f),
	}, nil
}
