# Go への書き直し計画

TypeScript (bun) で作った P0〜P4 のコアと MCP サーバーを Go に移し、CLI と TUI を足して、
`zoom` 1 本のバイナリにまとめる。全体の計画は [plan.html](plan.html)。

## なぜ書き直すか

- **TUI**: Bubble Tea が使える。ペダル側でつまみを回したときの通知（`64 12 00` など）を
  goroutine で受け続け、画面にすぐ映す作りが素直に書ける
- **配布**: CLI・TUI・MCP サーバーが 1 つの静的バイナリで動く。bun が要らない
- **今が安い**: コアは約 1700 行（テスト込み）で、外部ライブラリにほぼ頼っていない。
  CLI と TUI を TypeScript で書いてから移すと、移す量が倍になる

## 完成形

```
zoom info                         機種・ファームウェア・パッチ数
zoom list                         全パッチの番号と名前
zoom show [n]                     パッチの中身（省略で、いま鳴っているパッチ）
zoom effects [group]              エフェクト一覧
zoom effect <name>                エフェクトの説明とパラメータ
zoom set-effect <slot> <effect>   エフェクト差し替え（保存しない）
zoom set <slot> <param> <value>   パラメータ変更（保存しない）
zoom on|off|toggle <slot>         ON / OFF（保存しない）
zoom save <n> [--yes]             いま鳴っているパッチを n に保存（確認つき、上書き前を退避）
zoom restore <n>                  save で上書きした n を戻す
zoom backup                       全パッチを backups/ に退避
zoom catalog [--refresh]          ペダルの ZD2 からカタログを作る
zoom tui                          TUI
zoom mcp                          MCP サーバー (stdio)
zoom probe                        P0 の疎通確認（開発用）
```

- 番号はペダルの画面に合わせる（パッチは 1〜300、スロットは 1〜6）。今の MCP と同じ
- 読み出し系は `--json` で機械向けの出力を出す。人向けの出力は表にする

## 構成

```
cmd/zoom/main.go        サブコマンドの振り分け（標準の flag で足りる）
internal/
  midi/      L1  ALSA rawmidi。/dev/snd/midiCxDy を開き、SysEx を 1 つずつ受け取る
  sysex/         メッセージの組み立て、Identity Reply
  codec/     L2  7bit パック、CRC、パッチ SysEx の枠（45 / 64 12）
  patch/     L2  PTCF ⇔ 構造体
  zd2/       L3  ZD2 の解析（PRME / PRMJ と ELF のパラメータ表）
  catalog/   L3  カタログの読み書き、describePatch
  pedalfs/       ペダル内ファイルの一覧と読み出し（60 xx、読み出し系のみ）
  device/        Zoom: 読み出し・64 20 編集・45 保存
  backups/       backups/ への退避と復元
  ops/           CLI・TUI・MCP が共有する操作（名前で解決、範囲の検証、番号の変換、保存時の退避）
  cli/           サブコマンドの実装
  tui/       L4  Bubble Tea
  mcpserver/ L4  MCP サーバー
```

`ops/` が肝。今は `src/server.ts` の中に埋まっている次の処理をここへ出し、3 つの入口から同じ関数を呼ぶ。

- エフェクトを名前・ID で引く
- パラメータを名前で引いて、範囲を検証する
- 1 始まりと 0 始まりの番号を変換する
- 保存の前に、上書きされる中身を退避する

### TypeScript からの対応

| TypeScript | Go | メモ |
|---|---|---|
| `src/transport.ts` | `internal/midi` | 下の「リスク」を参照 |
| `src/sysex.ts` | `internal/sysex` | |
| `src/codec.ts` | `internal/codec` | `Bun.hash.crc32` → `hash/crc32.ChecksumIEEE` |
| `src/patch.ts` | `internal/patch` | 192bit / 256bit のビット操作は `math/big` か、バイト単位のビット読み書きで |
| `src/zd2.ts` | `internal/zd2` | Shift-JIS は `golang.org/x/text/encoding/japanese` |
| `src/catalog.ts` | `internal/catalog` | |
| `src/files.ts` | `internal/pedalfs` | |
| `src/device.ts` | `internal/device` | |
| `src/backups.ts` | `internal/backups` | |
| `src/server.ts` | `internal/ops` + `internal/mcpserver` | |
| `src/probe.ts` `backup.ts` `dump.ts` `build-catalog.ts` | `internal/cli` | |

