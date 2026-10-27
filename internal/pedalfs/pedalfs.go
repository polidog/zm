// Package pedalfs はペダル内のファイル（ZD2 など）を読む。手順は mungewell/zoom-zt2 の zoomzt2.py に合わせた。
//
// 使うのは 一覧 (60 25/26)・存在確認 (60 25 → 60 27)・開く (60 20 02)・読む (60 22)・閉じる (60 21, 60 09) だけ。
// 書き込み (60 20 01, 60 23) と削除 (60 24) は実装しない。
package pedalfs

import (
	"bytes"
	"fmt"
	"hash/crc32"
	"time"

	"github.com/polidog/zm/internal/codec"
	"github.com/polidog/zm/internal/midi"
	"github.com/polidog/zm/internal/sysex"
)

func ascii(s string) []byte { return append([]byte(s), 0x00) }

type Files struct {
	conn *midi.Conn
	dev  byte
}

func New(conn *midi.Conn, dev byte) *Files { return &Files{conn: conn, dev: dev} }

// req は送って応答を待つ。60 xx には 60 xx、それ以外 (52 / 53) には 00 00 だけを応答とみなし、
// 前の接続の応答の残りなどを取り違えないようにする。
func (f *Files) req(body ...byte) ([]byte, error) {
	msg := sysex.Zoom(f.dev, body...)
	want := []byte{0x00, 0x00}
	if body[0] == 0x60 {
		want = []byte{0x60}
	}
	reply, err := f.conn.Request(msg, 3*time.Second, func(m []byte) bool {
		return len(m) > 4+len(want) && bytes.HasPrefix(m[4:], want)
	})
	if err != nil {
		return nil, err
	}
	if reply == nil {
		return nil, fmt.Errorf("応答なし: %s", sysex.ToHex(msg))
	}
	return reply, nil
}

// PCModeOn はファイル操作の前に送る。終わったら PCModeOff。
func (f *Files) PCModeOn() error  { _, err := f.req(0x52); return err }
func (f *Files) PCModeOff() error { _, err := f.req(0x53); return err }

// List はペダル内の全ファイル名。
func (f *Files) List() ([]string, error) {
	var names []string
	for first := true; ; first = false {
		op := byte(0x26)
		if first {
			op = 0x25
		}
		reply, err := f.req(append([]byte{0x60, op, 0x00, 0x00}, ascii("*")...)...)
		if err != nil {
			return nil, err
		}
		// 応答: F0 52 00 dev 60 04 ... name は 15 バイト目から 12 バイト（8.3 形式）で、短ければ NUL で終わる。
		// すぐ後ろのバイトは 0 とは限らない（MS-80IR+ はパッチを読み出した後に 02 になる）ので、12 バイトで切る
		if len(reply) <= 15 || reply[5] != 0x04 {
			break
		}
		rest := reply[15:min(15+12, len(reply)-1)]
		if i := bytes.IndexByte(rest, 0); i >= 0 {
			rest = rest[:i]
		}
		if len(rest) == 0 {
			break
		}
		names = append(names, string(rest))
	}
	return names, nil
}

func (f *Files) Exists(name string) (bool, error) {
	if _, err := f.req(append([]byte{0x60, 0x25, 0x00, 0x00}, ascii(name)...)...); err != nil {
		return false, err
	}
	status, err := f.req(0x60, 0x05, 0x00)
	if err != nil {
		return false, err
	}
	if _, err := f.req(0x60, 0x27); err != nil {
		return false, err
	}
	if len(status) < 6 {
		return false, nil
	}
	for _, b := range status[len(status)-6 : len(status)-1] {
		if b != 0 {
			return false, nil
		}
	}
	return true, nil
}

func (f *Files) Download(name string) (data []byte, err error) {
	ok, err := f.Exists(name)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("%s がペダルにない", name)
	}
	open := append([]byte{0x60, 0x20, 0x02, 0, 0, 0, 0, 0, 0, 0, 0, 0}, ascii(name)...)
	// zoomzt2.py は同じ open を 2 回送っている。手順をそのままなぞる
	for range 2 {
		if _, err := f.req(open...); err != nil {
			return nil, err
		}
	}
	defer func() {
		if cerr := f.close(); err == nil {
			err = cerr
		}
	}()
	var out []byte
	for {
		if _, err := f.req(0x60, 0x05, 0x00); err != nil {
			return nil, err
		}
		if _, err := f.req(0x60, 0x22, 0x14, 0x2f, 0x60, 0x00, 0x0c, 0x00, 0x04, 0x00, 0x00, 0x00); err != nil {
			return nil, err
		}
		reply, err := f.req(0x60, 0x05, 0x00)
		if err != nil {
			return nil, err
		}
		// F0 52 00 dev 60 04 ?? ?? ?? lenL lenM | packed | crc(5) | F7
		if len(reply) < 17 || reply[5] != 0x04 {
			break
		}
		length := sysex.Join14(reply[9], reply[10])
		if length == 0 {
			break
		}
		block := codec.Unpack7(reply[11:len(reply)-6], length)
		if codec.Join35(reply[len(reply)-6:len(reply)-1])^0xffffffff != crc32.ChecksumIEEE(block) {
			return nil, fmt.Errorf("%s: CRC が合わない", name)
		}
		out = append(out, block...)
	}
	return out, nil
}

func (f *Files) close() error {
	if _, err := f.req(0x60, 0x21, 0x40, 0x00, 0x00, 0x00, 0x00); err != nil {
		return err
	}
	_, err := f.req(0x60, 0x09)
	return err
}
