// Package patch は PTCF パッチ ⇔ 構造体 (L2)。
//
// 構造は mungewell/zoom-zt2 の decode_preset.py (MIT) を元にした。
//
//	PTCF | size u32 | version u32 | fxCount u32 | target u32 | misc[6] | shortName[10] | ids u32[fxCount]
//	TXJ1 | len | Shift-JIS        （日本語の説明。空なら len=0）
//	TXE1 | len | ascii            （英語の説明。空なら len=0）
//	EDTB | len | effect[fxCount]  （1 つ 24 バイト）
//	PRM2 | len | 32 バイト         （パッチ全体の設定）
//	NAME | len | ascii            （14 文字まで空白で埋め、NUL 2 つで 16 バイト）
//
// PTCF は 832 バイト固定のバッファの先頭に入り、後ろには前のデータの残骸が残っている。
// 意味の分からないビットは構造体に残し、Encode でバイト一致させる。
// JSON のキーは TypeScript 版と同じにしてあり、出力をそのまま突き合わせられる。
package patch

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"

	"golang.org/x/text/encoding/japanese"
)

// ---- ビット操作 ----

// leBig はバイト列を little-endian の整数として読む。
func leBig(b []byte) *big.Int {
	be := make([]byte, len(b))
	for i, x := range b {
		be[len(b)-1-i] = x
	}
	return new(big.Int).SetBytes(be)
}

func bigLe(v *big.Int, n int) []byte {
	be := v.Bytes()
	out := make([]byte, n)
	for i := 0; i < n && i < len(be); i++ {
		out[i] = be[len(be)-1-i]
	}
	return out
}

func mask(width int) *big.Int {
	return new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(width)), big.NewInt(1))
}

func getField(v *big.Int, at, width int) uint64 {
	return new(big.Int).And(new(big.Int).Rsh(v, uint(at)), mask(width)).Uint64()
}

func setField(v *big.Int, at, width int, x uint64) {
	m := new(big.Int).Lsh(mask(width), uint(at))
	v.AndNot(v, m)
	v.Or(v, new(big.Int).And(new(big.Int).Lsh(new(big.Int).SetUint64(x), uint(at)), m))
}

// ---- EDTB: エフェクト 1 つ = 24 バイト = 192bit ----

// paramWidths は param1..param12 のビット幅。
var paramWidths = [...]int{12, 12, 12, 12, 12, 8, 8, 8, 12, 12, 12, 12}

const (
	effectBytes = 24
	// MaxParams はパッチに入るパラメータの最大数。
	MaxParams = len(paramWidths)
)

var effectKnownBits = func() int {
	n := 1 + 29
	for _, w := range paramWidths {
		n += w
	}
	return n
}()

type Effect struct {
	ID      uint32 `json:"id"`
	Enabled bool   `json:"enabled"`
	Params  []int  `json:"params"`
	// Extra は上位の意味不明ビット（hex）。
	Extra string `json:"extra"`
}

func decodeEffect(b []byte) Effect {
	v := leBig(b)
	e := Effect{Enabled: getField(v, 0, 1) == 1, ID: uint32(getField(v, 1, 29)), Params: make([]int, MaxParams)}
	at := 30
	for i, w := range paramWidths {
		e.Params[i] = int(getField(v, at, w))
		at += w
	}
	e.Extra = new(big.Int).Rsh(v, uint(effectKnownBits)).Text(16)
	return e
}

func encodeEffect(e Effect) ([]byte, error) {
	extra, ok := new(big.Int).SetString(e.Extra, 16)
	if !ok {
		return nil, fmt.Errorf("extra が hex ではない: %q", e.Extra)
	}
	v := new(big.Int).Lsh(extra, uint(effectKnownBits))
	if e.Enabled {
		setField(v, 0, 1, 1)
	}
	setField(v, 1, 29, uint64(e.ID))
	at := 30
	for i, w := range paramWidths {
		p := 0
		if i < len(e.Params) {
			p = e.Params[i]
		}
		setField(v, at, w, uint64(p))
		at += w
	}
	return bigLe(v, effectBytes), nil
}

// ---- PRM2: パッチ全体 32 バイト ----
// 下位ビットからの位置。zoom-zt2 の PPRM_v2_rev を逆順に数えたもの

const prm2Bytes = 32

var prm2Fields = [...]struct{ at, width int }{
	{77, 7},  // patchVolume
	{85, 3},  // editSlot
	{244, 8}, // tempo
}

type Prm2 struct {
	PatchVolume int    `json:"patchVolume"`
	EditSlot    int    `json:"editSlot"`
	Tempo       int    `json:"tempo"`
	Raw         string `json:"raw"`
}

