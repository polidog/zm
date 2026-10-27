/**
 * ペダル 1 台分の操作をまとめる。MCP サーバーはこの上に乗る。
 *
 * 書き込みは 2 種類。どちらも open({ edit: true }) でエディタモードを ON、AUTO SAVE を OFF にしてから使う。
 * - 64 20 による現在パッチの個別編集（保存しない。パッチを選び直せば戻る）
 * - 45 によるパッチメモリへの保存（フラッシュに書く）
 */

import { decodePatchSysex, encodePatchSysex } from "./codec";
import { decodePatch, encodePatch, type Patch } from "./patch";
import * as sx from "./sysex";
import { findZoomPort, RawMidi } from "./transport";

/** G2 FOUR: バンク 30 × 10。 */
export const PATCHES_PER_BANK = 10;
export const PATCH_COUNT = 300;

/** 5 バイト目以降（F0 52 00 dev の後ろ）が prefix で始まるか。 */
const zoomReply = (prefix: number[]) => (msg: Uint8Array) => prefix.every((b, i) => msg[4 + i] === b);

/** 0 始まりの通し番号 → バンク / プログラム。 */
function location(n: number) {
  if (!Number.isInteger(n) || n < 0 || n >= PATCH_COUNT) throw new Error(`パッチ番号は 0..${PATCH_COUNT - 1}`);
  return { bank: Math.floor(n / PATCHES_PER_BANK), program: n % PATCHES_PER_BANK };
}

export class Zoom implements Disposable, AsyncDisposable {
  private constructor(
    private midi: RawMidi,
    readonly identity: sx.Identity,
    readonly editable: boolean,
  ) {}

  static async open({ edit = false } = {}): Promise<Zoom> {
    const port = findZoomPort();
    if (!port) throw new Error("ZOOM のポートが見つからない");
    const midi = new RawMidi(port);
    try {
      const reply = await midi.request(sx.IDENTITY_REQUEST, 2000, (m) => m[1] === 0x7e && m[4] === 0x02);
      if (!reply) throw new Error("Identity Request に応答がない");
      const zoom = new Zoom(midi, sx.parseIdentityReply(reply), edit);
      if (edit) {
        await zoom.expect(sx.editorModeOn(zoom.deviceId), [0x00, 0x00]);
        await zoom.expect(sx.setAutoSave(zoom.deviceId, false), [0x64, 0x20, 0x01]);
      }
      return zoom;
    } catch (e) {
      midi.close();
      throw e;
    }
  }

  /**
   * 送って、5 バイト目以降が prefix で始まる応答を待つ。
   * 編集するとペダルは 64 12 00 (現在パッチの通知) を自分から送ってくるので、それ以外の応答は読み飛ばす。
   */
  private async expect(msg: Uint8Array, prefix: number[], timeoutMs = 2000): Promise<Uint8Array> {
    const reply = await this.midi.request(msg, timeoutMs, zoomReply(prefix));
    if (!reply) throw new Error(`${sx.toHex(msg.subarray(0, 12))}… が受理されない`);
    return reply;
  }

  private assertEditable() {
    if (!this.editable) throw new Error("書き込みには Zoom.open({ edit: true }) が要る");
  }

  get deviceId() {
    return this.identity.deviceId;
  }

  /** 生の SysEx（45 応答）を返す。バックアップ用。 */
  async getPatchSysex(n: number): Promise<Uint8Array> {
    const { bank, program } = location(n);
    const reply = await this.midi.request(
      sx.requestPatch(this.deviceId, bank, program),
      3000,
      zoomReply([0x45]),
    );
    if (!reply) throw new Error(`パッチ ${n} を読めない`);
    return reply;
  }

  async getPatch(n: number): Promise<Patch> {
    return decodePatch(decodePatchSysex(await this.getPatchSysex(n)).buffer);
  }

