# zm

[English](README.md) | 日本語

ZOOM G2 FOUR・MS-80IR+ と旧 MS-50G / MS-70CDR を、CLI・TUI・MCP サーバー（Claude Code から音作り）で操作する。1 本の Go のバイナリで動く。

Linux の ALSA rawmidi（`/dev/snd/midiCxDy`）を直接読み書きするので、ほかの OS では動かない。

ZOOM 公式のツールではない。ペダルに書き込むコマンド（`save` / `restore`）は上書き前の中身を退避するが、使う前に `zm backup` を取っておくこと。

## 対応ペダル

| ペダル | パッチ数 | エフェクトカタログ | 確かめたファームウェア |
| --- | --- | --- | --- |
| ZOOM G2 FOUR | 300 | `zm catalog` でペダルから作る | 1.00 |
| ZOOM MS-80IR+ | 80 | `zm catalog` でペダルから作る | 1.00 |
| ZOOM MS-50G | 50 | 埋め込み（172 エフェクト） | 3.10 |
| ZOOM MS-70CDR | 50 | 埋め込み（137 エフェクト） | 2.10 |

4 機種とも同じように使える。CLI と MCP サーバーからは読み出し・編集・保存・復元・バックアップ、TUI からは読み出し・編集・保存ができる。`rename` は CLI だけで、MS-50G / MS-70CDR でしか使えない。

MS-80IR+ の IR 系のエフェクトは ZD2 を持たないので `(unknown)` と出る。

ほかの機種は認識しない。MS-60B はパッチの形式が違い、ほかの MS+ シリーズと FOUR 系は試していない。

## 入れ方

```sh
go install github.com/polidog/zm/cmd/zm@latest
```

Go 1.27 以降が要る。リポジトリから作るなら `go build -o zm ./cmd/zm`。

## 使い方

```sh

zm info                           機種・ファームウェア・パッチ数
zm list                           全パッチの番号と名前
zm show [n]                       パッチの中身（省略で、いま鳴っているパッチ）
zm effects [group]                エフェクト一覧
zm effect <name>                  エフェクトの説明とパラメータ
zm set-effect <slot> <effect>     エフェクト差し替え（保存しない）
zm set <slot> <param> <value>     パラメータ変更（保存しない）
zm on|off|toggle <slot>           ON / OFF（保存しない）
zm rename <name>                  パッチ名の変更（保存しない。いまは旧 MS だけ）
zm save <n> [--yes]               いま鳴っているパッチを n に保存（確認つき、上書き前を退避）
zm restore <n> [--yes]            save で上書きした n を戻す
zm backup                         全パッチを backups/ に退避
zm catalog [--refresh]            ペダルの ZD2 からカタログを作る
zm tui                            TUI
zm mcp                            MCP サーバー (stdio)
zm probe                          疎通確認（開発用）
```

- 番号はペダルの画面に合わせる（パッチは 1〜300 / 旧 MS は 1〜50、スロットは 1〜6）
- `zm --pedal ms50g …`（ms70cdr / g2four / ms80irplus）/ `zm --pedal hw:5,0 …`（または `$ZM_PEDAL`）で使うペダルを選ぶ。省くと最初に見つかった ZOOM
- 読み出し系は `--json` で機械向けの出力を出す。`zm show --file x.syx` で保存済みの SysEx も読める
- 編集（set-effect / set / on / off / toggle）は「いま鳴っているパッチ」への変更で、保存しない。パッチを選び直せば戻る
- MIDI ポートは 1 プロセスしか開けない。TUI を開いている間は CLI と MCP からは使えない

### はじめに

```sh
zm backup       # 全パッチを退避（ペダルには書かない）
zm catalog      # ペダルの ZD2 を読み出してエフェクトカタログを作る（ペダルには書かない）
```

`backups/` と `catalog/` は `~/.config/zm/`（`$XDG_CONFIG_HOME` があれば `$XDG_CONFIG_HOME/zm/`）に置き、無ければ書くときに作る。
`--dir <path>`（`zm --dir x backup` でも `zm backup --dir x` でもよい）か `$ZM_DIR` で場所を変えられる。どこで実行しても同じ場所を使う。
どちらも ZOOM の著作物を含むので git には入れない。

旧 MS（MS-50G / MS-70CDR）のカタログは g200kg/zoom-ms-utility（MIT）のエフェクト一覧から作ってバイナリに埋め込んであるので、`zm catalog` は要らない。
旧 MS のバックアップは `backups/ms50g/`・`backups/ms70cdr/` に置く。旧 MS 固有の注意（AUTO SAVE、エディタモード）は [docs/legacy-ms.md](docs/legacy-ms.md)。

### Claude Code から使う

`zm` を PATH に入れたら、MCP サーバーとして登録する。

```sh
claude mcp add zm -- zm mcp
```

このリポジトリの `.mcp.json` には `go run ./cmd/zm mcp` を登録してあるので、ここで Claude Code を開いてもそのまま使える。

## 構成

```
cmd/zm/            サブコマンドの振り分け
internal/
  midi/            ALSA rawmidi。SysEx を 1 つずつ受け取る
  sysex/           メッセージの組み立て、Identity Reply
  codec/           7bit パック、CRC、パッチ SysEx の枠（45 / 64 12）
  patch/           PTCF ⇔ 構造体
  legacypatch/     旧 MS-50G / MS-70CDR のパッチ（146 バイトの 28 SysEx）
  zd2/             ZD2 の解析（PRME / PRMJ と ELF のパラメータ表）
  catalog/         カタログの読み書き、パッチの名前つき表示（旧 MS のカタログは埋め込み）
  pedalfs/         ペダル内ファイルの一覧と読み出し（読み出しのみ）
  device/          機種ごとの値と選び方。G2 FOUR / MS-80IR+（four.go）と旧 MS-50G / MS-70CDR（legacy.go）の読み出し・編集・保存
  backups/         backups/ への退避と復元
  ops/             CLI・TUI・MCP が共有する操作（名前で解決、範囲の検証、番号の変換、保存時の退避）
  cli/ tui/ mcpserver/
```

実機で分かったプロトコルの事実は [docs/go-rewrite.md](docs/go-rewrite.md) の「引き継ぐ実機の事実」にまとめてある。

## テスト

```sh
go test ./...
```

実機のパッチ（`backups/all-*`、`backups/ms50g/all-*`、`backups/ms70cdr/all-*`）とカタログ（`catalog/`）を使うテストは、CLI と同じ置き場（`~/.config/zm/` か `$ZM_DIR`）を見て、無ければスキップする。

## ライセンス

MIT。由来元（zoom-ms-utility、zoom-zt2、zoom-explorer）の表記は [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)。
