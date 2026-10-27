// Package mcpserver は MCP サーバー (L4, stdio)。
//
// 番号はペダルの画面に合わせる: パッチは 1〜パッチ数（G2 FOUR は 300、旧 MS は 50）、スロットは 1〜6。
// set_effect などの編集は「いま鳴っているパッチ」への変更で、保存しない（パッチを選び直せば元に戻る）。
// 保存は save_patch だけで、上書きされる中身を backups/overwritten/ に退避してから書く。
// 曖昧な指示（「80 年代っぽいクリーンに」）の解釈は Claude 側に任せ、ここは素直な操作だけを出す。
//
// ツール名・引数・返す JSON は TypeScript 版の src/server.ts と同じ。
package mcpserver

import (
	"context"
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/polidog/zoom-ai/internal/catalog"
	"github.com/polidog/zoom-ai/internal/device"
	"github.com/polidog/zoom-ai/internal/ops"
)

// model は引数の範囲（パッチ数・スロット数）とカタログを決める機種。New で、--pedal か繋がっているペダルから決める。
// 範囲は ops でも繋いだ機種で確かめ直す。
var model = device.G2Four

func text(v any) (*mcp.CallToolResult, any, error) {
	b, err := ops.JSON(v)
	if err != nil {
		return nil, nil, err
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(b)}}}, nil, nil
}

// handle は ops の結果を JSON の文字列で返す。エラーはツールのエラー (isError) にする。
func handle[In, Out any](fn func(In) (Out, error)) mcp.ToolHandlerFor[In, any] {
	return func(_ context.Context, _ *mcp.CallToolRequest, in In) (*mcp.CallToolResult, any, error) {
		v, err := fn(in)
		if err != nil {
			return nil, nil, err
		}
		return text(v)
	}
}

func withZoom[T any](edit bool, fn func(*device.Zoom) (T, error)) (T, error) {
	return ops.WithZoom(edit, fn)
}

func ptr[T any](v T) *T { return &v }

// schema は In から JSON Schema を作り、edit で範囲などを書き足す。
func schema[In any](edit func(props map[string]*jsonschema.Schema)) *jsonschema.Schema {
	s, err := jsonschema.For[In](nil)
	if err != nil {
		panic(err)
	}
	if edit != nil {
		edit(s.Properties)
	}
	return s
}

func intRange(s *jsonschema.Schema, lo, hi int) {
	s.Types = nil // *int だと ["null", "integer"] になる
	s.Type = "integer"
	s.Minimum = ptr(float64(lo))
	if hi > 0 {
		s.Maximum = ptr(float64(hi))
	}
}

type noArgs struct{}

type numberArg struct {
	Number *int `json:"number,omitempty" jsonschema:"パッチ番号 1〜パッチ数（G2 FOUR は 300、旧 MS は 50。ペダルの画面と同じ）"`
}

type requiredNumberArg struct {
	Number int `json:"number" jsonschema:"パッチ番号 1〜パッチ数（G2 FOUR は 300、旧 MS は 50。ペダルの画面と同じ）"`
}

type groupArg struct {
	Group string `json:"group,omitempty" jsonschema:"DYNAMICS / FILTER / DRIVE / AMP / MODULATION / SFX / DELAY / REVERB / PEDAL で絞る"`
}

type effectArg struct {
	Effect string `json:"effect" jsonschema:"エフェクト名（例: TS Drive）または ID（例: 0x03000030）"`
}

type setEffectArgs struct {
	Slot   int    `json:"slot" jsonschema:"スロット番号 1〜6（信号の流れの順）"`
	Effect string `json:"effect" jsonschema:"エフェクト名（例: TS Drive）または ID"`
}

type setParamArgs struct {
	Slot  int    `json:"slot" jsonschema:"スロット番号 1〜6（信号の流れの順）"`
	Param string `json:"param" jsonschema:"パラメータ名（get_patch / describe_effect に出るもの。大文字小文字は問わない）"`
	Value int    `json:"value"`
}

type toggleArgs struct {
	Slot    int   `json:"slot" jsonschema:"スロット番号 1〜6（信号の流れの順）"`
	Enabled *bool `json:"enabled,omitempty"`
}

