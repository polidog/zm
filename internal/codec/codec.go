// Package codec はパッチ SysEx の枠 (L2): 7bit パックと CRC。
//
// 応答の形（G2 FOUR で確認）:
//
//	45: F0 52 00 <dev> 45 00 00 bL bM pL pM | lenL lenM | packed(len) | crc(5) | F7
//	64: F0 52 00 <dev> 64 12 01             | lenL lenM | packed(len) | crc(5) | F7
//
// len は 8bit に戻した後のバイト数（G2 FOUR は常に 832）。
// crc は ~CRC32(8bit データ) を 7bit ずつ下位から 5 バイトに分けたもの。
package codec

import (
	"bytes"
	"errors"
	"fmt"
	"hash/crc32"

	"github.com/polidog/zm/internal/sysex"
)

// Pack7 は 7 バイトごとに先頭へ MSB をまとめる。先頭バイトの bit6 が 1 つ目のデータに対応する。
func Pack7(data []byte) []byte {
	out := make([]byte, 0, PackedLength(len(data)))
	for i := 0; i < len(data); i += 7 {
		group := data[i:min(i+7, len(data))]
		var hi byte
		for j, b := range group {
			if b&0x80 != 0 {
				hi |= 1 << (6 - j)
			}
		}
		out = append(out, hi)
		for _, b := range group {
			out = append(out, b&0x7f)
		}
	}
	return out
}

// Unpack7 は Pack7 の逆。packed が足りなければ足りない分を 0 とする。
func Unpack7(packed []byte, length int) []byte {
	at := func(i int) byte {
		if i < len(packed) {
			return packed[i]
		}
		return 0
	}
	out := make([]byte, length)
	n := 0
	for i := 0; n < length; i += 8 {
		hi := at(i)
		for j := 0; j < 7 && n < length; j++ {
			b := at(i + 1 + j)
			if hi&(1<<(6-j)) != 0 {
				b |= 0x80
			}
			out[n] = b
			n++
		}
	}
	return out
}

func PackedLength(n int) int {
	l := n / 7 * 8
	if n%7 != 0 {
		l += n%7 + 1
	}
	return l
}

// CRC7 は ~CRC32 を 7bit ずつ下位から 5 バイトに分ける（最後のバイトは上位 4bit だけ）。
func CRC7(data []byte) []byte {
	return sysex.Split35(^crc32.ChecksumIEEE(data))
}

// Join35 は CRC7 の 5 バイトを 32bit に戻す。
func Join35(b []byte) uint32 {
	return uint32(b[0]) | uint32(b[1])<<7 | uint32(b[2])<<14 | uint32(b[3])<<21 | uint32(b[4]&0x0f)<<28
}

// Location はパッチ SysEx の宛先。Current なら現在パッチ (64 12)、そうでなければパッチメモリ (45)。
type Location struct {
	Current bool
	Bank    int
	Program int
}

// DecodePatchSysex はパッチ SysEx を 8bit のバッファへ戻す。CRC が合わなければエラー。
func DecodePatchSysex(msg []byte) (Location, []byte, error) {
	if len(msg) < 8 || msg[0] != 0xf0 || msg[1] != sysex.ZoomManufacturer || msg[len(msg)-1] != 0xf7 {
		return Location{}, nil, errors.New("ZOOM の SysEx ではない")
	}
	var loc Location
	var off int
	switch {
	case msg[4] == 0x45 && len(msg) >= 14:
		loc = Location{Bank: sysex.Join14(msg[7], msg[8]), Program: sysex.Join14(msg[9], msg[10])}
		off = 11
	case msg[4] == 0x64 && msg[5] == 0x12:
		loc = Location{Current: true}
		off = 7
	default:
		return Location{}, nil, fmt.Errorf("パッチの SysEx ではない: %02x", msg[4])
	}
	length := sysex.Join14(msg[off], msg[off+1])
	packed := msg[off+2 : len(msg)-1]
	pl := PackedLength(length)
	if len(packed) != pl+5 {
		return Location{}, nil, fmt.Errorf("長さが合わない: %d != %d + 5", len(packed), pl)
	}
	buffer := Unpack7(packed, length)
	if !bytes.Equal(CRC7(buffer), packed[pl:]) {
		return Location{}, nil, errors.New("CRC が合わない")
	}
	return loc, buffer, nil
}

func EncodePatchSysex(dev byte, loc Location, buffer []byte) []byte {
	out := []byte{0xf0, sysex.ZoomManufacturer, 0x00, dev}
	if loc.Current {
		out = append(out, 0x64, 0x12, 0x01)
	} else {
		b, p := sysex.Split14(loc.Bank), sysex.Split14(loc.Program)
		out = append(out, 0x45, 0x00, 0x00, b[0], b[1], p[0], p[1])
	}
	l := sysex.Split14(len(buffer))
	out = append(out, l[0], l[1])
	out = append(out, Pack7(buffer)...)
	out = append(out, CRC7(buffer)...)
	return append(out, 0xf7)
}
