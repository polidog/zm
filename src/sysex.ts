/**
 * SysEx メッセージの組み立てと、Identity Reply の解釈。
 *
 * バイト列は MS+ / Four 系で確認されているもの（thammer/zoom-explorer, mungewell/zoom-zt2）。
 * G2 FOUR で同じ応答が返るかは P0 で確かめる。
 */

export const ZOOM_MANUFACTURER = 0x52;
/** MS-50G+ / G1 FOUR で共通の機種 ID。実機の Identity Reply から取れればそちらを優先する */
export const DEFAULT_DEVICE_ID = 0x6e;

export const IDENTITY_REQUEST = Uint8Array.of(0xf0, 0x7e, 0x7f, 0x06, 0x01, 0xf7);

export const zoom = (deviceId: number, ...body: number[]) =>
  Uint8Array.of(0xf0, ZOOM_MANUFACTURER, 0x00, deviceId, ...body, 0xf7);

export const editorModeOn = (dev: number) => zoom(dev, 0x50);
export const editorModeOff = (dev: number) => zoom(dev, 0x51);
export const getCurrentProgram = (dev: number) => zoom(dev, 0x33);
/** 応答は 64 12 で始まる。 */
export const requestCurrentPatch = (dev: number) => zoom(dev, 0x64, 0x13);

export const split14 = (v: number): [number, number] => [v & 0x7f, (v >> 7) & 0x7f];

export const requestPatch = (dev: number, bank: number, program: number) =>
  zoom(dev, 0x46, 0x00, 0x00, ...split14(bank), ...split14(program));

export type Identity = {
  manufacturer: number;
  family: number;
  model: number;
  version: string;
  /** Zoom 独自メッセージの 4 バイト目に入れる機種 ID（family の下位バイト）。 */
  deviceId: number;
};

export const toHex = (data: Uint8Array) =>
  Array.from(data, (b) => b.toString(16).padStart(2, "0")).join(" ");

export const fromHex = (s: string) =>
  Uint8Array.from(s.trim().split(/\s+/), (h) => parseInt(h, 16));

export function parseIdentityReply(msg: Uint8Array): Identity {
  // F0 7E <ch> 06 02 <mfr> <fam L> <fam M> <model L> <model M> <ver x4> F7
  if (msg.length < 15 || msg[0] !== 0xf0 || msg[1] !== 0x7e || msg[3] !== 0x06 || msg[4] !== 0x02) {
    throw new Error(`Identity Reply ではない: ${toHex(msg)}`);
  }
  const family = msg[6]! | (msg[7]! << 7);
  return {
    manufacturer: msg[5]!,
    family,
    model: msg[8]! | (msg[9]! << 7),
    version: new TextDecoder().decode(msg.slice(10, 14)),
    deviceId: family & 0x7f,
  };
}
