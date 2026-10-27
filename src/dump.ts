/**
 * P1 dump_patch: パッチを JSON で出す。ペダルからでも、保存済みの .syx からでも読める。
 *
 *   bun run dump <n>          ペダルのパッチ n (0..299)
 *   bun run dump current      いま鳴っているパッチ
 *   bun run dump list         全パッチの番号と名前
 *   bun run dump file.syx     保存済みの SysEx
 *
 * --named を付けると、カタログでエフェクト名・パラメータ名を引いた形で出す。
 */

import { readFileSync } from "node:fs";
import { describePatch, loadCatalog } from "./catalog";
import { decodePatchSysex } from "./codec";
import { Zoom } from "./device";
import { decodePatch, type Patch } from "./patch";

const named = process.argv.includes("--named");
const arg = process.argv.slice(2).find((a) => !a.startsWith("--"));
if (!arg) {
  console.error("usage: bun run dump <n|current|list|file.syx> [--named]");
  process.exit(1);
}

let result: unknown;
if (arg.endsWith(".syx")) {
  result = decodePatch(decodePatchSysex(Uint8Array.from(readFileSync(arg))).buffer);
} else {
  using zoom = await Zoom.open();
  result =
    arg === "current" ? await zoom.getCurrentPatch()
    : arg === "list" ? await zoom.listPatches()
    : await zoom.getPatch(Number(arg));
}
if (named && arg !== "list") result = describePatch(loadCatalog(), result as Patch);
console.log(JSON.stringify(result, null, 2));
