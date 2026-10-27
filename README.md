# zoom

ZOOM G2 FOUR を、CLI・TUI・MCP サーバー（Claude Code から音作り）で操作する。1 本の Go のバイナリで動く。

Linux の ALSA rawmidi（`/dev/snd/midiCxDy`）を直接読み書きするので、ほかの OS では動かない。

## 使い方

```sh
go build -o zoom ./cmd/zoom      # または go install ./cmd/zoom

zoom info                         機種・ファームウェア・パッチ数
zoom list                         全パッチの番号と名前
zoom show [n]                     パッチの中身（省略で、いま鳴っているパッチ）
zoom effects [group]              エフェクト一覧
zoom effect <name>                エフェクトの説明とパラメータ
zoom set-effect <slot> <effect>   エフェクト差し替え（保存しない）
zoom set <slot> <param> <value>   パラメータ変更（保存しない）
zoom on|off|toggle <slot>         ON / OFF（保存しない）
zoom save <n> [--yes]             いま鳴っているパッチを n に保存（確認つき、上書き前を退避）
zoom restore <n> [--yes]          save で上書きした n を戻す
zoom backup                       全パッチを backups/ に退避
zoom catalog [--refresh]          ペダルの ZD2 からカタログを作る
zoom tui                          TUI
zoom mcp                          MCP サーバー (stdio)
zoom probe                        疎通確認（開発用）
```

- 番号はペダルの画面に合わせる（パッチは 1〜300、スロットは 1〜6）
- 読み出し系は `--json` で機械向けの出力を出す。`zoom show --file x.syx` で保存済みの SysEx も読める
- 編集（set-effect / set / on / off / toggle）は「いま鳴っているパッチ」への変更で、保存しない。パッチを選び直せば戻る
- MIDI ポートは 1 プロセスしか開けない。TUI を開いている間は CLI と MCP からは使えない

### はじめに

```sh
zoom backup     # 全パッチを退避（ペダルには書かない）
zoom catalog    # ペダルの ZD2 を読み出してエフェクトカタログを作る（ペダルには書かない）
```

`backups/` と `catalog/` は `$ZOOM_DIR`（無ければカレントディレクトリ）に置く。
どちらも ZOOM の著作物を含むので git には入れない。

### Claude Code から使う

`.mcp.json` に `go run ./cmd/zoom mcp` を登録してある。このディレクトリで Claude Code を開けば `zoom` の MCP サーバーが使える。
`zoom` を PATH に入れたなら、`"command": "zoom", "args": ["mcp"]` にしてもよい。

## 構成

```
cmd/zoom/          サブコマンドの振り分け
internal/
  midi/            ALSA rawmidi。SysEx を 1 つずつ受け取る
  sysex/           メッセージの組み立て、Identity Reply
  codec/           7bit パック、CRC、パッチ SysEx の枠（45 / 64 12）
  patch/           PTCF ⇔ 構造体
  zd2/             ZD2 の解析（PRME / PRMJ と ELF のパラメータ表）
  catalog/         カタログの読み書き、パッチの名前つき表示
  pedalfs/         ペダル内ファイルの一覧と読み出し（読み出しのみ）
  device/          機種ごとの値、読み出し・64 20 編集・45 保存
  backups/         backups/ への退避と復元
  ops/             CLI・TUI・MCP が共有する操作（名前で解決、範囲の検証、番号の変換、保存時の退避）
  cli/ tui/ mcpserver/
```

実機で分かったプロトコルの事実は [docs/go-rewrite.md](docs/go-rewrite.md) の「引き継ぐ実機の事実」にまとめてある。

## テスト

```sh
go test ./...
```

実機のパッチ（`backups/all-*`）とカタログ（`catalog/`）を使うテストは、無ければスキップする。
