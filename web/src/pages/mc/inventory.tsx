import { useEffect, useRef, useState } from "react";
import { cn } from "@/lib/utils";

/**
 * Authentic Minecraft inventory rendering: real 16×16 item textures
 * (served from /mc-tex, pixelated and scaled), the classic beveled slot
 * look, stack counts, an enchantment glint, custom-name tooltips, and the
 * character panel (armor column, 2D skin body, offhand). Item textures are
 * the resolved 1.21 set; a missing id falls back to a bare slot.
 */

export type InvItem = {
  slot?: number;
  type: string;
  count: number;
  name?: string;
  enchants?: Record<string, number>;
};
export type Inventory = {
  heldSlot?: number;
  items?: InvItem[];
  armor?: { helmet?: InvItem; chestplate?: InvItem; leggings?: InvItem; boots?: InvItem };
  offHand?: InvItem;
};

const SLOT = 44; // px

function texUrl(type: string): string {
  const id = type.replace(/^minecraft:/, "").toLowerCase();
  return `/mc-tex/${id}.png`;
}

// Legacy § color/format codes carry no meaning here — strip for display.
function cleanName(name: string): string {
  return name.replace(/[§&][0-9a-fk-or]/gi, "").trim();
}

function prettyType(type: string): string {
  return type.replace(/^minecraft:/, "").replace(/_/g, " ");
}

/** One inventory slot: beveled MC frame, texture, count, glint, tooltip. */
export function Slot({ item, held, empty }: { item?: InvItem; held?: boolean; empty?: boolean }) {
  const [broken, setBroken] = useState(false);
  const enchanted = item && item.enchants && Object.keys(item.enchants).length > 0;
  const label = item
    ? (item.name ? cleanName(item.name) : prettyType(item.type)) +
      (item.enchants ? " — " + Object.entries(item.enchants).map(([e, l]) => `${e.replace(/_/g, " ")} ${l}`).join(", ") : "")
    : undefined;
  return (
    <div
      className={cn("relative", held && "ring-2 ring-amber-300")}
      style={{
        width: SLOT,
        height: SLOT,
        background: "#8b8b8b",
        boxShadow: "inset 2px 2px 0 #373737, inset -2px -2px 0 #ffffff",
      }}
      title={label}
    >
      {item && !broken && (
        <img
          src={texUrl(item.type)}
          alt={label}
          onError={() => setBroken(true)}
          className="absolute inset-0 m-auto"
          style={{ width: SLOT - 10, height: SLOT - 10, imageRendering: "pixelated" }}
        />
      )}
      {item && broken && (
        <span className="absolute inset-0 flex items-center justify-center px-0.5 text-center text-[8px] leading-none text-white/80">
          {prettyType(item.type).slice(0, 14)}
        </span>
      )}
      {enchanted && !broken && (
        <span
          className="pointer-events-none absolute inset-0"
          style={{
            background: "linear-gradient(135deg, rgba(180,90,255,0) 30%, rgba(200,120,255,0.55) 50%, rgba(180,90,255,0) 70%)",
            mixBlendMode: "screen",
            backgroundSize: "200% 200%",
            animation: "hh-glint 2.4s linear infinite",
          }}
        />
      )}
      {item && item.count > 1 && (
        <span
          className="absolute right-0.5 bottom-0 font-bold tabular-nums"
          style={{ fontSize: 15, color: "#fff", textShadow: "2px 2px 0 #3f3f3f", fontFamily: "monospace" }}
        >
          {item.count}
        </span>
      )}
      {empty && !item && <ArmorGhost />}
    </div>
  );
}

function ArmorGhost() {
  return <span className="absolute inset-0 m-auto opacity-20" />;
}

