/**
 * L2 (中身): PTCF パッチ ⇔ JSON。
 *
 * 構造は mungewell/zoom-zt2 の decode_preset.py (MIT) を元にした。
 *
 *   PTCF | size u32 | version u32 | fxCount u32 | target u32 | misc[6] | shortName[10] | ids u32[fxCount]
 *   TXJ1 | len | Shift-JIS        （日本語の説明。空なら len=0）
 *   TXE1 | len | ascii            （英語の説明。空なら len=0）
 *   EDTB | len | effect[fxCount]  （1 つ 24 バイト）
 *   PRM2 | len | 32 バイト         （パッチ全体の設定）
 *   NAME | len | ascii            （14 文字まで空白で埋め、NUL 2 つで 16 バイト）
 *
 * PTCF は 832 バイト固定のバッファの先頭に入り、後ろには前のデータの残骸が残っている。
 * 意味の分からないビットは JSON に残し、encode でバイト一致させる。
 */

const ascii = new TextDecoder("latin1");
const encodeAscii = (s: string) => Uint8Array.from(s, (c) => c.charCodeAt(0));
const hex = (b: Uint8Array) => Buffer.from(b).toString("hex");
const unhex = (s: string) => Uint8Array.from(Buffer.from(s, "hex"));

/** バイト列を little-endian の整数として読む（ビット操作用）。 */
function leBig(b: Uint8Array): bigint {
  let v = 0n;
  for (let i = b.length - 1; i >= 0; i--) v = (v << 8n) | BigInt(b[i]!);
  return v;
}

function bigLe(v: bigint, n: number): Uint8Array {
  const out = new Uint8Array(n);
  for (let i = 0; i < n; i++) {
    out[i] = Number(v & 0xffn);
    v >>= 8n;
  }
  return out;
}

/** 下位ビットから順に並ぶフィールドを読み書きする。 */
class Bits {
  private pos = 0n;
  constructor(public v: bigint) {}
  read(width: number): number {
    const x = (this.v >> this.pos) & ((1n << BigInt(width)) - 1n);
    this.pos += BigInt(width);
    return Number(x);
  }
  write(width: number, x: number) {
    const mask = ((1n << BigInt(width)) - 1n) << this.pos;
    this.v = (this.v & ~mask) | ((BigInt(x) << this.pos) & mask);
    this.pos += BigInt(width);
  }
  skip(width: number) {
    this.pos += BigInt(width);
  }
}

// ---- EDTB: エフェクト 1 つ = 24 バイト = 192bit ----

/** param1..param12 のビット幅。 */
const PARAM_WIDTHS = [12, 12, 12, 12, 12, 8, 8, 8, 12, 12, 12, 12];
const EFFECT_BYTES = 24;
const EFFECT_KNOWN_BITS = 1 + 29 + PARAM_WIDTHS.reduce((a, b) => a + b);

export type Effect = {
  id: number;
  enabled: boolean;
  params: number[];
  /** 上位の意味不明ビット（hex）。 */
  extra: string;
};

function decodeEffect(b: Uint8Array): Effect {
  const v = leBig(b);
  const bits = new Bits(v);
  const enabled = bits.read(1) === 1;
  const id = bits.read(29);
  const params = PARAM_WIDTHS.map((w) => bits.read(w));
  return { id, enabled, params, extra: (v >> BigInt(EFFECT_KNOWN_BITS)).toString(16) };
}

function encodeEffect(e: Effect): Uint8Array {
  const bits = new Bits(BigInt("0x" + e.extra) << BigInt(EFFECT_KNOWN_BITS));
  bits.write(1, e.enabled ? 1 : 0);
  bits.write(29, e.id);
  PARAM_WIDTHS.forEach((w, i) => bits.write(w, e.params[i] ?? 0));
  return bigLe(bits.v, EFFECT_BYTES);
}

// ---- PRM2: パッチ全体 32 バイト ----
// 下位ビットからの位置。zoom-zt2 の PPRM_v2_rev を逆順に数えたもの

const PRM2_BYTES = 32;
const PRM2_FIELDS = {
  patchVolume: { at: 77, width: 7 },
  editSlot: { at: 85, width: 3 },
  tempo: { at: 244, width: 8 },
} as const;

export type Prm2 = { patchVolume: number; editSlot: number; tempo: number; raw: string };

function decodePrm2(b: Uint8Array): Prm2 {
  const v = leBig(b);
  const field = ({ at, width }: { at: number; width: number }) =>
    Number((v >> BigInt(at)) & ((1n << BigInt(width)) - 1n));
  return {
    patchVolume: field(PRM2_FIELDS.patchVolume),
    editSlot: field(PRM2_FIELDS.editSlot),
    tempo: field(PRM2_FIELDS.tempo),
    raw: hex(b),
  };
}

function encodePrm2(p: Prm2): Uint8Array {
  let v = leBig(unhex(p.raw));
  for (const [key, { at, width }] of Object.entries(PRM2_FIELDS)) {
    const mask = ((1n << BigInt(width)) - 1n) << BigInt(at);
    v = (v & ~mask) | ((BigInt(p[key as keyof typeof PRM2_FIELDS]) << BigInt(at)) & mask);
  }
  return bigLe(v, PRM2_BYTES);
}

// ---- PTCF ----

