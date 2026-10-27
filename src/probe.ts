/**
 * P0 疎通確認: Identity Request → 現在プログラム → 現在パッチのダンプ。
 *
 * ペダルに書き込むメッセージは送らない（エディタモードの ON/OFF を除く）。
 * 受け取った生バイトは backups/ に保存し、P1 のコーデック作りの材料にする。
 *
 *   bun run probe [--port hw:C,D] [--editor-mode] [--out backups]
 */

import { mkdirSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { parseArgs } from "node:util";
import { timestamp } from "./backups";
import * as sx from "./sysex";
import { findZoomPort, listPorts, portPath, RawMidi, type MidiPort } from "./transport";

const hexdump = (data: Uint8Array, width = 16) => {
  const lines = [];
  for (let i = 0; i < data.length; i += width) lines.push("    " + sx.toHex(data.slice(i, i + width)));
  return lines.join("\n");
};

const parsePort = (spec: string): MidiPort => {
  const [card, device = "0"] = spec.replace(/^hw:/, "").split(",");
  return { card: Number(card), device: Number(device), name: spec };
};

async function main(): Promise<number> {
  const { values: args } = parseArgs({
    options: {
      port: { type: "string" },
      "editor-mode": { type: "boolean", default: false },
      out: { type: "string", default: "backups" },
    },
  });

  console.log("rawmidi ports:");
  for (const p of listPorts()) console.log(`  hw:${p.card},${p.device}  ${p.name}`);

  const port = args.port ? parsePort(args.port) : findZoomPort();
  if (!port) {
    console.log("ZOOM のポートが見つからない。USB を繋いで電源を入れてから再実行する。");
    return 1;
  }
  console.log(`\nusing ${portPath(port)} (${port.name})`);

  const stamp = timestamp();
  mkdirSync(args.out, { recursive: true });
  const save = (label: string, data: Uint8Array) => {
    const path = join(args.out, `${stamp}-${label}.syx`);
    writeFileSync(path, data);
    console.log(`  saved ${path} (${data.length} bytes)`);
  };

  using midi = new RawMidi(port);

  console.log("\n[1] Identity Request");
  let reply = await midi.request(sx.IDENTITY_REQUEST);
  if (!reply) {
    console.log("  応答なし。ポート違いか、Identity Request に答えない機種。");
    return 2;
  }
  console.log(hexdump(reply));
  save("identity", reply);
  const ident = sx.parseIdentityReply(reply);
  const h = (n: number, w = 2) => "0x" + n.toString(16).toUpperCase().padStart(w, "0");
  console.log(
    `  manufacturer=${h(ident.manufacturer)} family=${h(ident.family, 4)} model=${h(ident.model, 4)} version=${JSON.stringify(ident.version)}`,
  );
  if (ident.manufacturer !== sx.ZOOM_MANUFACTURER) {
    console.log("  ZOOM (0x52) ではない。");
    return 2;
  }
  const dev = ident.deviceId;
  const known = dev === sx.DEFAULT_DEVICE_ID ? "（MS+ / G1 FOUR と同じ）" : "（既知の 6E と違う）";
  console.log(`  device id = ${h(dev)} ${known}`);

  if (args["editor-mode"]) {
    // 50 には 00 00 の ACK が返る。待たないと次の応答と取り違える
    const ack = await midi.request(sx.editorModeOn(dev));
    console.log(`\n[*] editor mode on: ${ack ? sx.toHex(ack) : "応答なし"}`);
  }
  try {
    console.log("\n[2] 現在のバンク / プログラム (33)");
    reply = await midi.request(sx.getCurrentProgram(dev));
    if (!reply) console.log("  応答なし");
    else {
      console.log(hexdump(reply));
      save("program", reply);
    }

    console.log("\n[3] 現在パッチのダンプ (64 13)");
    reply = await midi.request(sx.requestCurrentPatch(dev), 4000);
    if (!reply) console.log("  応答なし。--editor-mode を付けて再実行してみる。");
    else {
      console.log(hexdump(reply.slice(0, 64)) + (reply.length > 64 ? "\n    ..." : ""));
      const tag = reply[4] === 0x64 && reply[5] === 0x12 ? "ok (64 12)" : "想定外のヘッダ";
      console.log(`  ${reply.length} bytes, ${tag}`);
      save("current-patch", reply);
    }
  } finally {
    if (args["editor-mode"]) await midi.send(sx.editorModeOff(dev));
  }
  return 0;
}

process.exit(await main());
