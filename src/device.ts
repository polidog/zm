/**
 * ペダル 1 台分の操作をまとめる（読み出しのみ）。MCP サーバーはこの上に乗る。
 */

import { decodePatchSysex } from "./codec";
import { decodePatch, type Patch } from "./patch";
import * as sx from "./sysex";
import { findZoomPort, RawMidi } from "./transport";

/** G2 FOUR: バンク 30 × 10。 */
export const PATCHES_PER_BANK = 10;
export const PATCH_COUNT = 300;

export class Zoom implements Disposable {
  private constructor(
    private midi: RawMidi,
    readonly identity: sx.Identity,
  ) {}

  static async open(): Promise<Zoom> {
    const port = findZoomPort();
    if (!port) throw new Error("ZOOM のポートが見つからない");
    const midi = new RawMidi(port);
    const reply = await midi.request(sx.IDENTITY_REQUEST);
    if (!reply) {
      midi.close();
      throw new Error("Identity Request に応答がない");
    }
    return new Zoom(midi, sx.parseIdentityReply(reply));
  }

  get deviceId() {
    return this.identity.deviceId;
  }

  /** 生の SysEx（45 応答）を返す。バックアップ用。 */
  async getPatchSysex(n: number): Promise<Uint8Array> {
    if (!Number.isInteger(n) || n < 0 || n >= PATCH_COUNT) throw new Error(`パッチ番号は 0..${PATCH_COUNT - 1}`);
    const bank = Math.floor(n / PATCHES_PER_BANK);
    const reply = await this.midi.request(sx.requestPatch(this.deviceId, bank, n % PATCHES_PER_BANK), 3000);
    if (!reply || reply[4] !== 0x45) throw new Error(`パッチ ${n} を読めない: ${reply ? sx.toHex(reply) : "応答なし"}`);
    return reply;
  }

  async getPatch(n: number): Promise<Patch> {
    return decodePatch(decodePatchSysex(await this.getPatchSysex(n)).buffer);
  }

  async getCurrentPatch(): Promise<Patch> {
    const reply = await this.midi.request(sx.requestCurrentPatch(this.deviceId), 4000);
    if (!reply) throw new Error("現在パッチのダンプに応答がない");
    return decodePatch(decodePatchSysex(reply).buffer);
  }

  async listPatches(): Promise<{ n: number; name: string }[]> {
    const out = [];
    for (let n = 0; n < PATCH_COUNT; n++) out.push({ n, name: (await this.getPatch(n)).name });
    return out;
  }

  close() {
    this.midi.close();
  }

  [Symbol.dispose]() {
    this.close();
  }
}
