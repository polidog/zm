package cli

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/polidog/zoom-ai/internal/backups"
	"github.com/polidog/zoom-ai/internal/midi"
	"github.com/polidog/zoom-ai/internal/paths"
	"github.com/polidog/zoom-ai/internal/sysex"
)

func hexdump(data []byte) string {
	var lines []string
	for i := 0; i < len(data); i += 16 {
		lines = append(lines, "    "+sysex.ToHex(data[i:min(i+16, len(data))]))
	}
	return strings.Join(lines, "\n")
}

// Probe は P0 の疎通確認: Identity Request → 現在プログラム → 現在パッチのダンプ。
//
// ペダルに書き込むメッセージは送らない（エディタモードの ON/OFF を除く）。受け取った生バイトは backups/ に保存する。
func Probe(args []string) error {
	fs := flag.NewFlagSet("probe", flag.ExitOnError)
	portSpec := fs.String("port", "", "hw:C,D（省略で ZOOM を探す）")
	editorMode := fs.Bool("editor-mode", false, "エディタモードを ON にしてから読む")
	out := fs.String("out", paths.Backups(), "生バイトの保存先")
	fs.Parse(args)

	fmt.Println("rawmidi ports:")
	ports, _ := midi.ListPorts()
	for _, p := range ports {
		fmt.Printf("  hw:%d,%d  %s\n", p.Card, p.Device, p.Name)
	}

	var port midi.Port
	if *portSpec != "" {
		p, err := midi.ParsePort(*portSpec)
		if err != nil {
			return err
		}
		port = p
	} else {
		p, ok := midi.FindZoomPort()
		if !ok {
			return fmt.Errorf("ZOOM のポートが見つからない。USB を繋いで電源を入れてから再実行する")
		}
		port = p
	}
	fmt.Printf("\nusing %s (%s)\n", port.Path(), port.Name)

	stamp := backups.Timestamp(time.Now())
	if err := os.MkdirAll(*out, 0o755); err != nil {
		return err
	}
	save := func(label string, data []byte) error {
		path := filepath.Join(*out, stamp+"-"+label+".syx")
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return err
		}
		fmt.Printf("  saved %s (%d bytes)\n", path, len(data))
		return nil
	}

	conn, err := midi.Open(port)
	if err != nil {
		return err
	}
	defer conn.Close()

	fmt.Println("\n[1] Identity Request")
	reply, err := conn.Request(sysex.IdentityRequest, 2*time.Second, nil)
	if err != nil {
		return err
	}
	if reply == nil {
		return fmt.Errorf("応答なし。ポート違いか、Identity Request に答えない機種")
	}
	fmt.Println(hexdump(reply))
	if err := save("identity", reply); err != nil {
		return err
	}
	ident, err := sysex.ParseIdentityReply(reply)
	if err != nil {
		return err
	}
	fmt.Printf("  manufacturer=0x%02X family=0x%04X model=0x%04X version=%q\n",
		ident.Manufacturer, ident.Family, ident.Model, ident.Version)
	if ident.Manufacturer != sysex.ZoomManufacturer {
		return fmt.Errorf("ZOOM (0x52) ではない")
	}
	dev := ident.DeviceID
	known := "（既知の 6E と違う）"
	if dev == sysex.DefaultDeviceID {
		known = "（MS+ / G1 FOUR と同じ）"
	}
	fmt.Printf("  device id = 0x%02X %s\n", dev, known)

	if *editorMode {
		// 50 には 00 00 の ACK が返る。待たないと次の応答と取り違える
		ack, _ := conn.Request(sysex.EditorModeOn(dev), 2*time.Second, nil)
		fmt.Printf("\n[*] editor mode on: %s\n", orNone(ack))
		defer func() {
			ack, _ := conn.Request(sysex.EditorModeOff(dev), 2*time.Second, nil)
			fmt.Printf("\n[*] editor mode off: %s\n", orNone(ack))
		}()
	}

	if dev == sysex.LegacyDeviceID {
		return probeLegacy(conn, dev, save)
	}

	fmt.Println("\n[2] 現在のバンク / プログラム (33)")
	reply, err = conn.Request(sysex.GetCurrentProgram(dev), 2*time.Second, nil)
	if err != nil {
		return err
	}
	if reply == nil {
		fmt.Println("  応答なし（G2 FOUR は答えない）")
	} else {
		fmt.Println(hexdump(reply))
		if err := save("program", reply); err != nil {
			return err
		}
	}

	fmt.Println("\n[3] 現在パッチのダンプ (64 13)")
	reply, err = conn.Request(sysex.RequestCurrentPatch(dev), 4*time.Second, nil)
	if err != nil {
		return err
	}
	if reply == nil {
		fmt.Println("  応答なし。--editor-mode を付けて再実行してみる。")
		return nil
	}
	fmt.Println(hexdump(reply[:min(64, len(reply))]))
	if len(reply) > 64 {
		fmt.Println("    ...")
	}
	tag := "想定外のヘッダ"
	if reply[4] == 0x64 && reply[5] == 0x12 {
		tag = "ok (64 12)"
	}
	fmt.Printf("  %d bytes, %s\n", len(reply), tag)
	return save("current-patch", reply)
}

