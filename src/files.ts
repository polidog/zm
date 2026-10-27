/**
 * ペダル内のファイル（ZD2 など）を読む。手順は mungewell/zoom-zt2 の zoomzt2.py に合わせた。
 *
 * 使うのは 一覧 (60 25/26)・存在確認 (60 25 → 60 27)・開く (60 20 02)・読む (60 22)・閉じる (60 21, 60 09) だけ。
 * 書き込み (60 20 01, 60 23) と削除 (60 24) は実装しない。
 */

import { unpack7 } from "./codec";
import * as sx from "./sysex";
import type { RawMidi } from "./transport";

const ascii = (s: string) => [...Array.from(s, (c) => c.charCodeAt(0)), 0x00];

export class PedalFiles {
  constructor(
    private midi: RawMidi,
    private dev: number,
  ) {}

  private async req(...body: number[]): Promise<Uint8Array> {
    const reply = await this.midi.request(sx.zoom(this.dev, ...body), 3000);
    if (!reply) throw new Error(`応答なし: ${sx.toHex(sx.zoom(this.dev, ...body))}`);
    return reply;
  }

  pcModeOn() {
    return this.req(0x52);
  }

  pcModeOff() {
    return this.req(0x53);
  }

  /** ペダル内の全ファイル名。 */
  async list(): Promise<string[]> {
    const names: string[] = [];
    for (let first = true; ; first = false) {
      const reply = await this.req(0x60, first ? 0x25 : 0x26, 0x00, 0x00, ...ascii("*"));
      // 応答: F0 52 00 dev 60 04 ... name は 15 バイト目から NUL まで
      if (reply[5] !== 0x04) break;
      const end = reply.indexOf(0x00, 15);
      const name = String.fromCharCode(...reply.subarray(15, end < 0 ? reply.length - 1 : end));
      if (!name) break;
      names.push(name);
    }
    return names;
  }

  async exists(name: string): Promise<boolean> {
    await this.req(0x60, 0x25, 0x00, 0x00, ...ascii(name));
    const status = await this.req(0x60, 0x05, 0x00);
    await this.req(0x60, 0x27);
    return status.subarray(-6, -1).every((b) => b === 0);
  }

  async download(name: string): Promise<Uint8Array> {
    if (!(await this.exists(name))) throw new Error(`${name} がペダルにない`);
    const open = [0x60, 0x20, 0x02, 0, 0, 0, 0, 0, 0, 0, 0, 0, ...ascii(name)];
    // zoomzt2.py は同じ open を 2 回送っている。手順をそのままなぞる
    await this.req(...open);
    await this.req(...open);
    const chunks: Uint8Array[] = [];
    try {
      for (;;) {
        await this.req(0x60, 0x05, 0x00);
        await this.req(0x60, 0x22, 0x14, 0x2f, 0x60, 0x00, 0x0c, 0x00, 0x04, 0x00, 0x00, 0x00);
        const reply = await this.req(0x60, 0x05, 0x00);
        // F0 52 00 dev 60 04 ?? ?? ?? lenL lenM | packed | crc(5) | F7
        const length = reply[9]! | (reply[10]! << 7);
        if (reply[5] !== 0x04 || length === 0) break;
        const block = unpack7(reply.subarray(11, reply.length - 6), length);
        const t = reply.subarray(-6, -1);
        const crc = (t[0]! | (t[1]! << 7) | (t[2]! << 14) | (t[3]! << 21) | ((t[4]! & 0x0f) << 28)) >>> 0;
        if (((crc ^ 0xffffffff) >>> 0) !== Bun.hash.crc32(block)) throw new Error(`${name}: CRC が合わない`);
        chunks.push(block);
      }
    } finally {
      await this.close();
    }
    return Uint8Array.from(Buffer.concat(chunks));
  }

  private async close() {
    await this.req(0x60, 0x21, 0x40, 0x00, 0x00, 0x00, 0x00);
    await this.req(0x60, 0x09);
  }
}