### 依存

- `golang.org/x/text`: Shift-JIS（TXJ1）
- `golang.org/x/sys/unix`: rawmidi の poll（必要なら）
- `github.com/modelcontextprotocol/go-sdk`: MCP
- `github.com/charmbracelet/bubbletea` と `lipgloss`: TUI

## 引き継ぐ実機の事実

コードとテストには残っているが、書き直しで落としやすいものを並べておく。

**プロトコル**
- デバイス ID は `6E`（Identity Reply の family 下位）。パッチは 300 件 = バンク 30 × 10
- 46 で読むと 45 で返る。`45 00 00 bL bM pL pM | lenL lenM | 7bit データ | CRC 5 バイト`
- 7bit パックは先頭バイトの bit6 が 1 つ目のデータに対応。CRC は ~CRC32 を下位から 7bit × 5
- `33`（現在のプログラム番号）は G2 FOUR では応答がない
- 編集にはエディタモード（`50`）が要る。開くたびに AUTO SAVE OFF（`64 20 00 64 0F 0…`）も送る
- `64 20 00 <slot> <paramNo> <値 7bit×5>`。paramNo 0 = ON/OFF、1 = エフェクト ID、2〜 = param1〜
  - ON/OFF とパラメータの受理は `64 20 01`。**値が変わらないときは何も返らない**ので、読み直して確かめる
  - エフェクト ID の変更は `64 12 00`（変更後のパッチ全体）が返る
  - パラメータはエフェクトごとに、電源を入れている間ずっと覚えている。差し替え後の値は返ってきたパッチを見る
- `64 13` への応答は `64 12 01`。`64 12 00` はペダルが自分から送る通知なので、応答と取り違えない
- `28`（現在パッチの丸ごと送信）は**使わない**。エフェクトが読み込まれず、パラメータが 0 のまま編集も受け付けなくなる
- 保存は `45` をそのまま送り、`00 00` で受理。読み直して中身の一致を確かめる
- ファイル操作は PC モード（`52` / `53`）で、zoomzt2.py と同じ順に送る（open は 2 回送る）

**PTCF / ZD2**
- PTCF は 832 バイト固定のバッファの先頭に入り、後ろに前のデータの残骸が付く。ラウンドトリップでは残骸もそのまま戻し、PTCF の長さが変わったら 0 で埋める
- TXJ1 は Shift-JIS、TXE1 は 4 の倍数に切り上げ（ちょうど割り切れれば NUL なし）、NAME は 14 文字を空白で埋めて NUL 2 つ
- ヘッダの ids 欄は effects の ID から作る
- PRM2: patchVolume は bit 77（7bit）、editSlot は bit 85（3bit）、tempo は bit 244（8bit）
- ZD2 の PRMJ は UTF-8、TXJ1 は Shift-JIS。パラメータ表は ELF の中で `OnOff` から 56 バイト刻み。PRME より後ろの項目は隠しパラメータ（アンプ系の `Dummy` など）

**運用**
- ポートを開いたまま例外で抜けると、以降がすべて EBUSY になる。必ず閉じる
- 編集用の接続は、エディタモード OFF の応答を待ってから閉じる（残った応答が次の接続に混ざる）
- MIDI ポートは 1 プロセスしか開けない。TUI と MCP は同時に使えない

## データの互換

今あるファイルは形式を変えずに Go からも読み書きする。書き直しの途中で作り直さなくてよい。

- `backups/all-<時刻>/NNN.syx`（NNN は 0 始まり）と `backups/overwritten/<時刻>-NNN.syx`
- `catalog/zd2/*.ZD2`（ペダルから取ったキャッシュ）と `catalog/g2four.json`
- `.mcp.json` だけ、`bun run src/server.ts` から `zoom mcp` に書き換える

## 正しさの確かめ方

TypeScript 版を「正解」として残したまま、同じ入力に同じ出力を返すことを機械的に確かめる。

