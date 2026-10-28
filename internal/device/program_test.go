package device

import "testing"

func TestProgramTap(t *testing.T) {
	for _, c := range []struct {
		name    string
		perBank int
		chunks  [][]byte
		want    []int
	}{
		// MS-80IR+ の 33 の応答（バンク 5、プログラム 1）
		{"bank", 10, [][]byte{{0xb0, 0x00, 0x00, 0xb0, 0x20, 0x05, 0xc0, 0x01}}, []int{51}},
		// 受信の切れ目がメッセージの途中に来る
		{"split", 10, [][]byte{{0xb0, 0x00}, {0x00, 0xb0, 0x20}, {0x1d, 0xc0}, {0x09}}, []int{299}},
		// ランニングステータスと、間に挟まる SysEx・リアルタイム
		{"running", 50, [][]byte{{0xc0, 0x03, 0xf8, 0x04, 0xf0, 0x52, 0x00, 0x61, 0x28, 0xf7, 0xc0, 0x31}}, []int{3, 4, 49}},
		// SysEx の中身を Program Change と読まない
		{"sysex", 50, [][]byte{{0xf0, 0x52, 0x00, 0x61, 0x31, 0x00, 0xf7}}, nil},
	} {
		tap := newProgramTap(c.perBank)
		for _, b := range c.chunks {
			tap.feed(b)
		}
		var got []int
		for len(tap.ch) > 0 {
			got = append(got, <-tap.ch)
		}
		if len(got) != len(c.want) {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%s: %v, want %v", c.name, got, c.want)
			}
		}
	}
}