/** The full inventory: character panel + main storage + hotbar. */
export function MinecraftInventory({ inventory, uuid }: { inventory: Inventory; uuid?: string }) {
  const bySlot = new Map<number, InvItem>();
  for (const it of inventory.items ?? []) if (typeof it.slot === "number") bySlot.set(it.slot, it);
  const main = Array.from({ length: 27 }, (_, i) => bySlot.get(i + 9)); // slots 9..35
  const hotbar = Array.from({ length: 9 }, (_, i) => bySlot.get(i)); // slots 0..8

  return (
    <div
      className="inline-block max-w-full overflow-x-auto"
      style={{ background: "#c6c6c6", padding: 12, boxShadow: "inset 3px 3px 0 #ffffff, inset -3px -3px 0 #555" }}
    >
      {/* character panel */}
      <div className="mb-3 flex gap-3">
        <div className="flex flex-col gap-1">
          <Slot item={inventory.armor?.helmet} empty />
          <Slot item={inventory.armor?.chestplate} empty />
          <Slot item={inventory.armor?.leggings} empty />
          <Slot item={inventory.armor?.boots} empty />
        </div>
        <div
          className="flex items-center justify-center"
          style={{ background: "#8b8b8b", boxShadow: "inset 2px 2px 0 #373737, inset -2px -2px 0 #ffffff", width: SLOT * 2 + 8, padding: 6 }}
        >
          <SkinBody uuid={uuid} scale={6} />
        </div>
        <div className="flex flex-col justify-end">
          <Slot item={inventory.offHand} empty />
        </div>
      </div>

      {/* main storage: 3 × 9 */}
      <div className="grid gap-1" style={{ gridTemplateColumns: `repeat(9, ${SLOT}px)` }}>
        {main.map((it, i) => (
          <Slot key={"m" + i} item={it} />
        ))}
      </div>

      {/* hotbar */}
      <div className="mt-3 grid gap-1" style={{ gridTemplateColumns: `repeat(9, ${SLOT}px)` }}>
        {hotbar.map((it, i) => (
          <Slot key={"h" + i} item={it} held={i === inventory.heldSlot} />
        ))}
      </div>
    </div>
  );
}

/** A compact grid for the ender chest (no character panel). */
export function ItemGrid({ items }: { items: InvItem[] }) {
  const bySlot = new Map<number, InvItem>();
  items.forEach((it, i) => bySlot.set(typeof it.slot === "number" ? it.slot : i, it));
  const max = Math.max(0, ...[...bySlot.keys()]);
  const cells = Array.from({ length: Math.max(27, max + 1) }, (_, i) => bySlot.get(i));
  return (
    <div
      className="inline-block max-w-full overflow-x-auto"
      style={{ background: "#c6c6c6", padding: 12, boxShadow: "inset 3px 3px 0 #ffffff, inset -3px -3px 0 #555" }}
    >
      <div className="grid gap-1" style={{ gridTemplateColumns: `repeat(9, ${SLOT}px)` }}>
        {cells.map((it, i) => (
          <Slot key={i} item={it} />
        ))}
      </div>
    </div>
  );
}

/**
 * The player's front-facing 2D body, drawn from their skin (proxied by the
 * backend so offline-mode uuids fall back to Steve). Static pose, real
 * pixels — nearest-neighbor upscaled.
 */
function SkinBody({ uuid, scale = 6 }: { uuid?: string; scale?: number }) {
  const canvasRef = useRef<HTMLCanvasElement>(null);
  // 16 wide × 32 tall in skin pixels.
  const W = 16 * scale;
  const H = 32 * scale;

  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;
    const ctx = canvas.getContext("2d");
    if (!ctx) return;
    const img = new Image();
    img.onload = () => {
      ctx.clearRect(0, 0, W, H);
      ctx.imageSmoothingEnabled = false;
      // src (sx,sy,sw,sh) → dest (dx,dy) in skin-pixel units × scale
      const part = (sx: number, sy: number, sw: number, sh: number, dx: number, dy: number) =>
        ctx.drawImage(img, sx, sy, sw, sh, dx * scale, dy * scale, sw * scale, sh * scale);
      // base layers
      part(8, 8, 8, 8, 4, 0); // head
      part(20, 20, 8, 12, 4, 8); // body
      part(44, 20, 4, 12, 0, 8); // right arm (viewer left)
      part(36, 52, 4, 12, 12, 8); // left arm
      part(4, 20, 4, 12, 4, 20); // right leg
      part(20, 52, 4, 12, 8, 20); // left leg
      // overlays (hat/jacket/sleeves/pants)
      part(40, 8, 8, 8, 4, 0); // hat
      part(20, 36, 8, 12, 4, 8); // jacket
      part(44, 36, 4, 12, 0, 8); // right sleeve
      part(52, 52, 4, 12, 12, 8); // left sleeve
      part(4, 36, 4, 12, 4, 20); // right pant
      part(4, 52, 4, 12, 8, 20); // left pant
    };
    img.onerror = () => {
      ctx.clearRect(0, 0, W, H);
    };
    img.src = uuid ? `/api/mc/skin/${uuid}` : "/mc-tex/_steve.png";
  }, [uuid, scale, W, H]);

  return <canvas ref={canvasRef} width={W} height={H} style={{ imageRendering: "pixelated" }} />;
}
