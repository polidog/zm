package device

import (
	"testing"

	"github.com/polidog/zoom-ai/internal/legacypatch"
	"github.com/polidog/zoom-ai/internal/sysex"
)

// legacyEmpty は zoom-ms-utility の apatch.js にある MS-50G の空パッチ。
func legacyEmpty() []byte {
	b := make([]byte, legacypatch.Size)
	copy(b, []byte{0xf0, 0x52, 0x00, 0x58, 0x28})
	b[26], b[67] = 0x01, 0x01
	copy(b[129:], []byte{0x40, 0x05, 0x0f, 0x45, 0x00, 0x6d, 0x70, 0x74, 0x79, 0x20, 0x20, 0x20, 0x00, 0x20, 0x20, 0x00, 0xf7})
	return b
}

// ペダルのつまみを回したときの 31 が、最後に読んだ現在パッチに当たるか。
func TestLegacyNotice(t *testing.T) {
	lp, err := legacypatch.Parse(legacyEmpty())
	if err != nil {
		t.Fatal(err)
	}
	lp.SetEffect(0, 0x00100002, [legacypatch.MaxParams]int{6, 6, 100}, nil)
	z := &Zoom{Identity: sysex.Identity{DeviceID: sysex.LegacyDeviceID}, legacy: &legacyState{last: lp.Raw}}

	p, ok := legacy{}.notice(z, sysex.LegacySetParameter(sysex.LegacyDeviceID, 0, 4, 130))
	if !ok || p.Name != "Empty" || p.Effects[0].ID != 0x00100002 || p.Effects[0].Params[2] != 130 {
		t.Fatalf("ok=%v %+v", ok, p)
	}
	if p, ok = (legacy{}).notice(z, sysex.LegacySetParameter(sysex.LegacyDeviceID, 0, 0, 1)); !ok || !p.Effects[0].Enabled {
		t.Errorf("ON にならない: %+v", p)
	}
	if _, ok := (legacy{}).notice(z, sysex.Zoom(sysex.DefaultDeviceID, 0x64, 0x12, 0x00)); ok {
		t.Error("ほかの機種のメッセージを拾った")
	}
}

func TestModelByKey(t *testing.T) {
	for in, want := range map[string]string{"ms50g": "ms50g", "MS-50G": "ms50g", "ZOOM MS-50G": "ms50g", "g2four": "g2four", "G2 FOUR": "g2four", "MS-70CDR": "ms70cdr"} {
		if m, ok := ModelByKey(in); !ok || m.Key != want {
			t.Errorf("%q → %q %v", in, m.Key, ok)
		}
	}
	if _, ok := ModelByKey("ms60b"); ok {
		t.Error("ms60b は対応していない")
	}
}

func TestMS70CDRIsLegacy(t *testing.T) {
	m, ok := ModelByID(sysex.MS70CDRDeviceID)
	if !ok || !m.Legacy || m.protocol() != (legacy{}) {
		t.Fatalf("%+v", m)
	}
	msg := legacyEmpty()
	msg[3] = sysex.MS70CDRDeviceID
	if p, err := DecodeSysex(msg); err != nil || p.Name != "Empty" {
		t.Errorf("%v %v", p, err)
	}
}
