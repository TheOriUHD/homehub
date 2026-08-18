import { useEffect, useRef, useState } from "react";

/**
 * Authentic Minecraft inventory, laid out on the vanilla GUI grid: the
 * survival inventory background is 176×166 GUI pixels, and every slot here
 * sits at its exact vanilla coordinate (armor column at 7,7 — character
 * window 25,7 — offhand 77,62 — crafting 2×2 at 97,17 with arrow and result
 * at 153,27 — main storage 7,83 — hotbar 7,141), scaled up by a constant
 * factor. Real 16×16 item textures render pixelated with stack counts and an
 * enchant glint that is alpha-masked to the item's own sprite so it can never
 * sweep past the item's edges. The crafting field and recipe book are
 * decorative only — this is a viewer, not a client.
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

/** Pixels per vanilla GUI pixel. */
const S = 3;
const CELL = 18 * S;
const ITEM = 16 * S;

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

type GhostKind = "helmet" | "chestplate" | "leggings" | "boots" | "shield";

/** The faint silhouette vanilla shows in an empty armor/offhand slot. */
function GhostImg({ kind }: { kind: GhostKind }) {
  const [ok, setOk] = useState(true);
  if (!ok) return null;
  return (
    <img
      src={`/mc-tex/_slot_${kind}.png`}
      alt=""
      onError={() => setOk(false)}
      style={{ position: "absolute", left: S, top: S, width: ITEM, height: ITEM, imageRendering: "pixelated" }}
    />
  );
}

/** One 18×18 GUI-pixel slot: vanilla bevel, texture, count, masked glint. */
export function Slot({
  item,
  x,
  y,
  held,
  ghost,
}: {
  item?: InvItem;
  x?: number;
  y?: number;
  held?: boolean;
  ghost?: GhostKind;
}) {
  const [broken, setBroken] = useState(false);
  const enchanted = !!item && !!item.enchants && Object.keys(item.enchants).length > 0;
  const label = item
    ? (item.name ? cleanName(item.name) : prettyType(item.type)) +
      (item.enchants ? " — " + Object.entries(item.enchants).map(([e, l]) => `${e.replace(/_/g, " ")} ${l}`).join(", ") : "")
    : undefined;
  const pos =
    x !== undefined && y !== undefined
      ? ({ position: "absolute", left: x * S, top: y * S } as const)
      : ({ position: "relative" } as const);
  return (
    <div
      title={label}
      style={{
        ...pos,
        width: CELL,
        height: CELL,
        background: "#8b8b8b",
        boxShadow: `inset ${S}px ${S}px 0 #373737, inset ${-S}px ${-S}px 0 #ffffff`,
        ...(held ? { outline: `${S}px solid #fcd34d`, outlineOffset: -S, zIndex: 1 } : {}),
      }}
    >
      {!item && ghost && <GhostImg kind={ghost} />}
      {item && !broken && (
        <img
          src={texUrl(item.type)}
          alt={label}
          onError={() => setBroken(true)}
          style={{ position: "absolute", left: S, top: S, width: ITEM, height: ITEM, imageRendering: "pixelated" }}
        />
      )}
      {item && broken && (
        <span
          style={{ fontSize: 3.2 * S }}
          className="absolute inset-0 flex items-center justify-center px-0.5 text-center leading-none text-white/80"
        >
          {prettyType(item.type).slice(0, 14)}
        </span>
      )}
      {enchanted && !broken && item && (
        <span
          style={{
            position: "absolute",
            left: S,
            top: S,
            width: ITEM,
            height: ITEM,
            pointerEvents: "none",
            // The glint only exists where the item sprite has pixels — masked
            // by the texture's own alpha, exactly like the game does it.
            WebkitMaskImage: `url(${texUrl(item.type)})`,
            maskImage: `url(${texUrl(item.type)})`,
            WebkitMaskSize: "100% 100%",
            maskSize: "100% 100%",
            mixBlendMode: "screen",
            backgroundColor: "rgba(160, 90, 255, 0.18)",
            backgroundImage:
              "linear-gradient(125deg, rgba(180,90,255,0) 30%, rgba(200,120,255,0.42) 45%, rgba(255,255,255,0.30) 50%, rgba(200,120,255,0.42) 55%, rgba(180,90,255,0) 70%)",
            backgroundSize: "200% 200%",
            animation: "hh-glint 2.8s linear infinite",
          }}
        />
      )}
      {item && item.count > 1 && (
        <span
          style={{
            position: "absolute",
            right: S,
            bottom: 0,
            fontSize: 7.5 * S,
            fontFamily: "monospace",
            fontWeight: 700,
            color: "#fff",
            textShadow: `${S}px ${S}px 0 #3f3f3f`,
            zIndex: 2,
          }}
          className="tabular-nums"
        >
          {item.count}
        </span>
      )}
    </div>
  );
}

