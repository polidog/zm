/**
 * 実機ダンプのラウンドトリップ: SysEx → 8bit → Patch(JSON) → 8bit → SysEx がバイト一致するか。
 *
 * backups/ は git に入れていないので、無ければスキップする（`bun run backup` で作る）。
 */

import { describe, expect, test } from "bun:test";
import { existsSync, readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { decodePatchSysex, encodePatchSysex, pack7, unpack7 } from "../src/codec";
import { decodePatch, encodePatch, PATCH_BUFFER_BYTES } from "../src/patch";

const BACKUPS = join(import.meta.dir, "..", "backups");
const latest = existsSync(BACKUPS)
  ? readdirSync(BACKUPS)
      .filter((d) => d.startsWith("all-"))
      .sort()
      .at(-1)
  : undefined;
const files = latest
  ? readdirSync(join(BACKUPS, latest))
      .sort()
      .map((f) => join(BACKUPS, latest, f))
  : [];
const currentDumps = existsSync(BACKUPS)
  ? readdirSync(BACKUPS)
      .filter((f) => f.endsWith("-current-patch.syx"))
      .map((f) => join(BACKUPS, f))
  : [];

test("pack7 / unpack7 は逆変換", () => {
  const data = Uint8Array.from({ length: 50 }, (_, i) => (i * 37) & 0xff);
  expect(unpack7(pack7(data), data.length)).toEqual(data);
});

describe.skipIf(files.length === 0)(`実機ダンプ (${latest})`, () => {
  test.each(files)("%s", (file) => {
    const raw = Uint8Array.from(readFileSync(file));
    const { location, buffer } = decodePatchSysex(raw);
    expect(buffer.length).toBe(PATCH_BUFFER_BYTES);

    const patch = decodePatch(buffer);
    expect(patch.ids).toEqual(patch.effects.map((e) => e.id));

    const json = JSON.parse(JSON.stringify(patch));
    const rebuilt = encodePatch(json);
    expect(Buffer.from(rebuilt).toString("hex")).toBe(Buffer.from(buffer).toString("hex"));
    expect(encodePatchSysex(raw[3]!, location, rebuilt)).toEqual(raw);
  });
});

describe.skipIf(currentDumps.length === 0)("現在パッチのダンプ (64 12)", () => {
  test.each(currentDumps)("%s", (file) => {
    const raw = Uint8Array.from(readFileSync(file));
    const { location, buffer } = decodePatchSysex(raw);
    expect(location.kind).toBe("current");
    expect(encodePatchSysex(raw[3]!, location, encodePatch(decodePatch(buffer)))).toEqual(raw);
  });
});
