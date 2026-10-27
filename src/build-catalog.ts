/**
 * P2: ペダルの ZD2 を全部読み出し、catalog/g2four.json を作る。
 *
 * 読み出した ZD2 は catalog/zd2/ にキャッシュし、2 回目以降はペダルに触らない（--refresh で取り直す）。
 * ペダルには書き込まない。
 *
 *   bun run catalog [--refresh]
 */

import { existsSync, mkdirSync, readdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { parseArgs } from "node:util";
import { CATALOG_PATH, formatId, type Catalog } from "./catalog";
import { PedalFiles } from "./files";
import * as sx from "./sysex";
import { findZoomPort, RawMidi } from "./transport";
import { parseZd2 } from "./zd2";

const { values: args } = parseArgs({ options: { refresh: { type: "boolean", default: false } } });

const cache = join(dirname(CATALOG_PATH), "zd2");
mkdirSync(cache, { recursive: true });

if (args.refresh || readdirSync(cache).length === 0) {
  const port = findZoomPort();
  if (!port) throw new Error("ZOOM のポートが見つからない");
  using midi = new RawMidi(port);
  const ident = sx.parseIdentityReply((await midi.request(sx.IDENTITY_REQUEST))!);
  const files = new PedalFiles(midi, ident.deviceId);
  await files.pcModeOn();
  try {
    const names = (await files.list()).filter((n) => n.endsWith(".ZD2"));
    for (const [i, name] of names.entries()) {
      const path = join(cache, name);
      if (!args.refresh && existsSync(path)) continue;
      writeFileSync(path, await files.download(name));
      console.log(`[${i + 1}/${names.length}] ${name}`);
    }
  } finally {
    await files.pcModeOff();
  }
}

const effects: Catalog["effects"] = [];
for (const file of readdirSync(cache).filter((f) => f.endsWith(".ZD2")).sort()) {
  try {
    const z = parseZd2(Uint8Array.from(readFileSync(join(cache, file))));
    effects.push({ ...z, id: formatId(z.id), file });
  } catch (e) {
    console.error(`${file}: ${(e as Error).message}`);
  }
}
effects.sort((a, b) => a.id.localeCompare(b.id));

const catalog: Catalog = { device: "ZOOM G2 FOUR", generatedAt: new Date().toISOString(), effects };
writeFileSync(CATALOG_PATH, JSON.stringify(catalog, null, 2) + "\n");
console.log(`${effects.length} effects → ${CATALOG_PATH}`);
