package sysex

import "testing"

func TestCheatSheet(t *testing.T) {
	cases := map[string][]byte{
		"f0 52 00 6e 33 f7":                   GetCurrentProgram(0x6e),
		"f0 52 00 6e 64 13 f7":                RequestCurrentPatch(0x6e),
		"f0 52 00 6e 46 00 00 01 00 02 01 f7": RequestPatch(0x6e, 1, 130),
	}
	for want, got := range cases {
		if ToHex(got) != want {
			t.Errorf("%s != %s", ToHex(got), want)
		}
	}
}

func TestParseIdentityReply(t *testing.T) {
	msg, _ := FromHex("f0 7e 00 06 02 52 6e 00 00 00 31 2e 31 30 f7")
	id, err := ParseIdentityReply(msg)
	if err != nil {
		t.Fatal(err)
	}
	if id.Manufacturer != ZoomManufacturer || id.DeviceID != 0x6e || id.Version != "1.10" {
		t.Errorf("%+v", id)
	}
}

func TestRejectNonIdentity(t *testing.T) {
	msg, _ := FromHex("f0 52 00 6e 33 f7")
	if _, err := ParseIdentityReply(msg); err == nil {
		t.Error("弾かない")
	}
}
