/**
 * P0 全パッチのバックアップ: 46 (指定パッチのダウンロード) で 300 件を読み、応答 (45 ...) をそのまま .syx で保存する。
 *
 * 読むだけで、ペダルには何も書き込まない。
 *
 *   bun run backup [--out backups]
 */

import { mkdirSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { parseArgs } from "node:util";
import { PATCH_COUNT, Zoom } from "./device";

const { values: args } = parseArgs({
  options: { out: { type: "string", default: "backups" } },
});

using zoom = await Zoom.open();

const d = new Date();
const p2 = (n: number) => String(n).padStart(2, "0");
const stamp = `${d.getFullYear()}${p2(d.getMonth() + 1)}${p2(d.getDate())}-${p2(d.getHours())}${p2(d.getMinutes())}${p2(d.getSeconds())}`;
const dir = join(args.out, `all-${stamp}`);
mkdirSync(dir, { recursive: true });

for (let n = 0; n < PATCH_COUNT; n++) {
  writeFileSync(join(dir, `${String(n).padStart(3, "0")}.syx`), await zoom.getPatchSysex(n));
  if ((n + 1) % 50 === 0) console.log(`${n + 1} patches`);
}
console.log(`saved ${PATCH_COUNT} patches to ${dir}`);
