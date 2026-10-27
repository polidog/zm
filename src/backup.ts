/**
 * P0 全パッチのバックアップ: 46 (指定パッチのダウンロード) で 300 件を読み、応答 (45 ...) をそのまま .syx で保存する。
 *
 * 読むだけで、ペダルには何も書き込まない。
 *
 *   bun run backup
 */

import { backupAll } from "./backups";
import { PATCH_COUNT, Zoom } from "./device";

using zoom = await Zoom.open();
const dir = await backupAll(zoom, (done) => {
  if (done % 50 === 0) console.log(`${done} patches`);
});
console.log(`saved ${PATCH_COUNT} patches to ${dir}`);