func (p *Prm2) fields() [3]*int { return [3]*int{&p.PatchVolume, &p.EditSlot, &p.Tempo} }

func decodePrm2(b []byte) Prm2 {
	v := leBig(b)
	p := Prm2{Raw: hex.EncodeToString(b)}
	for i, f := range p.fields() {
		*f = int(getField(v, prm2Fields[i].at, prm2Fields[i].width))
	}
	return p
}

func encodePrm2(p Prm2) ([]byte, error) {
	raw, err := hex.DecodeString(p.Raw)
	if err != nil {
		return nil, fmt.Errorf("prm2.raw: %w", err)
	}
	v := leBig(raw)
	for i, f := range p.fields() {
		setField(v, prm2Fields[i].at, prm2Fields[i].width, uint64(*f))
	}
	return bigLe(v, prm2Bytes), nil
}

// ---- PTCF ----

type Patch struct {
	Name    string `json:"name"`
	Version uint32 `json:"version"`
	Target  uint32 `json:"target"`
	// Misc はヘッダ内の 6 バイト（未解析）。
	Misc string `json:"misc"`
	// ShortName はヘッダ内の 10 バイトの名前欄。
	ShortName string `json:"shortName"`
	// IDs はヘッダの ids 欄。実機では常に Effects[].ID と同じで、Encode は Effects から作る。
	IDs []uint32 `json:"ids"`
	// DescriptionJa は TXJ1 の Shift-JIS を読んだもの（表示用）。書き戻しは TXJ1 から。
	DescriptionJa string `json:"descriptionJa"`
	// TXJ1 は TXJ1 の生バイト（hex）。こちらを正とする。
	TXJ1          string   `json:"txj1"`
	DescriptionEn string   `json:"descriptionEn"`
	Effects       []Effect `json:"effects"`
	Prm2          Prm2     `json:"prm2"`
	// Trailer は 832 バイトのバッファのうち PTCF の後ろ（前のデータの残骸, hex）。
	Trailer string `json:"trailer"`
}

type reader struct {
	b   []byte
	off int
	err error
}

func (r *reader) bytes(n int) []byte {
	if r.err != nil {
		return nil
	}
	if n < 0 || r.off+n > len(r.b) {
		r.err = fmt.Errorf("offset %d から %d バイト読めない", r.off, n)
		return nil
	}
	v := r.b[r.off : r.off+n]
	r.off += n
	return v
}

func (r *reader) u32() uint32 {
	b := r.bytes(4)
	if b == nil {
		return 0
	}
	return binary.LittleEndian.Uint32(b)
}

func (r *reader) tag(expect string) {
	t := r.bytes(4)
	if r.err == nil && string(t) != expect {
		r.err = fmt.Errorf("%s のはずが %q (offset %d)", expect, latin1(t), r.off-4)
	}
}

func (r *reader) chunk(tag string) []byte {
	r.tag(tag)
	return r.bytes(int(r.u32()))
}

// latin1 は 1 バイトを 1 文字として読む。
func latin1(b []byte) string {
	rs := make([]rune, len(b))
	for i, x := range b {
		rs[i] = rune(x)
	}
	return string(rs)
}

func encodeLatin1(s string) []byte {
	out := make([]byte, 0, len(s))
	for _, r := range s {
		out = append(out, byte(r))
	}
	return out
}

func stripNul(b []byte) string { return strings.TrimRight(latin1(b), "\x00") }

func decodeSJIS(b []byte) string {
	s, err := japanese.ShiftJIS.NewDecoder().Bytes(b)
	if err != nil {
		return latin1(b)
	}
	return string(s)
}

