# zoom

ZOOM G2 FOUR と旧 MS-50G / MS-70CDR を、CLI・TUI・MCP サーバー（Claude Code から音作り）で操作する。1 本の Go のバイナリで動く。

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
zoom rename <name>                パッチ名の変更（保存しない。いまは旧 MS だけ）
zoom save <n> [--yes]             いま鳴っているパッチを n に保存（確認つき、上書き前を退避）
zoom restore <n> [--yes]          save で上書きした n を戻す
zoom backup                       全パッチを backups/ に退避
zoom catalog [--refresh]          ペダルの ZD2 からカタログを作る
zoom tui                          TUI
zoom mcp                          MCP サーバー (stdio)
zoom probe                        疎通確認（開発用）
```

- 番号はペダルの画面に合わせる（パッチは 1〜300 / 旧 MS は 1〜50、スロットは 1〜6）
- `zoom --pedal ms50g …`（ms70cdr / g2four）/ `zoom --pedal hw:5,0 …`（または `$ZOOM_PEDAL`）で使うペダルを選ぶ。省くと最初に見つかった ZOOM
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

旧 MS（MS-50G / MS-70CDR）のカタログは g200kg/zoom-ms-utility（MIT）のエフェクト一覧から作ってバイナリに埋め込んであるので、`zoom catalog` は要らない。
旧 MS のバックアップは `backups/ms50g/`・`backups/ms70cdr/` に置く。旧 MS 固有の注意（AUTO SAVE、エディタモード）は [docs/legacy-ms.md](docs/legacy-ms.md)。

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
  legacypatch/     旧 MS-50G / MS-70CDR のパッチ（146 バイトの 28 SysEx）
  zd2/             ZD2 の解析（PRME / PRMJ と ELF のパラメータ表）
  catalog/         カタログの読み書き、パッチの名前つき表示（旧 MS のカタログは埋め込み）
  pedalfs/         ペダル内ファイルの一覧と読み出し（読み出しのみ）
  device/          機種ごとの値と選び方。G2 FOUR（four.go）と旧 MS-50G / MS-70CDR（legacy.go）の読み出し・編集・保存
  backups/         backups/ への退避と復元
  ops/             CLI・TUI・MCP が共有する操作（名前で解決、範囲の検証、番号の変換、保存時の退避）
  cli/ tui/ mcpserver/
```

実機で分かったプロトコルの事実は [docs/go-rewrite.md](docs/go-rewrite.md) の「引き継ぐ実機の事実」にまとめてある。

## テスト

```sh
go test ./...
```

実機のパッチ（`backups/all-*`、`backups/ms50g/all-*`、`backups/ms70cdr/all-*`）とカタログ（`catalog/`）を使うテストは、無ければスキップする。
