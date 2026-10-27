/**
 * backups/ への退避。中身はペダルの応答 (45 ...) をそのまま .syx にしたもの。
 *
 *   backups/all-<時刻>/NNN.syx         全パッチ (NNN は 0 始まり)
 *   backups/overwritten/<時刻>-NNN.syx  保存で上書きされる直前の中身
 */

import { existsSync, mkdirSync, readdirSync, readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { PATCH_COUNT, type Zoom } from "./device";

export const BACKUP_DIR = join(import.meta.dir, "..", "backups");
const OVERWRITTEN_DIR = join(BACKUP_DIR, "overwritten");

/** ローカル時刻の YYYYMMDD-HHMMSS。 */
export function timestamp(d = new Date()) {
  const p2 = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}${p2(d.getMonth() + 1)}${p2(d.getDate())}-${p2(d.getHours())}${p2(d.getMinutes())}${p2(d.getSeconds())}`;
}

const nnn = (n: number) => String(n).padStart(3, "0");

export async function backupAll(zoom: Zoom, onProgress?: (done: number) => void): Promise<string> {
  const dir = join(BACKUP_DIR, `all-${timestamp()}`);
  mkdirSync(dir, { recursive: true });
  for (let n = 0; n < PATCH_COUNT; n++) {
    writeFileSync(join(dir, `${nnn(n)}.syx`), await zoom.getPatchSysex(n));
    onProgress?.(n + 1);
  }
  return dir;
}

/** パッチ n の今の中身を overwritten/ に退避し、そのパスを返す。 */
export async function backupBeforeOverwrite(zoom: Zoom, n: number): Promise<string> {
  mkdirSync(OVERWRITTEN_DIR, { recursive: true });
  const path = join(OVERWRITTEN_DIR, `${timestamp()}-${nnn(n)}.syx`);
  writeFileSync(path, await zoom.getPatchSysex(n));
  return path;
}

/** パッチ n の、いちばん新しい退避。 */
export function latestOverwritten(n: number): { path: string; data: Uint8Array } | undefined {
  if (!existsSync(OVERWRITTEN_DIR)) return undefined;
  const name = readdirSync(OVERWRITTEN_DIR)
    .filter((f) => f.endsWith(`-${nnn(n)}.syx`))
    .sort()
    .at(-1);
  if (!name) return undefined;
  const path = join(OVERWRITTEN_DIR, name);
  return { path, data: Uint8Array.from(readFileSync(path)) };
}
