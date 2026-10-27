/**
 * ZD2 (エフェクトファイル) からカタログに要る情報を抜き出す。
 *
 *   ZDLF | headerLen u32 | checksum u32 | target u32 | ?[73] | version[4] | 00 00 | group u8 | id u32 | name[11] | groupName CString
 *   ... ICON / TXJ1 / TXE1 / INFO / DATA (TI C6x の ELF) / PRMJ / PRME ...
 *
 * PRME / PRMJ はパラメータ名と説明の JSON（PRMJ は UTF-8、TXJ1 は Shift-JIS）。値の範囲は入っていないので、
 * DATA の ELF にあるパラメータ表 (1 項目 56 バイト: name[12] | max u32 | default u32 | ...) から読む。
 * 表は OnOff, エフェクト名, param1, param2, ... の順に並ぶ。PRME より後ろの項目 (アンプ系の Dummy など) は
 * 画面に出ない隠しパラメータで、パッチにも既定値が入っている。
 */

export type Zd2Param = {
  name: string;
  max: number;
  default: number;
  explanation: string;
  explanationJa: string;
  /** PRME に載っていない（ペダルの画面に出ない）パラメータ。 */
  hidden: boolean;
};

export type Zd2 = {
  id: number;
  name: string;
  group: number;
  groupName: string;
  version: string;
  description: string;
  descriptionJa: string;
  params: Zd2Param[];
};

const latin1 = new TextDecoder("latin1");
const sjis = new TextDecoder("shift_jis");
const utf8 = new TextDecoder();

const cstring = (b: Uint8Array) => {
  const end = b.indexOf(0);
  return latin1.decode(end < 0 ? b : b.subarray(0, end));
};

function chunks(data: Uint8Array, from: number): Map<string, Uint8Array> {
  const dv = new DataView(data.buffer, data.byteOffset, data.byteLength);
  const out = new Map<string, Uint8Array>();
  let off = from;
  while (off + 8 <= data.length) {
    const tag = latin1.decode(data.subarray(off, off + 4));
    if (!/^[A-Z0-9]{4}$/.test(tag)) break;
    const len = dv.getUint32(off + 4, true);
    out.set(tag, data.subarray(off + 8, off + 8 + len));
    off += 8 + len;
  }
  return out;
}

type PrmJson = { Parameters: { name: string; explanation: string }[] };

function parsePrm(b: Uint8Array | undefined, decoder: TextDecoder): PrmJson["Parameters"] {
  if (!b) return [];
  // 末尾に NUL やゴミが付くので最後の } までで切る
  const text = decoder.decode(b);
  return (JSON.parse(text.slice(0, text.lastIndexOf("}") + 1)) as PrmJson).Parameters;
}

const ENTRY_BYTES = 56;

/** パッチに入るパラメータは最大 12 個。 */
const MAX_PARAMS = 12;

/** ELF からパラメータ表を探す。OnOff の項目を起点に 56 バイト刻みで、名前が読める間だけ読む。 */
function paramTable(elf: Uint8Array): { name: string; max: number; default: number }[] {
  const dv = new DataView(elf.buffer, elf.byteOffset, elf.byteLength);
  const needle = Uint8Array.from("OnOff\0\0\0\0\0\0\0", (c) => c.charCodeAt(0));
  for (let at = 0; at + needle.length <= elf.length; at++) {
    if (!needle.every((b, i) => elf[at + i] === b) || dv.getUint32(at + 12, true) !== 1) continue;
    const entries = [];
    for (let i = 2; i < 2 + MAX_PARAMS; i++) {
      const off = at + i * ENTRY_BYTES;
      const name = cstring(elf.subarray(off, off + 12));
      if (!/^[ -~]+$/.test(name)) break;
      entries.push({ name, max: dv.getUint32(off + 12, true), default: dv.getUint32(off + 16, true) });
    }
    return entries;
  }
  throw new Error("パラメータ表が見つからない");
}

export function parseZd2(data: Uint8Array): Zd2 {
  if (latin1.decode(data.subarray(0, 4)) !== "ZDLF") throw new Error("ZDLF ではない");
  const dv = new DataView(data.buffer, data.byteOffset, data.byteLength);
  const version = latin1.decode(data.subarray(89, 93));
  const group = data[95]!;
  const id = dv.getUint32(96, true);
  const name = cstring(data.subarray(100, 111));
  const groupName = cstring(data.subarray(111, 123));

  const iconAt = latin1.decode(data).indexOf("ICON");
  const c = chunks(data, iconAt);
  const prme = parsePrm(c.get("PRME"), latin1);
  const prmj = parsePrm(c.get("PRMJ"), utf8);
  const table = c.get("DATA") && prme.length ? paramTable(c.get("DATA")!) : [];
  if (table.length < prme.length) throw new Error(`パラメータ表が PRME より短い: ${table.length} < ${prme.length}`);

  return {
    id,
    name,
    group,
    groupName,
    version,
    description: cstring(c.get("TXE1") ?? new Uint8Array()).trim(),
    descriptionJa: sjis.decode(c.get("TXJ1") ?? new Uint8Array()).replace(/\0.*$/s, "").trim(),
    params: table.map((t, i) => ({
      name: prme[i]?.name ?? t.name,
      max: t.max,
      default: t.default,
      explanation: prme[i]?.explanation ?? "",
      explanationJa: prmj[i]?.explanation ?? "",
      hidden: i >= prme.length,
    })),
  };
}