/** The 1-GUI-pixel white outline vanilla engraves on a slot group's top-left. */
function GroupFrame({ x, y, w, h }: { x: number; y: number; w: number; h: number }) {
  return (
    <div
      style={{
        position: "absolute",
        left: (x - 1) * S,
        top: (y - 1) * S,
        width: (w + 1) * S,
        height: (h + 1) * S,
        background: "#ffffff",
      }}
    />
  );
}

/** The full survival inventory on the vanilla 176×166 layout. */
export function MinecraftInventory({ inventory, uuid }: { inventory: Inventory; uuid?: string }) {
  const bySlot = new Map<number, InvItem>();
  for (const it of inventory.items ?? []) if (typeof it.slot === "number") bySlot.set(it.slot, it);

  return (
    <div className="max-w-full overflow-x-auto">
      <div
        style={{
          position: "relative",
          width: 176 * S,
          height: 166 * S,
          background: "#c6c6c6",
          borderRadius: 2 * S,
          boxShadow: `inset ${S}px ${S}px 0 #ffffff, inset ${-S}px ${-S}px 0 #555555`,
        }}
      >
        {/* armor column */}
        <GroupFrame x={7} y={7} w={18} h={72} />
        <Slot x={7} y={7} item={inventory.armor?.helmet} ghost="helmet" />
        <Slot x={7} y={25} item={inventory.armor?.chestplate} ghost="chestplate" />
        <Slot x={7} y={43} item={inventory.armor?.leggings} ghost="leggings" />
        <Slot x={7} y={61} item={inventory.armor?.boots} ghost="boots" />

        {/* character window */}
        <div
          style={{
            position: "absolute",
            left: 25 * S,
            top: 7 * S,
            width: 50 * S,
            height: 70 * S,
            background: "#000",
            boxShadow: `inset ${S}px ${S}px 0 #373737, inset ${-S}px ${-S}px 0 #ffffff`,
          }}
        >
          <div style={{ position: "absolute", left: "50%", top: "50%", transform: "translate(-50%, -50%)" }}>
            <SkinBody uuid={uuid} scale={2 * S} />
          </div>
        </div>

        {/* offhand */}
        <GroupFrame x={77} y={62} w={18} h={18} />
        <Slot x={77} y={62} item={inventory.offHand} ghost="shield" />

        {/* crafting (decorative — a viewer can't craft) */}
        <span
          style={{
            position: "absolute",
            left: 97 * S,
            top: 7 * S,
            fontSize: 7 * S,
            fontFamily: "monospace",
            fontWeight: 700,
            color: "#404040",
            letterSpacing: 0.5,
            lineHeight: 1,
          }}
        >
          Crafting
        </span>
        <GroupFrame x={97} y={17} w={36} h={36} />
        <Slot x={97} y={17} />
        <Slot x={115} y={17} />
        <Slot x={97} y={35} />
        <Slot x={115} y={35} />
        <img
          src="/mc-tex/_craft_arrow.png"
          alt=""
          style={{ position: "absolute", left: 135 * S, top: 29 * S, width: 16 * S, height: 13 * S, imageRendering: "pixelated" }}
        />
        <GroupFrame x={153} y={27} w={18} h={18} />
        <Slot x={153} y={27} />
        <img
          src="/mc-tex/_recipe_book.png"
          alt=""
          style={{ position: "absolute", left: 104 * S, top: 61 * S, width: 20 * S, height: 18 * S, imageRendering: "pixelated" }}
        />

        {/* main storage: slots 9..35 */}
        <GroupFrame x={7} y={83} w={162} h={54} />
        {Array.from({ length: 27 }, (_, i) => (
          <Slot key={"m" + i} x={7 + (i % 9) * 18} y={83 + Math.floor(i / 9) * 18} item={bySlot.get(i + 9)} />
        ))}

        {/* hotbar: slots 0..8 */}
        <GroupFrame x={7} y={141} w={162} h={18} />
        {Array.from({ length: 9 }, (_, i) => (
          <Slot key={"h" + i} x={7 + i * 18} y={141} item={bySlot.get(i)} held={i === inventory.heldSlot} />
        ))}
      </div>
    </div>
  );
}