  async getCurrentPatch(): Promise<Patch> {
    // 64 12 00 は編集時の通知。要求への応答は 64 12 01
    const reply = await this.midi.request(sx.requestCurrentPatch(this.deviceId), 4000, zoomReply([0x64, 0x12, 0x01]));
    if (!reply) throw new Error("現在パッチのダンプに応答がない");
    return decodePatch(decodePatchSysex(reply).buffer);
  }

  async listPatches(): Promise<{ n: number; name: string }[]> {
    const out = [];
    for (let n = 0; n < PATCH_COUNT; n++) out.push({ n, name: (await this.getPatch(n)).name });
    return out;
  }

  // ---- 書き込み（現在パッチのみ。保存しない） ----

  /**
   * 64 20 00 で 1 項目変える。値が変わらないときペダルは何も返さないので、
   * 受理が来なければ現在パッチを読み直し、値が合っていれば成功とする。
   */
  private async edit(slot: number, paramNo: number, value: number, applied: (p: Patch) => boolean) {
    this.assertEditable();
    try {
      await this.expect(sx.setParameter(this.deviceId, slot, paramNo, value), [0x64, 0x20, 0x01], 1000);
    } catch (e) {
      if (!applied(await this.getCurrentPatch())) throw e;
    }
  }

  /** paramIndex は 0 始まり（patch.effects[slot].params の添字）。 */
  async setParam(slot: number, paramIndex: number, value: number) {
    await this.edit(slot, paramIndex + 2, value, (p) => p.effects[slot]?.params[paramIndex] === value);
  }

  async setEnabled(slot: number, on: boolean) {
    await this.edit(slot, 0, on ? 1 : 0, (p) => p.effects[slot]?.enabled === on);
  }

  /**
   * エフェクトを差し替える。応答は 64 20 01 ではなく 64 12 00（差し替え後の現在パッチ全体）なので、それを返す。
   * パラメータは初めて使うエフェクトなら既定値、同じ電源投入中に使ったことがあればそのときの値になる。
   */
  async setEffect(slot: number, id: number): Promise<Patch> {
    this.assertEditable();
    const reply = await this.expect(sx.setParameter(this.deviceId, slot, 1, id), [0x64, 0x12, 0x00], 4000);
    return decodePatch(decodePatchSysex(reply).buffer);
  }

  /** 編集用に開いたときは、エディタモード OFF の応答まで待ってから閉じる（残った応答が次の接続に混ざらないように）。 */
  async closeAsync() {
    if (this.editable) await this.expect(sx.editorModeOff(this.deviceId), [0x00, 0x00]).catch(() => {});
    this.midi.close();
  }

  /**
   * パッチメモリ n に保存する（フラッシュに書く）。送った後に読み直し、中身が一致しなければ例外。
   * 呼ぶ側で、上書きされる中身を getPatchSysex で退避しておくこと。
   */
  async savePatch(n: number, patch: Patch) {
    this.assertEditable();
    await this.writePatchSysex(n, encodePatchSysex(this.deviceId, { kind: "program", ...location(n) }, encodePatch(patch)));
    const back = await this.getPatch(n);
    const body = (p: Patch) => JSON.stringify({ ...p, trailer: "" });
    if (body(back) !== body(patch)) throw new Error(`パッチ ${n} に保存した中身が読み直しと一致しない`);
  }

  /** getPatchSysex で取った 45 をそのまま書き戻す（バックアップからの復元用）。 */
  async writePatchSysex(n: number, msg: Uint8Array) {
    this.assertEditable();
    const { bank, program } = location(n);
    const at = decodePatchSysex(msg).location;
    if (at.kind !== "program" || at.bank !== bank || at.program !== program) {
      throw new Error(`パッチ ${n} 宛ての SysEx ではない`);
    }
    await this.expect(msg, [0x00, 0x00], 5000);
  }

  close() {
    if (this.editable) void this.midi.send(sx.editorModeOff(this.deviceId));
    this.midi.close();
  }

  [Symbol.dispose]() {
    this.close();
  }

  [Symbol.asyncDispose]() {
    return this.closeAsync();
  }
}