func Decode(buffer []byte) (*Patch, error) {
	r := &reader{b: buffer}
	r.tag("PTCF")
	size := r.u32()
	version := r.u32()
	if r.err == nil && version < 2 {
		return nil, fmt.Errorf("PTCF version %d は未対応", version)
	}
	fxCount := int(r.u32())
	if fxCount > 64 {
		return nil, fmt.Errorf("エフェクト数がおかしい: %d", fxCount)
	}
	p := &Patch{Version: version}
	p.Target = r.u32()
	p.Misc = hex.EncodeToString(r.bytes(6))
	p.ShortName = stripNul(r.bytes(10))
	p.IDs = make([]uint32, fxCount)
	for i := range p.IDs {
		p.IDs[i] = r.u32()
	}
	txj1 := r.chunk("TXJ1")
	p.DescriptionEn = stripNul(r.chunk("TXE1"))
	edtb := r.chunk("EDTB")
	if r.err == nil && len(edtb) < fxCount*effectBytes {
		return nil, fmt.Errorf("EDTB が短い: %d < %d", len(edtb), fxCount*effectBytes)
	}
	prm2 := r.chunk("PRM2")
	name := r.chunk("NAME")
	if r.err != nil {
		return nil, r.err
	}
	if len(prm2) != prm2Bytes {
		return nil, fmt.Errorf("PRM2 が %d バイトではない: %d", prm2Bytes, len(prm2))
	}
	if r.off != int(size) {
		return nil, fmt.Errorf("PTCF のサイズが合わない: %d != %d", r.off, size)
	}
	p.Effects = make([]Effect, fxCount)
	for i := range p.Effects {
		p.Effects[i] = decodeEffect(edtb[i*effectBytes : (i+1)*effectBytes])
	}
	p.Prm2 = decodePrm2(prm2)
	p.Name = strings.TrimRight(stripNul(name), " \t\n\r")
	p.TXJ1 = hex.EncodeToString(txj1)
	p.DescriptionJa = strings.TrimRight(decodeSJIS(txj1), "\x00")
	p.Trailer = hex.EncodeToString(buffer[size:])
	return p, nil
}

// fit は n バイトの領域に詰め、残りを NUL で埋める。
func fit(b []byte, n int) []byte {
	out := make([]byte, n)
	copy(out, b)
	return out
}

// pad4 は 4 バイト境界まで NUL で埋める（ちょうど割り切れれば NUL は付かない）。
func pad4(b []byte) []byte { return fit(b, (len(b)+3)&^3) }

const (
	NameChars = 14
	nameBytes = 16
	// BufferBytes は PTCF を入れるバッファの大きさ。
	BufferBytes = 832
)

func u32(v uint32) []byte { return binary.LittleEndian.AppendUint32(nil, v) }

func chunk(tag string, body []byte) []byte {
	return append(append([]byte(tag), u32(uint32(len(body)))...), body...)
}

func Encode(p *Patch) ([]byte, error) {
	misc, err := hex.DecodeString(p.Misc)
	if err != nil {
		return nil, fmt.Errorf("misc: %w", err)
	}
	txj1, err := hex.DecodeString(p.TXJ1)
	if err != nil {
		return nil, fmt.Errorf("txj1: %w", err)
	}
	var edtb []byte
	for _, e := range p.Effects {
		b, err := encodeEffect(e)
		if err != nil {
			return nil, err
		}
		edtb = append(edtb, b...)
	}
	prm2, err := encodePrm2(p.Prm2)
	if err != nil {
		return nil, err
	}
	name := []rune(p.Name)
	if len(name) > NameChars {
		name = name[:NameChars]
	}
	padded := string(name) + strings.Repeat(" ", NameChars-len(name))

	var body bytes.Buffer
	body.Write(u32(p.Version))
	body.Write(u32(uint32(len(p.Effects))))
	body.Write(u32(p.Target))
	body.Write(misc)
	body.Write(fit(encodeLatin1(p.ShortName), 10))
	for _, e := range p.Effects {
		body.Write(u32(e.ID))
	}
	body.Write(chunk("TXJ1", txj1))
	body.Write(chunk("TXE1", pad4(encodeLatin1(p.DescriptionEn))))
	body.Write(chunk("EDTB", edtb))
	body.Write(chunk("PRM2", prm2))
	body.Write(chunk("NAME", fit(encodeLatin1(padded), nameBytes)))

	size := 8 + body.Len()
	ptcf := append(append([]byte("PTCF"), u32(uint32(size))...), body.Bytes()...)
	rest := BufferBytes - len(ptcf)
	if rest < 0 {
		return nil, fmt.Errorf("PTCF が %d バイトを超える: %d", BufferBytes, len(ptcf))
	}
	// 残骸はそのまま戻す（ラウンドトリップ用）。PTCF の長さが変わって合わなければ 0 で埋める
	trailer, err := hex.DecodeString(p.Trailer)
	if err != nil || len(trailer) != rest {
		trailer = make([]byte, rest)
	}
	return append(ptcf, trailer...), nil
}

// Clone は深いコピーを返す。
func (p *Patch) Clone() *Patch {
	c := *p
	c.IDs = append([]uint32(nil), p.IDs...)
	c.Effects = make([]Effect, len(p.Effects))
	for i, e := range p.Effects {
		e.Params = append([]int(nil), e.Params...)
		c.Effects[i] = e
	}
	return &c
}
