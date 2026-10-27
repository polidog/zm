/**
 * P1 dump_patch: パッチを JSON で出す。ペダルからでも、保存済みの .syx からでも読める。
 *
 *   bun run dump <n>          ペダルのパッチ n (0..299)
 *   bun run dump current      いま鳴っているパッチ
 *   bun run dump list         全パッチの番号と名前
 *   bun run dump file.syx     保存済みの SysEx
 */

import { readFileSync } from "node:fs";
import { decodePatchSysex } from "./codec";
import { Zoom } from "./device";
import { decodePatch } from "./patch";

const arg = process.argv[2];
if (!arg) {
  console.error("usage: bun run dump <n|current|list|file.syx>");
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
console.log(JSON.stringify(result, null, 2));