export type Patch = {
  name: string;
  version: number;
  target: number;
  /** ヘッダ内の 6 バイト（未解析）。 */
  misc: string;
  /** ヘッダ内の 10 バイトの名前欄。 */
  shortName: string;
  /** ヘッダの ids 欄。実機では常に effects[].id と同じで、encode は effects から作る。 */
  ids: number[];
  /** TXJ1 の Shift-JIS を読んだもの（表示用）。書き戻しは txj1 から。 */
  descriptionJa: string;
  /** TXJ1 の生バイト（hex）。TextEncoder は Shift-JIS を書けないのでこちらを正とする。 */
  txj1: string;
  descriptionEn: string;
  effects: Effect[];
  prm2: Prm2;
  /** 832 バイトのバッファのうち PTCF の後ろ（前のデータの残骸）。 */
  trailer: string;
};

class Reader {
  private dv: DataView;
  off = 0;
  constructor(private b: Uint8Array) {
    this.dv = new DataView(b.buffer, b.byteOffset, b.byteLength);
  }
  u32() {
    const v = this.dv.getUint32(this.off, true);
    this.off += 4;
    return v;
  }
  bytes(n: number) {
    const v = this.b.subarray(this.off, this.off + n);
    this.off += n;
    return v;
  }
  tag(expect: string) {
    const t = ascii.decode(this.bytes(4));
    if (t !== expect) throw new Error(`${expect} のはずが ${JSON.stringify(t)} (offset ${this.off - 4})`);
  }
  chunk(tag: string) {
    this.tag(tag);
    return this.bytes(this.u32());
  }
}

const stripNul = (b: Uint8Array) => ascii.decode(b).replace(/\0+$/, "");

export function decodePatch(buffer: Uint8Array): Patch {
  const r = new Reader(buffer);
  r.tag("PTCF");
  const size = r.u32();
  const version = r.u32();
  if (version < 2) throw new Error(`PTCF version ${version} は未対応`);
  const fxCount = r.u32();
  const target = r.u32();
  const misc = hex(r.bytes(6));
  const shortName = stripNul(r.bytes(10));
  const ids = Array.from({ length: fxCount }, () => r.u32());
  const txj1 = r.chunk("TXJ1");
  const descriptionEn = stripNul(r.chunk("TXE1"));
  const edtb = r.chunk("EDTB");
  const effects = Array.from({ length: fxCount }, (_, i) =>
    decodeEffect(edtb.subarray(i * EFFECT_BYTES, (i + 1) * EFFECT_BYTES)),
  );
  const prm2 = decodePrm2(r.chunk("PRM2"));
  const name = stripNul(r.chunk("NAME")).trimEnd();
  if (r.off !== size) throw new Error(`PTCF のサイズが合わない: ${r.off} != ${size}`);
  return {
    name,
    version,
    target,
    misc,
    shortName,
    ids,
    descriptionJa: new TextDecoder("shift_jis").decode(txj1).replace(/\0+$/, ""),
    txj1: hex(txj1),
    descriptionEn,
    effects,
    prm2,
    trailer: hex(buffer.subarray(size)),
  };
}

/** n バイトの領域に詰め、残りを NUL で埋める。 */
const fit = (b: Uint8Array, n: number) => {
  const out = new Uint8Array(n);
  out.set(b.subarray(0, n));
  return out;
};

/** 4 バイト境界まで NUL で埋める（ちょうど割り切れれば NUL は付かない）。 */
const pad4 = (b: Uint8Array) => fit(b, (b.length + 3) & ~3);

export const NAME_CHARS = 14;
const NAME_BYTES = 16;

function chunk(tag: string, body: Uint8Array): Uint8Array {
  const out = new Uint8Array(8 + body.length);
  out.set(encodeAscii(tag));
  new DataView(out.buffer).setUint32(4, body.length, true);
  out.set(body, 8);
  return out;
}

const u32 = (v: number) => {
  const b = new Uint8Array(4);
  new DataView(b.buffer).setUint32(0, v, true);
  return b;
};

export const PATCH_BUFFER_BYTES = 832;

export function encodePatch(p: Patch): Uint8Array {
  const body = [
    u32(p.version),
    u32(p.effects.length),
    u32(p.target),
    unhex(p.misc),
    fit(encodeAscii(p.shortName), 10),
    ...p.effects.map((e) => u32(e.id)),
    chunk("TXJ1", unhex(p.txj1)),
    chunk("TXE1", pad4(encodeAscii(p.descriptionEn))),
    chunk("EDTB", Buffer.concat(p.effects.map(encodeEffect))),
    chunk("PRM2", encodePrm2(p.prm2)),
    chunk("NAME", fit(encodeAscii(p.name.slice(0, NAME_CHARS).padEnd(NAME_CHARS, " ")), NAME_BYTES)),
  ];
  const size = 8 + body.reduce((n, b) => n + b.length, 0);
  const ptcf = Buffer.concat([encodeAscii("PTCF"), u32(size), ...body]);
  const rest = PATCH_BUFFER_BYTES - ptcf.length;
  if (rest < 0) throw new Error(`PTCF が ${PATCH_BUFFER_BYTES} バイトを超える: ${ptcf.length}`);
  // 残骸はそのまま戻す（ラウンドトリップ用）。PTCF の長さが変わって合わなければ 0 で埋める
  let trailer = unhex(p.trailer);
  if (trailer.length !== rest) trailer = new Uint8Array(rest);
  return Uint8Array.from(Buffer.concat([ptcf, trailer]));
}
