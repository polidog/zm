# zm

English | [日本語](README.ja.md)

Control a ZOOM G2 FOUR, MS-80IR+ and the older MS-50G / MS-70CDR from a CLI, a TUI, or an MCP server (shape your tone from Claude Code). It is a single Go binary.

zm reads and writes ALSA rawmidi (`/dev/snd/midiCxDy`) directly, so it runs on Linux only.

zm is not an official ZOOM tool. The commands that write to the pedal (`save` / `restore`) back up what they overwrite, but take a `zm backup` before you use them.

## Supported pedals

| Pedal | Patches | Effect catalog | Tested firmware |
| --- | --- | --- | --- |
| ZOOM G2 FOUR | 300 | built from the pedal with `zm catalog` | 1.00 |
| ZOOM MS-80IR+ | 80 | built from the pedal with `zm catalog` | 1.00, 1.10 |
| ZOOM MS-50G | 50 | embedded (172 effects) | 3.10 |
| ZOOM MS-70CDR | 50 | embedded (137 effects) | 2.10 |

All four work the same way: reading, editing, saving, restoring and backups from the CLI and MCP server, and reading, editing and saving from the TUI. `rename` is CLI only and works on the MS-50G / MS-70CDR only.

The MS-80IR+'s IR effects have no ZD2 file and show as `(unknown)`.

Other models are not recognized. The MS-60B uses a different patch format, and the other MS+ models and FOUR models have not been tested.

## Install

