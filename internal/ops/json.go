package ops

import (
	"bytes"
	"encoding/json"
)

// JSON は 2 桁インデントで、<> & をエスケープしない（TypeScript 版の JSON.stringify(v, null, 2) に合わせる）。
func JSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// CompactJSON は 1 行の JSON（<> & をエスケープしない）。
func CompactJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}
