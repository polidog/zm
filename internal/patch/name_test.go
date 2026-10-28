package patch_test

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/polidog/zm/internal/codec"
	"github.com/polidog/zm/internal/patch"
	"github.com/polidog/zm/internal/testdata"
)

// MS-80IR+ の実機ダンプの名前を変えて Encode → Decode し、名前だけが変わっているか。
func TestSetNameMS80IRPlus(t *testing.T) {
	_, files := testdata.LatestAllIn(t, "ms80irplus")
	raw, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	_, buffer, err := codec.DecodePatchSysex(raw)
	if err != nil {
		t.Fatal(err)
	}
	p, err := patch.Decode(buffer)
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"", "   ", strings.Repeat("x", 29), "ジャズ", "a\tb"} {
		if err := p.Clone().SetName(bad); err == nil {
			t.Errorf("%q が通った", bad)
		}
	}
	for _, name := range []string{"Jazz", "GLAY 90s", "I Shot The    Sheriff", strings.Repeat("x", 28)} {
		q := p.Clone()
		if err := q.SetName(name); err != nil {
			t.Fatal(err)
		}
		b, err := patch.Encode(q)
		if err != nil {
			t.Fatal(err)
		}
		if len(b) != len(buffer) {
			t.Fatalf("%q: %d バイト（元は %d）", name, len(b), len(buffer))
		}
		back, err := patch.Decode(b)
		if err != nil {
			t.Fatal(err)
		}
		if back.Name != name || back.ShortName != fmt.Sprintf("%-10s", name[:min(len(name), 10)]) {
			t.Errorf("%q: 読み直すと %q / %q", name, back.Name, back.ShortName)
		}
		back.Name, back.ShortName = p.Name, p.ShortName
		b2, _ := patch.Encode(back)
		if orig, _ := patch.Encode(p); string(b2) != string(orig) {
			t.Errorf("%q: 名前のほかも変わった", name)
		}
	}
}
