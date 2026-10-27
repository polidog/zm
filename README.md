# zm

English | [日本語](README.ja.md)

Control a ZOOM G2 FOUR, MS-80IR+ and the older MS-50G / MS-70CDR from a CLI, a TUI, or an MCP server (shape your tone from Claude Code). It is a single Go binary.

zm reads and writes ALSA rawmidi (`/dev/snd/midiCxDy`) directly, so it runs on Linux only.

zm is not an official ZOOM tool. The commands that write to the pedal (`save` / `restore`) back up what they overwrite, but take a `zm backup` before you use them.

## Supported pedals

| Pedal | Patches | Effect catalog | Tested firmware |
| --- | --- | --- | --- |
| ZOOM G2 FOUR | 300 | built from the pedal with `zm catalog` | 1.00 |
| ZOOM MS-80IR+ | 80 | built from the pedal with `zm catalog` | 1.00 |
| ZOOM MS-50G | 50 | embedded (172 effects) | 3.10 |
| ZOOM MS-70CDR | 50 | embedded (137 effects) | 2.10 |

The G2 FOUR, MS-50G and MS-70CDR work the same way: reading, editing, saving, restoring and backups from the CLI and MCP server, and reading, editing and saving from the TUI. `rename` is CLI only and works on the MS-50G / MS-70CDR only.

The MS-80IR+ is read-only for now (`info` / `list` / `show` / `effects` / `backup` / `catalog`). Editing and saving stay disabled until they have been checked on the pedal. Its IR effects have no ZD2 file and show as `(unknown)`.

Other models are not recognized. The MS-60B uses a different patch format, and the other MS+ models and FOUR models have not been tested.

## Install

```sh
go install github.com/polidog/zm/cmd/zm@latest
```

Requires Go 1.27 or later. To build from a checkout: `go build -o zm ./cmd/zm`.

## Usage

```sh
zm info                           model, firmware, number of patches
zm list                           number and name of every patch
zm show [n]                       contents of a patch (omit n for the patch currently playing)
zm effects [group]                list effects
zm effect <name>                  an effect's description and parameters
zm set-effect <slot> <effect>     replace an effect (not saved)
zm set <slot> <param> <value>     change a parameter (not saved)
zm on|off|toggle <slot>           switch an effect on / off (not saved)
zm rename <name>                  rename the patch (not saved; legacy MS only for now)
zm save <n> [--yes]               save the current patch to n (asks first, backs up what it overwrites)
zm restore <n> [--yes]            undo a save to n
zm backup                         back up every patch to backups/
zm catalog [--refresh]            build the effect catalog from the pedal's ZD2 files
zm tui                            TUI
zm mcp                            MCP server (stdio)
zm probe                          connectivity check (for development)
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
