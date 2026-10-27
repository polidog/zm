/**
 * L2 (枠): パッチ SysEx の 7bit パックと CRC。
 *
 * 応答の形（G2 FOUR で確認）:
 *   45: F0 52 00 <dev> 45 00 00 bL bM pL pM | lenL lenM | packed(len) | crc(5) | F7
 *   64: F0 52 00 <dev> 64 12 01             | lenL lenM | packed(len) | crc(5) | F7
 *
 * len は 8bit に戻した後のバイト数（G2 FOUR は常に 832）。
 * crc は ~CRC32(8bit データ) を 7bit ずつ下位から 5 バイトに分けたもの。
 */

import { split14, ZOOM_MANUFACTURER } from "./sysex";

/** 7 バイトごとに先頭へ MSB をまとめる。先頭バイトの bit6 が 1 つ目のデータに対応する。 */
export function pack7(data: Uint8Array): Uint8Array {
  const out: number[] = [];
  for (let i = 0; i < data.length; i += 7) {
    const group = data.subarray(i, i + 7);
    let hi = 0;
    group.forEach((b, j) => {
      if (b & 0x80) hi |= 1 << (6 - j);
    });
    out.push(hi, ...Array.from(group, (b) => b & 0x7f));
  }
  return Uint8Array.from(out);
}

export function unpack7(packed: Uint8Array, length: number): Uint8Array {
  const out = new Uint8Array(length);
  let n = 0;
  for (let i = 0; n < length; i += 8) {
    const hi = packed[i]!;
    for (let j = 0; j < 7 && n < length; j++) {
      out[n++] = packed[i + 1 + j]! | (hi & (1 << (6 - j)) ? 0x80 : 0);
    }
  }
  return out;
}

export const packedLength = (n: number) => Math.floor(n / 7) * 8 + (n % 7 ? (n % 7) + 1 : 0);

export function crc7(data: Uint8Array): Uint8Array {
  const v = ~Bun.hash.crc32(data) >>> 0;
  // 32bit を 7bit ずつ。最後のバイトは上位 4bit だけ
  return Uint8Array.from([0, 7, 14, 21, 28], (s) => (v >>> s) & 0x7f);
}

export type PatchLocation = { kind: "program"; bank: number; program: number } | { kind: "current" };

const join14 = (l: number, m: number) => l | (m << 7);

/** パッチ SysEx を 8bit のバッファへ戻す。CRC が合わなければ例外。 */
export function decodePatchSysex(msg: Uint8Array): { location: PatchLocation; buffer: Uint8Array } {
  if (msg[0] !== 0xf0 || msg[1] !== ZOOM_MANUFACTURER || msg.at(-1) !== 0xf7) {
    throw new Error("ZOOM の SysEx ではない");
  }
  let location: PatchLocation;
  let off: number;
  if (msg[4] === 0x45) {
    location = { kind: "program", bank: join14(msg[7]!, msg[8]!), program: join14(msg[9]!, msg[10]!) };
    off = 11;
  } else if (msg[4] === 0x64 && msg[5] === 0x12) {
    location = { kind: "current" };
    off = 7;
  } else {
    throw new Error(`パッチの SysEx ではない: ${msg[4]?.toString(16)}`);
  }
  const length = join14(msg[off]!, msg[off + 1]!);
  const packed = msg.subarray(off + 2, msg.length - 1);
  const pl = packedLength(length);
  if (packed.length !== pl + 5) throw new Error(`長さが合わない: ${packed.length} != ${pl} + 5`);
  const buffer = unpack7(packed, length);
  const crc = crc7(buffer);
  if (!crc.every((b, i) => b === packed[pl + i])) throw new Error("CRC が合わない");
  return { location, buffer };
}

export function encodePatchSysex(dev: number, location: PatchLocation, buffer: Uint8Array): Uint8Array {
  const head =
    location.kind === "program"
      ? [0x45, 0x00, 0x00, ...split14(location.bank), ...split14(location.program)]
      : [0x64, 0x12, 0x01];
  return Uint8Array.from([
    0xf0,
    ZOOM_MANUFACTURER,
    0x00,
    dev,
    ...head,
    ...split14(buffer.length),
    ...pack7(buffer),
    ...crc7(buffer),
    0xf7,
  ]);
}
