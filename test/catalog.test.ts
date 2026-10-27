/**
 * カタログ (catalog/g2four.json) を実機パッチと突き合わせる。
 *
 * catalog/ と backups/ は git に入れていないので、無ければスキップする（`bun run catalog` / `bun run backup` で作る）。
 */

import { describe, expect, test } from "bun:test";
import { existsSync, readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { CATALOG_PATH, describePatch, findEffect, formatId, loadCatalog, SYSTEM_EFFECTS } from "../src/catalog";
import { decodePatchSysex } from "../src/codec";
import { decodePatch } from "../src/patch";

const BACKUPS = join(import.meta.dir, "..", "backups");
const latest = existsSync(BACKUPS)
  ? readdirSync(BACKUPS)
      .filter((d) => d.startsWith("all-"))
      .sort()
      .at(-1)
  : undefined;

describe.skipIf(!existsSync(CATALOG_PATH) || !latest)("カタログ × 実機パッチ", () => {
  const catalog = loadCatalog();
  const patches = readdirSync(join(BACKUPS, latest!)).map((f) =>
    decodePatch(decodePatchSysex(Uint8Array.from(readFileSync(join(BACKUPS, latest!, f)))).buffer),
  );

  test("パッチで使われている ID はすべてカタログか既知の特殊 ID にある", () => {
    const unknown = patches
      .flatMap((p) => p.effects.map((e) => formatId(e.id)))
      .filter((id) => !(id in SYSTEM_EFFECTS) && !catalog.effects.some((e) => e.id === id));
    expect([...new Set(unknown)]).toEqual([]);
  });

  test("パラメータ値はカタログの max 以下で、カタログより後ろの param は 0", () => {
    const bad: string[] = [];
    for (const p of patches) {
      for (const fx of p.effects) {
        const e = findEffect(catalog, fx.id);
        if (!e) continue;
        fx.params.forEach((v, i) => {
          const max = e.params[i]?.max ?? 0;
          if (v > max) bad.push(`${p.name} / ${e.name} param${i + 1}=${v} > ${max}`);
        });
      }
    }
    expect(bad).toEqual([]);
  });

  test("既定値は max 以下", () => {
    const bad = catalog.effects.flatMap((e) =>
      e.params.filter((p) => p.default > p.max).map((p) => `${e.name}.${p.name}`),
    );
    expect(bad).toEqual([]);
  });

  test("describePatch で名前が引ける", () => {
    const d = describePatch(catalog, patches[0]!);
    expect(d.slots.every((s) => s.effect !== "(unknown)")).toBe(true);
    expect(d.slots[0]!.params.length).toBeGreaterThan(0);
  });
});
