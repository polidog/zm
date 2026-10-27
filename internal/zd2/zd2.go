// Package zd2 は ZD2 (エフェクトファイル) からカタログに要る情報を抜き出す (L3)。
//
//	ZDLF | headerLen u32 | checksum u32 | target u32 | ?[73] | version[4] | 00 00 | group u8 | id u32 | name[11] | groupName CString
//	... ICON / TXJ1 / TXE1 / INFO / DATA (TI C6x の ELF) / PRMJ / PRME ...
//
// PRME / PRMJ はパラメータ名と説明の JSON（PRMJ は UTF-8、TXJ1 は Shift-JIS）。値の範囲は入っていないので、
// DATA の ELF にあるパラメータ表 (1 項目 56 バイト: name[12] | max u32 | default u32 | ...) から読む。
// 表は OnOff, エフェクト名, param1, param2, ... の順に並ぶ。PRME より後ろの項目 (アンプ系の Dummy など) は
// 画面に出ない隠しパラメータで、パッチにも既定値が入っている。
package zd2

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"golang.org/x/text/encoding/japanese"
)

type Param struct {
	Name          string
	Max           int
	Default       int
	Explanation   string
	ExplanationJa string
	// Hidden は PRME に載っていない（ペダルの画面に出ない）パラメータ。
	Hidden bool
}

type ZD2 struct {
	ID            uint32
	Name          string
	Group         int
	GroupName     string
	Version       string
	Description   string
	DescriptionJa string
	Params        []Param
}

func latin1(b []byte) string {
	rs := make([]rune, len(b))
	for i, x := range b {
		rs[i] = rune(x)
	}
	return string(rs)
}

func cstring(b []byte) string {
	if i := bytes.IndexByte(b, 0); i >= 0 {
		b = b[:i]
	}
	return latin1(b)
}

var tagPattern = regexp.MustCompile(`^[A-Z0-9]{4}$`)

func chunks(data []byte, from int) map[string][]byte {
	out := map[string][]byte{}
	if from < 0 {
		return out
	}
	for off := from; off+8 <= len(data); {
		tag := string(data[off : off+4])
		if !tagPattern.MatchString(tag) {
			break
		}
		n := int(binary.LittleEndian.Uint32(data[off+4:]))
		end := min(off+8+n, len(data))
		out[tag] = data[off+8 : end]
		off += 8 + n
	}
	return out
}

type prm struct {
	Name        string `json:"name"`
	Explanation string `json:"explanation"`
}

// trailingComma は ] や } の直前のカンマ。MS-80IR+ の一部の ZD2（KRMPS_ST, WCANVAS）は配列の最後に付いている。
var trailingComma = regexp.MustCompile(`,(\s*[\]}])`)

func parsePrm(text string) ([]prm, error) {
	// 末尾に NUL やゴミが付くので最後の } までで切る
	end := strings.LastIndex(text, "}")
	if end < 0 {
		return nil, errors.New("PRM の JSON がない")
	}
	var v struct {
		Parameters []prm `json:"Parameters"`
	}
	if err := json.Unmarshal([]byte(trailingComma.ReplaceAllString(text[:end+1], "$1")), &v); err != nil {
		return nil, fmt.Errorf("PRM の JSON: %w", err)
	}
	return v.Parameters, nil
}

const (
	entryBytes = 56
	// maxParams はパッチに入るパラメータの最大数。
	maxParams = 12
)

var printable = regexp.MustCompile(`^[ -~]+$`)

type entry struct {
	name          string
	max, default_ int
}

// paramTable は ELF からパラメータ表を探す。OnOff の項目を起点に 56 バイト刻みで、名前が読める間だけ読む。
func paramTable(elf []byte) ([]entry, error) {
	needle := append([]byte("OnOff"), make([]byte, 7)...)
	for at := 0; at+len(needle) <= len(elf); at++ {
		if !bytes.Equal(elf[at:at+len(needle)], needle) || at+16 > len(elf) || binary.LittleEndian.Uint32(elf[at+12:]) != 1 {
			continue
		}
		var entries []entry
		for i := 2; i < 2+maxParams; i++ {
			off := at + i*entryBytes
			if off+20 > len(elf) {
				break
			}
			name := cstring(elf[off : off+12])
			if !printable.MatchString(name) {
				break
			}
			entries = append(entries, entry{
				name:     name,
				max:      int(binary.LittleEndian.Uint32(elf[off+12:])),
				default_: int(binary.LittleEndian.Uint32(elf[off+16:])),
			})
		}
		return entries, nil
	}
	return nil, errors.New("パラメータ表が見つからない")
}

func Parse(data []byte) (*ZD2, error) {
	if len(data) < 123 || string(data[:4]) != "ZDLF" {
		return nil, errors.New("ZDLF ではない")
	}
	z := &ZD2{
		Version:   latin1(data[89:93]),
		Group:     int(data[95]),
		ID:        binary.LittleEndian.Uint32(data[96:]),
		Name:      cstring(data[100:111]),
		GroupName: cstring(data[111:123]),
	}

	c := chunks(data, bytes.Index(data, []byte("ICON")))
	var prme, prmj []prm
	var err error
	if b, ok := c["PRME"]; ok {
		if prme, err = parsePrm(latin1(b)); err != nil {
			return nil, err
		}
	}
	if b, ok := c["PRMJ"]; ok {
		if prmj, err = parsePrm(string(b)); err != nil {
			return nil, err
		}
	}
	var table []entry
	if elf, ok := c["DATA"]; ok && len(prme) > 0 {
		if table, err = paramTable(elf); err != nil {
			return nil, err
		}
	}
	if len(table) < len(prme) {
		return nil, fmt.Errorf("パラメータ表が PRME より短い: %d < %d", len(table), len(prme))
	}

	z.Description = strings.TrimSpace(cstring(c["TXE1"]))
	ja, err := japanese.ShiftJIS.NewDecoder().Bytes(c["TXJ1"])
	if err != nil {
		ja = c["TXJ1"]
	}
	if i := bytes.IndexByte(ja, 0); i >= 0 {
		ja = ja[:i]
	}
	z.DescriptionJa = strings.TrimSpace(string(ja))

	z.Params = make([]Param, len(table))
	for i, t := range table {
		p := Param{Name: t.name, Max: t.max, Default: t.default_, Hidden: i >= len(prme)}
		if i < len(prme) {
			p.Name = prme[i].Name
			p.Explanation = prme[i].Explanation
		}
		if i < len(prmj) {
			p.ExplanationJa = prmj[i].Explanation
		}
		z.Params[i] = p
	}
	return z, nil
}