// probeLegacy は旧 MS (58) 向け。--editor-mode に関わらずエディタモードを ON にする（33 と 29 はエディタモード中しか答えない）。
// 33 には SysEx 以外で返る可能性があるので、受信の生バイトをそのまま見る。
func probeLegacy(conn *midi.Conn, dev byte, save func(string, []byte) error) error {
	var mu sync.Mutex
	var raw []byte
	conn.SetTap(func(b []byte) {
		mu.Lock()
		raw = append(raw, b...)
		mu.Unlock()
	})
	takeRaw := func() []byte {
		mu.Lock()
		defer mu.Unlock()
		out := raw
		raw = nil
		return out
	}

	fmt.Println("\n[2] 現在のプログラム (33)、SysEx 以外も含めて 2 秒待つ")
	takeRaw()
	if err := conn.Send(sysex.GetCurrentProgram(dev)); err != nil {
		return err
	}
	time.Sleep(2 * time.Second)
	if got := takeRaw(); len(got) == 0 {
		fmt.Println("  応答なし")
	} else {
		fmt.Println(hexdump(got))
		if err := save("program-raw", got); err != nil {
			return err
		}
	}
	conn.Drain()

	// 51 (OFF) は送らない。旧 MS は 51 で未保存の編集を捨てる
	ack, err := conn.Request(sysex.EditorModeOn(dev), 2*time.Second, nil)
	if err != nil {
		return err
	}
	fmt.Printf("\n[*] editor mode on (50): %s\n", orNone(ack))

	fmt.Println("\n[3] 現在のプログラム (33)、エディタモード中")
	takeRaw()
	if err := conn.Send(sysex.GetCurrentProgram(dev)); err != nil {
		return err
	}
	time.Sleep(2 * time.Second)
	if got := takeRaw(); len(got) == 0 {
		fmt.Println("  応答なし")
	} else {
		fmt.Println(hexdump(got))
		if err := save("program-editor-raw", got); err != nil {
			return err
		}
	}
	conn.Drain()

	fmt.Println("\n[4] 現在パッチのダンプ (29)")
	reply, err := conn.Request(sysex.LegacyRequestCurrentPatch(dev), 4*time.Second, nil)
	if err != nil {
		return err
	}
	if reply == nil {
		fmt.Println("  応答なし")
		return nil
	}
	fmt.Println(hexdump(reply))
	tag := "想定外のヘッダ"
	if len(reply) > 4 && reply[4] == 0x28 {
		tag = "ok (28)"
	}
	fmt.Printf("  %d bytes, %s\n", len(reply), tag)
	return save("current-patch", reply)
}

func orNone(b []byte) string {
	if b == nil {
		return "応答なし"
	}
	return sysex.ToHex(b)
}
