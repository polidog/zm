// ms50g.json を g200kg/zoom-ms-utility（MIT）の js/effectslist.js から作る。
//
//   node internal/catalog/ms50g/gen.mjs path/to/effectslist.js > internal/catalog/ms50g/ms50g.json
//
// いまの ms50g.json は 5bf930c8e1622b0c70c0c3d761424f84955a227a (2022-12-20) の effectslist.js から作った。
//
// MS-50G で使えるのは ver の下位 4bit が 1 以上のもの（数字は入ったファームウェアの主番号）。
// G-AMP の CAB は index.html の gampcab（ver 3.00）で置き換える。値は飛び飛びなので説明に並べる。
import { readFileSync } from "node:fs";
import vm from "node:vm";

const src = readFileSync(process.argv[2], "utf8");
const ctx = { module: {} };
vm.runInNewContext(src, ctx);
const list = ctx.module.exports;

const gampcab = {
  max: [8, 16, 32, 48, 64, 80, 96, 112, 128, 144, 160, 176, 192, 208, 224, 240, 256, 272, 288, 304, 320, 336, 0],
  disp: ["FD COMBO 2x12", "DLX-R 1x12", "FD VIBRO 2x10", "US BLUES 4x10", "VX COMBO 2x12", "VX JMI 2x12", "BG CRUNCH 1x12", "MATCH 30 2x12", "CAR DRIVE 1x12", "TW ROCK 1x12", "TONE CITY 4x12", "HW STACK 4x12", "TANGERINE 4x12", "B-BRKR 2x12", "MS CRUNCH 4x12", "MS 1959 4x12", "MS DRIVE 4x12", "BGN DRIVE 4x12", "BG DRIVE 4x12", "DZ DRIVE 4x12", "ALIEN 4x12", "REVO-1 4x12", "OFF"],
};

const groups = [];
const effects = [];
for (const [key, e] of Object.entries(list)) {
  const v50 = e.ver & 0xf;
  if (v50 === 0 || e.group === "THRU") continue;
  const params = e.param.map((p, i) => {
    let max = p.max;
    let disp = p.disp;
    if (e.group === "AMP" && i === 7) {
      max = Math.max(...gampcab.max);
      disp = gampcab.max.map((v, j) => `${v}=${gampcab.disp[j]}`).join(", ");
    } else if (Array.isArray(disp)) {
      disp = disp.map((d, j) => `${j}=${d}`).join(", ");
    } else if (typeof disp === "number") {
      disp = `画面の表示は 値${disp < 0 ? "" : "+"}${disp}`;
    } else {
      disp = "";
    }
    return { name: p.name, max, default: p.def, explanation: disp, explanationJa: "", hidden: false };
  });
  effects.push({
    id: "0x" + Number(key).toString(16).padStart(8, "0"),
    name: e.name,
    group: 0,
    groupName: e.group,
    version: "0x" + e.ver.toString(16).padStart(4, "0"),
    description: e.title ?? "",
    descriptionJa: "",
    params,
    file: "",
    order: e.order,
  });
}
effects.sort((a, b) => a.order - b.order);
for (const e of effects) {
  if (!groups.includes(e.groupName)) groups.push(e.groupName);
}
for (const e of effects) {
  e.group = groups.indexOf(e.groupName) + 1;
  delete e.order;
}
const out = {
  device: "ZOOM MS-50G",
  generatedAt: "g200kg/zoom-ms-utility@5bf930c js/effectslist.js",
  effects,
};
process.stdout.write(JSON.stringify(out, null, 2) + "\n");
