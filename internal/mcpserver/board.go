package mcpserver

import (
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/polidog/zm/internal/board"
	"github.com/polidog/zm/internal/ops"
)

// instructions は MCP クライアント（Claude）に渡す、ボード全体で音を作るときの手順と考え方。
const instructions = `zm は ZOOM のマルチエフェクター（MS-50G / MS-70CDR / MS-80IR+ / G2 FOUR）を操作する。

## ペダルが何台もあるとき（ボード）
board.json があれば、ギターからアンプまでの並び（MIDI の無いアナログのペダルも含む）が get_board で分かる。
音作りを頼まれたら、まず get_board で並びと各ペダルのいまの音を見て、1 台ずつではなくボード全体で考える。
ペダルを触るツールには pedal（機種名）を必ず渡す。

考えること:
- 信号の順番。前段で作った音は後段の入力になる。歪みの前にコンプ・ブースト・EQ を置けば歪みの質が変わり、歪みの後に置けば仕上げになる
- 役割を重ねない。同じ種類のエフェクト（歪みを 2 台、リバーブを 2 台）を重ねるなら理由を言う
- 歪みの量は合計で見る。前段のブーストとアナログの歪み、後段のアンプシミュレーターのゲインを足して、狙いより多くならないようにする
- 低域と高域は段ごとに積み上がる。こもる・耳に痛いときは、どの段で削るかを決める
- 音量はシーンを切り替えても揃うように、各ペダルのパッチ音量（とアナログの Level）で合わせる
- アナログのペダルのつまみは zm から回せない。どう回すかをユーザーに言い、回したら set_analog で位置を書いておく
- エフェクトのパラメータの意味は describe_effect で確かめてから値を決める

## 保存
- 編集（set_effect / set_param / toggle_effect）は各ペダルのいま鳴っているパッチへの変更で、保存しない
- 気に入ったら save_scene でボード全体をファイルに残す（ペダルには書かない）。load_scene で各ペダルに流し込んで戻せる
- save_patch はペダルのフラッシュに書く。必ずユーザーに番号を確かめてから呼ぶ
- MS-50G / MS-70CDR は本体の AUTO SAVE が ON だと、編集中・流し込んだ後にペダルでパッチを切り替えた時点でそのパッチに保存される。編集の後はそのことを伝える`

type sceneNameArg struct {
	Name string `json:"name" jsonschema:"シーン名（ファイル名になる。/ は使えない）"`
}

type saveSceneArgs struct {
	Name      string `json:"name" jsonschema:"シーン名（ファイル名になる。/ は使えない）"`
	Note      string `json:"note,omitempty" jsonschema:"狙った音（曲名、どんな場面で使うか、など）"`
	Overwrite bool   `json:"overwrite,omitempty" jsonschema:"同じ名前のシーンがあれば上書きする"`
}

type setAnalogArgs struct {
	Name  string            `json:"name" jsonschema:"アナログのペダルの名前（board.json の name）"`
	Knobs map[string]string `json:"knobs" jsonschema:"つまみと位置（例: {\"Dist\": \"10時\", \"Mode\": \"Custom\"}）。空文字のつまみは消す"`
}

func addBoardTools(s *mcp.Server, readOnly *mcp.ToolAnnotations) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "get_board",
		Description: "エフェクターボードの並び（ギター側から順）と、各ペダルのいま鳴っているパッチ、アナログのペダルのつまみの位置を返す。" +
			"ボード全体で音を作るときに最初に呼ぶ。繋がっていないペダルは error に理由が入る。",
		Annotations: readOnly,
	}, handle(func(noArgs) (ops.BoardView, error) { return ops.GetBoard() }))

	mcp.AddTool(s, &mcp.Tool{
		Name:        "set_analog",
		Description: "MIDI の無いペダルのつまみの位置を board.json に書く（ペダルには何も送らない）。ユーザーが実際に回した後に呼ぶ。",
		InputSchema: schema[setAnalogArgs](func(p map[string]*jsonschema.Schema) {
			p["knobs"].AdditionalProperties = &jsonschema.Schema{Type: "string"}
		}),
	}, handle(func(a setAnalogArgs) (ops.StageView, error) { return ops.SetKnobs(a.Name, a.Knobs) }))

	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_scenes",
		Description: "保存したシーン（ボード全体の音色）の一覧。",
		Annotations: readOnly,
	}, handle(func(noArgs) ([]board.SceneEntry, error) { return ops.ListScenes() }))

	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_scene",
		Description: "シーンの中身（各ペダルのパッチとアナログのつまみ）を返す。ペダルには触らない。",
		Annotations: readOnly,
	}, handle(func(a sceneNameArg) (*board.Scene, error) {
		sc, err := ops.GetScene(a.Name)
		if err != nil {
			return nil, err
		}
		for i := range sc.Stages {
			sc.Stages[i].Sysex = "" // 長いだけで読めない
		}
		return sc, nil
	}))

	mcp.AddTool(s, &mcp.Tool{
		Name: "save_scene",
		Description: "各ペダルのいま鳴っているパッチと、アナログのペダルのつまみ（board.json の位置）を、シーンとしてこのマシンのファイルに残す。" +
			"ペダルには書き込まない。1 台でも読めなければ残さない。",
	}, handle(func(a saveSceneArgs) (ops.SceneResult, error) {
		r, err := ops.SaveScene(a.Name, a.Note, a.Overwrite)
		for i := range r.Stages {
			r.Stages[i].Sysex = ""
		}
		return r, err
	}))

	mcp.AddTool(s, &mcp.Tool{
		Name: "load_scene",
		Description: "シーンを各ペダルのいま鳴っているパッチに流し込む（保存しない）。アナログのペダルは返ってくる knobs をユーザーに伝えて回してもらう。" +
			"MS-50G / MS-70CDR は AUTO SAVE が ON だと、この後ペダルでパッチを切り替えた時点でそのパッチに保存される。",
	}, handle(func(a sceneNameArg) (ops.LoadResult, error) { return ops.LoadScene(a.Name) }))
}
