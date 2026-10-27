package patch_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/polidog/zm/internal/codec"
	"github.com/polidog/zm/internal/patch"
	"github.com/polidog/zm/internal/testdata"
)

func TestPack7(t *testing.T) {
	data := make([]byte, 50)
	for i := range data {
		data[i] = byte(i * 37)
	}
	if got := codec.Unpack7(codec.Pack7(data), len(data)); !bytes.Equal(got, data) {
		t.Errorf("%x != %x", got, data)
	}
}

// 実機ダンプのラウンドトリップ: SysEx → 8bit → Patch(JSON) → 8bit → SysEx がバイト一致するか。
func TestRoundtrip(t *testing.T) {
	dir, files := testdata.LatestAll(t)
	t.Logf("%s: %d 件", filepath.Base(dir), len(files))
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			raw, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			loc, buffer, err := codec.DecodePatchSysex(raw)
			if err != nil {
				t.Fatal(err)
			}
			if len(buffer) != patch.BufferBytes {
				t.Fatalf("バッファが %d バイト", len(buffer))
			}
			p, err := patch.Decode(buffer)
			if err != nil {
				t.Fatal(err)
			}
			ids := make([]uint32, len(p.Effects))
			for i, e := range p.Effects {
				ids[i] = e.ID
			}
			if !slices.Equal(ids, p.IDs) {
				t.Errorf("ids %v != effects %v", p.IDs, ids)
			}
			// JSON を通しても戻る
			js, _ := json.Marshal(p)
			var back patch.Patch
			if err := json.Unmarshal(js, &back); err != nil {
				t.Fatal(err)
			}
			rebuilt, err := patch.Encode(&back)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(rebuilt, buffer) {
				t.Fatal("バッファが一致しない")
			}
			if !bytes.Equal(codec.EncodePatchSysex(raw[3], loc, rebuilt), raw) {
				t.Fatal("SysEx が一致しない")
			}
		})
	}
}

func TestCurrentDumps(t *testing.T) {
	files := testdata.CurrentDumps()
	if len(files) == 0 {
		t.Skip("backups/*-current-patch.syx がない")
	}
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			raw, _ := os.ReadFile(file)
			loc, buffer, err := codec.DecodePatchSysex(raw)
			if err != nil {
				t.Fatal(err)
			}
			if !loc.Current {
				t.Fatal("64 12 ではない")
			}
			p, err := patch.Decode(buffer)
			if err != nil {
				t.Fatal(err)
			}
			rebuilt, _ := patch.Encode(p)
			if !bytes.Equal(codec.EncodePatchSysex(raw[3], loc, rebuilt), raw) {
				t.Fatal("SysEx が一致しない")
			}
		})
	}
}
