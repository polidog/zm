/**
 * L1: ALSA rawmidi (/dev/snd/midiCxDy) で SysEx を送受信する。
 *
 * 外部ライブラリを使わず、デバイスファイルを O_NONBLOCK で開いて直接読み書きする。
 */

import { closeSync, constants, openSync, readdirSync, readFileSync, readSync, writeSync } from "node:fs";

export type MidiPort = { card: number; device: number; name: string };

export const portPath = (p: MidiPort) => `/dev/snd/midiC${p.card}D${p.device}`;

/** rawmidi デバイスを持つサウンドカードを列挙する。 */
export function listPorts(): MidiPort[] {
  const names = new Map<number, string>();
  for (const line of readFileSync("/proc/asound/cards", "utf8").split("\n")) {
    const m = line.match(/^\s*(\d+)\s+\[[^\]]*\]:\s*(.*)$/);
    if (m) names.set(Number(m[1]), m[2]!.trim());
  }
  return readdirSync("/dev/snd")
    .flatMap((f) => {
      const m = f.match(/^midiC(\d+)D(\d+)$/);
      if (!m) return [];
      const card = Number(m[1]);
      return [{ card, device: Number(m[2]), name: names.get(card) ?? "?" }];
    })
    .sort((a, b) => a.card - b.card || a.device - b.device);
}

export const findZoomPort = () => listPorts().find((p) => /zoom/i.test(p.name));

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));

const isEagain = (e: unknown) => (e as NodeJS.ErrnoException).code === "EAGAIN";

export class RawMidi {
  private fd: number;
  private buf: number[] = [];
  private chunk = Buffer.alloc(4096);

  constructor(readonly port: MidiPort) {
    this.fd = openSync(portPath(port), constants.O_RDWR | constants.O_NONBLOCK);
  }

  close() {
    closeSync(this.fd);
  }

  [Symbol.dispose]() {
    this.close();
  }

  async send(data: Uint8Array) {
    let off = 0;
    while (off < data.length) {
      try {
        off += writeSync(this.fd, data, off);
      } catch (e) {
        if (!isEagain(e)) throw e;
        await sleep(2);
      }
    }
  }

  /** 読めるだけ読んで buf に足す。読めたバイト数を返す。 */
  private pump(): number {
    let total = 0;
    for (;;) {
      let n: number;
      try {
        n = readSync(this.fd, this.chunk, 0, this.chunk.length, null);
      } catch (e) {
        if (isEagain(e)) return total;
        throw e;
      }
      if (n <= 0) return total;
      for (let i = 0; i < n; i++) this.buf.push(this.chunk[i]!);
      total += n;
    }
  }

  /** 溜まっている受信データを捨てる。 */
  drain() {
    this.pump();
    this.buf = [];
  }

  /** F0 ... F7 を 1 つ受け取る。SysEx 以外のバイトは読み捨てる。 */
  async recvSysex(timeoutMs = 2000): Promise<Uint8Array | null> {
    const deadline = Date.now() + timeoutMs;
    for (;;) {
      this.pump();
      const start = this.buf.indexOf(0xf0);
      if (start < 0) {
        this.buf = [];
      } else {
        const end = this.buf.indexOf(0xf7, start);
        if (end >= 0) {
          const msg = Uint8Array.from(this.buf.slice(start, end + 1));
          this.buf = this.buf.slice(end + 1);
          return msg;
        }
      }
      if (Date.now() >= deadline) return null;
      await sleep(5);
    }
  }

  async request(data: Uint8Array, timeoutMs = 2000) {
    this.drain();
    await this.send(data);
    return this.recvSysex(timeoutMs);
  }
}
