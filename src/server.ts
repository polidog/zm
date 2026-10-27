/**
 * L4: MCP サーバー (stdio)。
 *
 * 番号はペダルの画面に合わせる: パッチは 1〜300、スロットは 1〜6。
 * 編集はすべて「いま鳴っているパッチ」への変更で、保存はしない（パッチを選び直せば元に戻る）。
 * 曖昧な指示（「80 年代っぽいクリーンに」）の解釈は Claude 側に任せ、ここは素直な操作だけを出す。
 *
 *   bun run src/server.ts
 */

import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import { mkdirSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { z } from "zod";
import { type Catalog, type CatalogEffect, describePatch, findEffect, formatId, loadCatalog } from "./catalog";
import { PATCH_COUNT, Zoom } from "./device";

/** ユーザーが使えるスロット数（7, 8 番目は Output VP とシステム用）。 */
const USER_SLOTS = 6;
const BACKUP_DIR = join(import.meta.dir, "..", "backups");

let catalog: Catalog | undefined;
const getCatalog = () => (catalog ??= loadCatalog());

// ペダルとのやりとりは 1 本の列に並べる。呼び出しごとに開いて閉じるので、抜き差ししても動く
let queue: Promise<unknown> = Promise.resolve();
function withZoom<T>(edit: boolean, fn: (zoom: Zoom) => Promise<T>): Promise<T> {
  const run = queue.then(async () => {
    await using zoom = await Zoom.open({ edit });
    // await しないと fn の途中でブロックを抜けてポートが閉じる
    return await fn(zoom);
  });
  queue = run.catch(() => {});
  return run;
}

const json = (value: unknown) => ({ content: [{ type: "text" as const, text: JSON.stringify(value, null, 2) }] });

function resolveEffect(query: string): CatalogEffect {
  const c = getCatalog();
  const q = query.trim().toLowerCase();
  const hit =
    c.effects.find((e) => e.id === q || e.name.toLowerCase() === q) ??
    c.effects.find((e) => e.name.toLowerCase().replace(/[\s-]/g, "") === q.replace(/[\s-]/g, ""));
  if (hit) return hit;
  const near = c.effects.filter((e) => e.name.toLowerCase().includes(q)).map((e) => e.name);
  throw new Error(`エフェクト "${query}" がカタログにない${near.length ? `。候補: ${near.join(", ")}` : "。list_effects で探す"}`);
}

const slotSchema = z.number().int().min(1).max(USER_SLOTS).describe(`スロット番号 1〜${USER_SLOTS}（信号の流れの順）`);

const server = new McpServer({ name: "zoom-g2four", version: "0.0.1" });

// ---- 読み出し ----

server.registerTool(
  "identify",
  {
    description: "接続中の ZOOM ペダルの機種 ID・ファームウェア版・パッチ数を返す。",
    annotations: { readOnlyHint: true },
  },
  async () =>
    json(
      await withZoom(false, async (zoom) => ({
        device: "ZOOM G2 FOUR",
        manufacturer: "0x" + zoom.identity.manufacturer.toString(16),
        model: "0x" + zoom.identity.model.toString(16),
        firmware: zoom.identity.version,
        patches: PATCH_COUNT,
        userSlots: USER_SLOTS,
      })),
    ),
);

server.registerTool(
  "list_patches",
  {
    description: "全パッチの番号（ペダルの画面と同じ 1〜300）と名前の一覧。5 秒ほどかかる。",
    annotations: { readOnlyHint: true },
  },
  async () =>
    json(await withZoom(false, async (zoom) => (await zoom.listPatches()).map(({ n, name }) => ({ number: n + 1, name })))),
);

server.registerTool(
  "get_patch",
  {
    description:
      "パッチの中身（スロットごとのエフェクト・ON/OFF・パラメータ値と最大値）を返す。number を省くと、いま鳴っている（編集中の）パッチ。",
    inputSchema: {
      number: z.number().int().min(1).max(PATCH_COUNT).optional().describe("パッチ番号 1〜300（ペダルの画面と同じ）"),
    },
    annotations: { readOnlyHint: true },
  },
  async ({ number }) =>
    json(
      await withZoom(false, async (zoom) => {
        const patch = number === undefined ? await zoom.getCurrentPatch() : await zoom.getPatch(number - 1);
        return { number: number ?? "current", ...describePatch(getCatalog(), patch) };
      }),
    ),
);

server.registerTool(
  "backup_all",
  {
    description: "全 300 パッチをこのマシンの backups/ に .syx で退避する。ペダルには書き込まない。",
    annotations: { readOnlyHint: true },
  },
  async () =>
    json(
      await withZoom(false, async (zoom) => {
        const d = new Date();
        const p2 = (n: number) => String(n).padStart(2, "0");
        const dir = join(
          BACKUP_DIR,
          `all-${d.getFullYear()}${p2(d.getMonth() + 1)}${p2(d.getDate())}-${p2(d.getHours())}${p2(d.getMinutes())}${p2(d.getSeconds())}`,
        );
        mkdirSync(dir, { recursive: true });
        for (let n = 0; n < PATCH_COUNT; n++) {
          writeFileSync(join(dir, `${String(n).padStart(3, "0")}.syx`), await zoom.getPatchSysex(n));
        }
        return { saved: PATCH_COUNT, dir };
      }),
    ),
);

// ---- カタログ ----

server.registerTool(
  "list_effects",
  {
    description:
      "ペダルに入っているエフェクトの一覧（名前・グループ・パラメータと範囲）。音作りの候補を選ぶのに使う。詳しい説明は describe_effect。",
    inputSchema: {
      group: z
        .string()
        .optional()
        .describe("DYNAMICS / FILTER / DRIVE / AMP / MODULATION / SFX / DELAY / REVERB / PEDAL で絞る"),
    },
    annotations: { readOnlyHint: true },
  },
  async ({ group }) =>
    json(
      getCatalog()
        .effects.filter((e) => !group || e.groupName.toLowerCase() === group.toLowerCase())
        .map((e) => ({
          name: e.name,
          group: e.groupName,
          params: e.params.filter((p) => !p.hidden).map((p) => `${p.name} 0-${p.max}`),
        })),
    ),
);

server.registerTool(
  "describe_effect",
  {
    description: "エフェクト 1 つの説明（モデルになった機材など）と、各パラメータの意味・範囲・既定値。",
    inputSchema: { effect: z.string().describe("エフェクト名（例: TS Drive）または ID（例: 0x03000030）") },
    annotations: { readOnlyHint: true },
  },
  async ({ effect }) => {
    const e = resolveEffect(effect);
    return json({
      name: e.name,
      id: e.id,
      group: e.groupName,
      description: e.descriptionJa || e.description,
      params: e.params
        .filter((p) => !p.hidden)
        .map((p) => ({ name: p.name, max: p.max, default: p.default, explanation: p.explanationJa || p.explanation })),
    });
  },
);

// ---- 編集（いま鳴っているパッチ。保存しない） ----

const slotView = async (zoom: Zoom, slot: number) => {
  const d = describePatch(getCatalog(), await zoom.getCurrentPatch());
  return d.slots.find((s) => s.slot === slot - 1) ?? { slot, effect: "(empty)" };
};

server.registerTool(
  "set_effect",
  {
    description:
      "いま鳴っているパッチのスロットのエフェクトを差し替える（保存しない）。パラメータは既定値か、同じ電源投入中に前回そのエフェクトで使った値になるので、結果を見て set_param で整える。",
    inputSchema: {
      slot: slotSchema,
      effect: z.string().describe("エフェクト名（例: TS Drive）または ID"),
    },
  },
  async ({ slot, effect }) => {
    const e = resolveEffect(effect);
    return json(
      await withZoom(true, async (zoom) => {
        await zoom.setEffect(slot - 1, Number(e.id));
        return { ...(await slotView(zoom, slot)), slot };
      }),
    );
  },
);

server.registerTool(
  "set_param",
  {
    description: "いま鳴っているパッチのスロットのパラメータを名前で変える（保存しない）。値はカタログの範囲で検証する。",
    inputSchema: {
      slot: slotSchema,
      param: z.string().describe("パラメータ名（get_patch / describe_effect に出るもの。大文字小文字は問わない）"),
      value: z.number().int().min(0),
    },
  },
  async ({ slot, param, value }) =>
    json(
      await withZoom(true, async (zoom) => {
        const fx = (await zoom.getCurrentPatch()).effects[slot - 1]!;
        const e = findEffect(getCatalog(), fx.id);
        if (!e) throw new Error(`スロット ${slot} は空か、カタログにないエフェクト (${formatId(fx.id)})`);
        const index = e.params.findIndex((p) => !p.hidden && p.name.toLowerCase() === param.toLowerCase());
        if (index < 0) {
          const names = e.params.filter((p) => !p.hidden).map((p) => p.name);
          throw new Error(`${e.name} にパラメータ "${param}" はない。あるのは: ${names.join(", ")}`);
        }
        const p = e.params[index]!;
        if (value > p.max) throw new Error(`${e.name} の ${p.name} は 0〜${p.max}`);
        await zoom.setParam(slot - 1, index, value);
        return { ...(await slotView(zoom, slot)), slot };
      }),
    ),
);

server.registerTool(
  "toggle_effect",
  {
    description: "いま鳴っているパッチのスロットの ON / OFF を切り替える（保存しない）。enabled を省くと反転。",
    inputSchema: { slot: slotSchema, enabled: z.boolean().optional() },
  },
  async ({ slot, enabled }) =>
    json(
      await withZoom(true, async (zoom) => {
        const fx = (await zoom.getCurrentPatch()).effects[slot - 1]!;
        if (fx.id === 0) throw new Error(`スロット ${slot} は空`);
        await zoom.setEnabled(slot - 1, enabled ?? !fx.enabled);
        return { ...(await slotView(zoom, slot)), slot };
      }),
    ),
);

// ---- リソース ----

server.registerResource(
  "catalog",
  "zoom://catalog",
  { description: "エフェクトカタログ全体（説明・パラメータの意味・範囲・既定値）", mimeType: "application/json" },
  async (uri) => ({ contents: [{ uri: uri.href, mimeType: "application/json", text: JSON.stringify(getCatalog()) }] }),
);

server.registerResource(
  "current_patch",
  "zoom://patch/current",
  { description: "いま鳴っている（編集中の）パッチ", mimeType: "application/json" },
  async (uri) => {
    const d = await withZoom(false, async (zoom) => describePatch(getCatalog(), await zoom.getCurrentPatch()));
    return { contents: [{ uri: uri.href, mimeType: "application/json", text: JSON.stringify(d, null, 2) }] };
  },
);

await server.connect(new StdioServerTransport());