1. **ラウンドトリップ**: `test/roundtrip.test.ts` を Go に移す。実機の 300 パッチで SysEx → 構造体 → SysEx がバイト一致
2. **カタログ**: `test/catalog.test.ts` を移す。加えて、Go が `catalog/zd2/` から作ったカタログと、TS 版の `g2four.json` が（`generatedAt` を除いて）一致
3. **TS との突き合わせ**: 300 パッチ全部で、`bun run dump <file> --named` と `zoom show --file <file> --json` の JSON が一致
4. **MCP**: 同じツール呼び出しを TS 版と Go 版に投げ、返る JSON を比べる（ペダルに書かない読み出し系で）
5. **実機**: P3 で手でやった確認を、画面の 263（Empty）で繰り返す
   - エフェクト差し替えで既定値が入る / パラメータ変更 / 同じ値の変更 / ON・OFF
   - 保存 → 選び直して音が出る → restore で Empty に戻る → 300 件が最初のバックアップと一致

backups/ と catalog/ は git に入れていないので、1〜3 は TS 版と同じく、無ければスキップする。

## 進め方

| 段 | やること | 終わりの条件 |
|---|---|---|
| G0 | `go.mod`、ディレクトリ、`zoom probe` | 実機で Identity と現在パッチのダンプが取れる |
| G1 | midi / sysex / codec / patch / device の読み出し | ラウンドトリップ 300 件一致、TS との突き合わせ一致 |
| G2 | zd2 / catalog / pedalfs、`zoom catalog` | カタログが TS 版と一致、カタログのテストが通る |
| G3 | device の編集と保存、ops、CLI 全部 | 実機の確認（上の 5）が CLI で通る |
| G4 | mcpserver、`.mcp.json` の書き換え | MCP の突き合わせが一致、Claude Code から音作りできる |
| G5 | TUI | 下の「TUI」を満たす |
| G6 | TypeScript 版を消す | `src/` `test/` `package.json` などを削除、README を Go 前提に |

G1〜G4 の間は TS 版を残し、突き合わせの正解として使う。各段の終わりでコミットする。

## TUI

```
┌ 263 USER-014 ───────────────── vol 100  tempo 120 ┐
│ 1 ● TS Drive    Gain ▮▮▮▮▮▮░░░░ 60   Tone 57 ...  │
│ 2 ○ (empty)                                       │
│ ...                                               │
├───────────────────────────────────────────────────┤
│ ↑↓ スロット  ←→ パラメータ  +/- 値  space ON/OFF  │
│ e エフェクト選択  s 保存  r 読み直し  q 終了       │
└───────────────────────────────────────────────────┘
```

- 起動中は 1 本の接続を開いたままにし、受信を goroutine で回す。ペダル側の操作（`64 12 00` などの通知）を画面に映す
- 値の変更は 64 20 ですぐ送る。押しっぱなしで連打しても詰まらないよう、送信は間引く
- エフェクト選択はグループ → 名前の絞り込み。説明（`describe_effect` と同じ内容）を横に出す
- 保存は番号を確かめるダイアログを出し、上書き前を退避する
- 終了時はエディタモード OFF の応答を待ってから閉じる

## リスクと先に確かめること

- **rawmidi の読み方（G0 で確かめる）**: TS 版は O_NONBLOCK で開いて 5ms ごとに読んでいる。Go では
  `os.OpenFile` を O_NONBLOCK で開けば、ランタイムの poller に載って `SetReadDeadline` が効くはず。
  効かなければ `unix.Poll` で待つ。閉じたときに、読んでいる goroutine が確実に抜けることも確かめる
- **ビット操作**: EDTB は 192bit、PRM2 は 256bit。`math/big` で TS と同じ書き方にするのが一番写しやすい。
  速さは問題にならない（1 パッチ 8 エフェクト）
- **TUI と MCP の同時利用**: ポートは 1 プロセスしか開けない。今回は「同時には使えない」と明示するだけにする。
  要るなら後で、ポートを持つ常駐プロセスを立て、TUI と MCP がそこへつなぐ形にする

## 決めていないこと

- 外のプロジェクトとして公開するか（する場合、カタログと backups を含めない今の方針で足りるか）
- Bubble Tea の版（v1 か、出ていれば v2 か）は G5 の時点で決める