func slotRange(p map[string]*jsonschema.Schema) { intRange(p["slot"], 1, model.UserSlots) }
func numberRange(p map[string]*jsonschema.Schema) {
	intRange(p["number"], 1, model.Patches)
}

func New(version string) *mcp.Server {
	model = ops.Model()
	s := mcp.NewServer(&mcp.Implementation{Name: "zm", Version: version}, nil)
	readOnly := &mcp.ToolAnnotations{ReadOnlyHint: true}
	destructive := &mcp.ToolAnnotations{DestructiveHint: ptr(true)}

	// ---- 読み出し ----

	mcp.AddTool(s, &mcp.Tool{
		Name:        "identify",
		Description: "接続中の ZOOM ペダルの機種 ID・ファームウェア版・パッチ数を返す。",
		Annotations: readOnly,
	}, handle(func(noArgs) (ops.Identity, error) {
		return withZoom(false, func(z *device.Zoom) (ops.Identity, error) { return ops.Identify(z), nil })
	}))

	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_patches",
		Description: "全パッチの番号（ペダルの画面と同じ 1〜300）と名前の一覧。5 秒ほどかかる。",
		Annotations: readOnly,
	}, handle(func(noArgs) ([]ops.PatchName, error) { return withZoom(false, ops.ListPatches) }))

	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_patch",
		Description: "パッチの中身（スロットごとのエフェクト・ON/OFF・パラメータ値と最大値）を返す。number を省くと、いま鳴っている（編集中の）パッチ。",
		InputSchema: schema[numberArg](numberRange),
		Annotations: readOnly,
	}, handle(func(a numberArg) (ops.PatchView, error) {
		return withZoom(false, func(z *device.Zoom) (ops.PatchView, error) {
			n := 0
			if a.Number != nil {
				n = *a.Number
			}
			return ops.GetPatch(z, n)
		})
	}))

	mcp.AddTool(s, &mcp.Tool{
		Name:        "backup_all",
		Description: "全 300 パッチをこのマシンの backups/ に .syx で退避する。ペダルには書き込まない。",
		Annotations: readOnly,
	}, handle(func(noArgs) (ops.BackupResult, error) {
		return withZoom(false, func(z *device.Zoom) (ops.BackupResult, error) { return ops.BackupAll(z, nil) })
	}))

	// ---- カタログ ----

	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_effects",
		Description: "ペダルに入っているエフェクトの一覧（名前・グループ・パラメータと範囲）。音作りの候補を選ぶのに使う。詳しい説明は describe_effect。",
		Annotations: readOnly,
	}, handle(func(a groupArg) ([]ops.EffectSummary, error) { return ops.ListEffects(a.Group) }))

	mcp.AddTool(s, &mcp.Tool{
		Name:        "describe_effect",
		Description: "エフェクト 1 つの説明（モデルになった機材など）と、各パラメータの意味・範囲・既定値。",
		Annotations: readOnly,
	}, handle(func(a effectArg) (ops.EffectDetail, error) { return ops.DescribeEffect(a.Effect) }))

	// ---- 編集（いま鳴っているパッチ。保存しない） ----

	mcp.AddTool(s, &mcp.Tool{
		Name:        "set_effect",
		Description: "いま鳴っているパッチのスロットのエフェクトを差し替える（保存しない）。パラメータは既定値か、同じ電源投入中に前回そのエフェクトで使った値になるので、結果を見て set_param で整える。",
		InputSchema: schema[setEffectArgs](slotRange),
	}, handle(func(a setEffectArgs) (ops.SlotView, error) {
		// カタログにないエフェクトはペダルを開く前に断る
		cat, err := ops.Catalog()
		if err != nil {
			return ops.SlotView{}, err
		}
		if _, err := cat.Resolve(a.Effect); err != nil {
			return ops.SlotView{}, err
		}
		return withZoom(true, func(z *device.Zoom) (ops.SlotView, error) { return ops.SetEffect(z, a.Slot, a.Effect) })
	}))

	mcp.AddTool(s, &mcp.Tool{
		Name:        "set_param",
		Description: "いま鳴っているパッチのスロットのパラメータを名前で変える（保存しない）。値はカタログの範囲で検証する。",
		InputSchema: schema[setParamArgs](func(p map[string]*jsonschema.Schema) {
			slotRange(p)
			intRange(p["value"], 0, 0)
		}),
	}, handle(func(a setParamArgs) (ops.SlotView, error) {
		return withZoom(true, func(z *device.Zoom) (ops.SlotView, error) { return ops.SetParam(z, a.Slot, a.Param, a.Value) })
	}))

	mcp.AddTool(s, &mcp.Tool{
		Name:        "toggle_effect",
		Description: "いま鳴っているパッチのスロットの ON / OFF を切り替える（保存しない）。enabled を省くと反転。",
		InputSchema: schema[toggleArgs](func(p map[string]*jsonschema.Schema) {
			slotRange(p)
			p["enabled"].Types, p["enabled"].Type = nil, "boolean"
		}),
	}, handle(func(a toggleArgs) (ops.SlotView, error) {
		return withZoom(true, func(z *device.Zoom) (ops.SlotView, error) { return ops.Toggle(z, a.Slot, a.Enabled) })
	}))

	// ---- 保存（フラッシュに書く） ----

	mcp.AddTool(s, &mcp.Tool{
		Name:        "save_patch",
		Description: "いま鳴っている（編集中の）パッチを、パッチメモリ number に保存する。ペダルのフラッシュに書き込むので、必ずユーザーに番号を確かめてから呼ぶ。上書きされる中身は backups/overwritten/ に退避され、restore_patch で戻せる。",
		InputSchema: schema[requiredNumberArg](numberRange),
		Annotations: destructive,
	}, handle(func(a requiredNumberArg) (ops.SaveResult, error) {
		return withZoom(true, func(z *device.Zoom) (ops.SaveResult, error) { return ops.Save(z, a.Number) })
	}))

	mcp.AddTool(s, &mcp.Tool{
		Name:        "restore_patch",
		Description: "save_patch で上書きしたパッチ number を、上書き直前の中身に戻す（いちばん新しい退避を使う。フラッシュに書き込む）。",
		InputSchema: schema[requiredNumberArg](numberRange),
		Annotations: destructive,
	}, handle(func(a requiredNumberArg) (ops.RestoreResult, error) {
		// 退避が無ければペダルを開く前に断る
		if _, _, err := ops.LatestBackup(model, a.Number); err != nil {
			return ops.RestoreResult{}, err
		}
		return withZoom(true, func(z *device.Zoom) (ops.RestoreResult, error) { return ops.Restore(z, a.Number) })
	}))

	// ---- リソース ----

	s.AddResource(&mcp.Resource{
		Name:        "catalog",
		URI:         "zm://catalog",
		Description: "エフェクトカタログ全体（説明・パラメータの意味・範囲・既定値）",
		MIMEType:    "application/json",
	}, func(_ context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		cat, err := ops.Catalog()
		if err != nil {
			return nil, err
		}
		b, err := compactJSON(cat)
		if err != nil {
			return nil, err
		}
		return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: req.Params.URI, MIMEType: "application/json", Text: b}}}, nil
	})

	s.AddResource(&mcp.Resource{
		Name:        "current_patch",
		URI:         "zm://patch/current",
		Description: "いま鳴っている（編集中の）パッチ",
		MIMEType:    "application/json",
	}, func(_ context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		d, err := withZoom(false, func(z *device.Zoom) (catalog.NamedPatch, error) {
			cat, err := ops.CatalogFor(z.Model)
			if err != nil {
				return catalog.NamedPatch{}, err
			}
			p, err := z.CurrentPatch()
			if err != nil {
				return catalog.NamedPatch{}, err
			}
			return cat.Describe(p), nil
		})
		if err != nil {
			return nil, err
		}
		b, err := ops.JSON(d)
		if err != nil {
			return nil, err
		}
		return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: req.Params.URI, MIMEType: "application/json", Text: string(b)}}}, nil
	})

	return s
}

func compactJSON(v any) (string, error) {
	b, err := ops.CompactJSON(v)
	if err != nil {
		return "", fmt.Errorf("JSON: %w", err)
	}
	return string(b), nil
}

// Run は stdio で MCP サーバーを動かす。
func Run(ctx context.Context, version string) error {
	return New(version).Run(ctx, &mcp.StdioTransport{})
}