/** A compact 9-wide chest-style grid (ender chest). */
export function ItemGrid({ items }: { items: InvItem[] }) {
  const bySlot = new Map<number, InvItem>();
  items.forEach((it, i) => bySlot.set(typeof it.slot === "number" ? it.slot : i, it));
  const max = Math.max(0, ...[...bySlot.keys()]);
  const rows = Math.max(3, Math.ceil((max + 1) / 9));
  return (
    <div className="max-w-full overflow-x-auto">
      <div
        style={{
          position: "relative",
          display: "inline-block",
          padding: 7 * S,
          background: "#c6c6c6",
          borderRadius: 2 * S,
          boxShadow: `inset ${S}px ${S}px 0 #ffffff, inset ${-S}px ${-S}px 0 #555555`,
        }}
      >
        <div
          style={{
            display: "grid",
            gridTemplateColumns: `repeat(9, ${CELL}px)`,
            gap: 0,
            boxShadow: `${-S}px ${-S}px 0 #ffffff`,
          }}
        >
          {Array.from({ length: rows * 9 }, (_, i) => (
            <Slot key={i} item={bySlot.get(i)} />
          ))}
        </div>
      </div>
    </div>
  );
}

/**
 * The player's face (8×8 + hat layer), for avatars. Drawn from the proxied
 * skin; falls back to Steve for offline-mode uuids.
 */
export function SkinFace({ uuid, size = 40, className }: { uuid?: string | null; size?: number; className?: string }) {
  const ref = useRef<HTMLCanvasElement>(null);
  useEffect(() => {
    const canvas = ref.current;
    if (!canvas) return;
    const ctx = canvas.getContext("2d");
    if (!ctx) return;
    const img = new Image();
    img.onload = () => {
      ctx.clearRect(0, 0, size, size);
      ctx.imageSmoothingEnabled = false;
      ctx.drawImage(img, 8, 8, 8, 8, 0, 0, size, size);
      ctx.drawImage(img, 40, 8, 8, 8, 0, 0, size, size);
    };
    img.src = uuid ? `/api/mc/skin/${uuid}` : "/mc-tex/_steve.png";
  }, [uuid, size]);
  return <canvas ref={ref} width={size} height={size} className={className} style={{ imageRendering: "pixelated" }} />;
}

/**
 * The player's front-facing 2D body, drawn from their skin. Static pose,
 * nearest-neighbor upscaled; 16×32 skin pixels at `scale`.
 */
function SkinBody({ uuid, scale = 6 }: { uuid?: string; scale?: number }) {
  const canvasRef = useRef<HTMLCanvasElement>(null);
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
