/**
 * L3: エフェクトカタログ。ID → 名前 / パラメータ名 / 範囲 / 説明。
 *
 * catalog/g2four.json は `bun run catalog` でペダルの ZD2 から作る（ZOOM の著作物を含むので git には入れない）。
 */

import { existsSync, readFileSync } from "node:fs";
import { join } from "node:path";
import type { Patch } from "./patch";
import type { Zd2 } from "./zd2";

export type CatalogEffect = Omit<Zd2, "id"> & { id: string; file: string };

export type Catalog = {
  device: string;
  generatedAt: string;
  effects: CatalogEffect[];
};

export const CATALOG_PATH = join(import.meta.dir, "..", "catalog", "g2four.json");

/** ZD2 を持たない、パッチの中だけに現れる ID。 */
export const SYSTEM_EFFECTS: Record<string, string> = {
  "0x00000000": "(empty)",
  "0x0b000010": "Output VP",
  "0x1c000010": "(system)",
};

export const formatId = (id: number) => "0x" + id.toString(16).padStart(8, "0");

export function loadCatalog(path = CATALOG_PATH): Catalog {
  if (!existsSync(path)) throw new Error(`${path} がない。bun run catalog で作る`);
  return JSON.parse(readFileSync(path, "utf8"));
}

export function findEffect(catalog: Catalog, id: number): CatalogEffect | undefined {
  const key = formatId(id);
  return catalog.effects.find((e) => e.id === key);
}

export type NamedSlot = {
  slot: number;
  id: string;
  effect: string;
  group?: string;
  enabled: boolean;
  params: { name: string; value: number; max: number; hidden?: true }[];
};

/** パッチのエフェクトをカタログの名前で読めるようにする。空きスロットは除く。 */
export function describePatch(catalog: Catalog, patch: Patch) {
  const slots: NamedSlot[] = [];
  patch.effects.forEach((fx, slot) => {
    if (fx.id === 0) return;
    const id = formatId(fx.id);
    const e = findEffect(catalog, fx.id);
    slots.push({
      slot,
      id,
      effect: e?.name ?? SYSTEM_EFFECTS[id] ?? "(unknown)",
      group: e?.groupName,
      enabled: fx.enabled,
      params: (e?.params ?? []).map((p, i) => ({
        name: p.name,
        value: fx.params[i] ?? 0,
        max: p.max,
        ...(p.hidden ? { hidden: true as const } : {}),
      })),
    });
  });
  return {
    name: patch.name,
    description: patch.descriptionJa || patch.descriptionEn,
    volume: patch.prm2.patchVolume,
    tempo: patch.prm2.tempo,
    slots,
  };
}