Download a binary for Linux (amd64 / arm64 / armv7) from [Releases](https://github.com/polidog/zm/releases):

```sh
curl -sL https://github.com/polidog/zm/releases/latest/download/zm_linux_amd64.tar.gz | tar xz zm
sudo install zm /usr/local/bin/
```

Or with Go:

```sh
go install github.com/polidog/zm/cmd/zm@latest
```

Requires Go 1.27 or later. To build from a checkout: `go build -o zm ./cmd/zm`.

## Usage

```sh
zm info                           model, firmware, number of patches
zm list                           number and name of every patch
zm show [n]                       contents of a patch (omit n for the patch currently playing)
zm select <n>                     switch to patch n (discards unsaved edits)
zm effects [group]                list effects
zm effect <name>                  an effect's description and parameters
zm set-effect <slot> <effect>     replace an effect (not saved)
zm set <slot> <param> <value>     change a parameter (not saved)
zm on|off|toggle <slot>           switch an effect on / off (not saved)
zm rename <name>                  rename the patch (not saved; legacy MS only for now)
zm tempo <bpm>                    set the patch tempo (not saved; 40–250)
zm save <n> [--yes]               save the current patch to n (asks first, backs up what it overwrites)
zm restore <n> [--yes]            undo a save to n
zm backup                         back up every patch to backups/
zm catalog [--refresh]            build the effect catalog from the pedal's ZD2 files
zm board ...                      the board and its scenes (see Boards below)
zm tui                            TUI
zm mcp                            MCP server (stdio)
zm probe                          connectivity check (for development)
zm version                        version of zm
```

- Numbers match the pedal's display (patches 1–300, or 1–50 on legacy MS; slots 1–6)
- Pick a pedal with `zm --pedal ms50g …` (ms70cdr / g2four / ms80irplus), `zm --pedal hw:5,0 …`, or `$ZM_PEDAL`. Without one, zm uses the first ZOOM pedal it finds
- Read commands take `--json` for machine-readable output. `zm show --file x.syx` reads a saved SysEx file
- Edits (set-effect / set / on / off / toggle) change the patch currently playing and are not saved. Selecting another patch discards them
- Only one process can open the MIDI port. While the TUI is open, the CLI and MCP server cannot reach the pedal

### Getting started

```sh
zm backup       # back up every patch (does not write to the pedal)
zm catalog      # read the pedal's ZD2 files and build the effect catalog (does not write to the pedal)
```

`backups/` and `catalog/` live in `~/.config/zm/` (`$XDG_CONFIG_HOME/zm/` if set) and are created on first write.
Use `--dir <path>` (either `zm --dir x backup` or `zm backup --dir x`) or `$ZM_DIR` to put them elsewhere. zm uses the same place wherever you run it.
Both contain ZOOM's copyrighted material, so keep them out of git.

The legacy MS (MS-50G / MS-70CDR) catalogs are built from the effect list in g200kg/zoom-ms-utility (MIT) and embedded in the binary, so they need no `zm catalog`.
Legacy MS backups go to `backups/ms50g/` and `backups/ms70cdr/`. Legacy-specific caveats (AUTO SAVE, editor mode) are in [docs/legacy-ms.md](docs/legacy-ms.md) (Japanese).

### Boards (several pedals together)

With several pedals connected, list them in `~/.config/zm/board.json` from the guitar side.
Pedals zm can reach take the model key in `pedal`; pedals without MIDI take a `name` and their knob positions (`knobs`).
Claude reads each `note` when shaping the tone across the whole board.

```json
{
  "chain": [
    { "pedal": "ms50g", "note": "before the drive: comp, boost, EQ" },
    { "name": "BOSS DS-1W", "note": "main drive", "knobs": { "Tone": "12:00", "Level": "12:00", "Dist": "10:00", "Mode": "Standard" } },
    { "pedal": "ms70cdr", "note": "after the drive: chorus, delay, reverb" },
    { "pedal": "ms80irplus", "note": "amp and cab (IR)" }
  ]
}
```

```sh
zm board                          the chain and each pedal's current patch
zm board knob "BOSS DS-1W" Dist=12:00   record an analog pedal's knob positions (sends nothing)
zm board save lead --note "solo"  keep every pedal's current sound as a scene (not written to the pedals)
zm board load lead                load a scene into each pedal's current patch (not saved)
zm board scenes                   list scenes
```

- A scene lives in `~/.config/zm/scenes/<name>.json` and holds each pedal's current-patch SysEx and the analog knob positions. Pedal patch memory is not used
- Legacy MS pedals get the whole patch with `28`; G2 FOUR / MS-80IR+ get only the differences with `64 20` (the patch name is not changed)
- zm cannot turn analog knobs. Set them by hand to what `load` prints
- Single-pedal commands take `--pedal` (`zm --pedal ms70cdr show`). MCP tools take a `pedal` argument, and `get_board` / `save_scene` / `load_scene` / `set_analog` work on the whole board

### Use from Claude Code

With `zm` on your PATH, register it as an MCP server:

```sh
claude mcp add zm -- zm mcp
```

This repository's `.mcp.json` registers `go run ./cmd/zm mcp`, so opening Claude Code here works as is.

## Layout

```
cmd/zm/            subcommand dispatch
internal/
  midi/            ALSA rawmidi; receives one SysEx at a time
  sysex/           message builders, Identity Reply
  codec/           7-bit packing, CRC, patch SysEx framing (45 / 64 12)
  patch/           PTCF <-> structs
  legacypatch/     legacy MS-50G / MS-70CDR patches (the 146-byte 28 SysEx)
  zd2/             ZD2 parsing (PRME / PRMJ and the ELF parameter tables)
  board/           board.json (the chain) and scene files
  catalog/         catalog I/O, named patch display (legacy MS catalogs are embedded)
  pedalfs/         listing and reading files on the pedal (read-only)
  device/          per-model values and selection; read / edit / save for G2 FOUR / MS-80IR+ (four.go) and legacy MS-50G / MS-70CDR (legacy.go)
  backups/         backing up to and restoring from backups/
  ops/             operations shared by CLI, TUI and MCP (name lookup, range checks, number conversion, backup before save)
  cli/ tui/ mcpserver/
```

Protocol facts learned from real hardware are collected in [docs/go-rewrite.md](docs/go-rewrite.md) (Japanese).

## Tests

```sh
go test ./...
```

Tests that use real patches (`backups/all-*`, `backups/ms50g/all-*`, `backups/ms70cdr/all-*`) and the catalog (`catalog/`) look in the same place as the CLI (`~/.config/zm/` or `$ZM_DIR`) and are skipped if those are missing.

## License

MIT. Notices for the projects zm builds on (zoom-ms-utility, zoom-zt2, zoom-explorer) are in [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).
