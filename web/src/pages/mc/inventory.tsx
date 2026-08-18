import { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";

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

function prettyType(type: string): string {
  return type
    .replace(/^minecraft:/, "")
    .replace(/_/g, " ")
    .replace(/\b\w/g, (c) => c.toUpperCase());
}

// ---- the game's own bitmap font (textures/font/ascii.png) -------------------

let fontImg: HTMLImageElement | null = null;
let fontWidths: number[] | null = null;
let fontLoad: Promise<void> | null = null;

function loadMcFont(): Promise<void> {
  if (!fontLoad) {
    fontLoad = new Promise((resolve) => {
      const img = new Image();
      img.onload = () => {
        const c = document.createElement("canvas");
        c.width = 128;
        c.height = 128;
        const cctx = c.getContext("2d");
        if (!cctx) return resolve();
        cctx.drawImage(img, 0, 0);
        const data = cctx.getImageData(0, 0, 128, 128).data;
        const widths = new Array(128).fill(0);
        for (let code = 32; code < 127; code++) {
          const gx = (code % 16) * 8;
          const gy = (code >> 4) * 8;
          let w = 0;
          for (let x = 0; x < 8; x++) {
            for (let y = 0; y < 8; y++) {
              if (data[((gy + y) * 128 + gx + x) * 4 + 3] > 16) {
                if (x + 1 > w) w = x + 1;
                break;
              }
            }
          }
          widths[code] = w;
        }
        fontImg = img;
        fontWidths = widths;
        resolve();
      };
      img.onerror = () => resolve();
      img.src = "/mc-tex/_font_ascii.png";
    });
  }
  return fontLoad;
}

function shadowColor(hex: string): string {
  const n = parseInt(hex.replace("#", ""), 16);
  const f = (v: number) => Math.floor(v * 0.25);
  return `rgb(${f((n >> 16) & 255)},${f((n >> 8) & 255)},${f(n & 255)})`;
}

/**
 * Text drawn with the actual Minecraft glyphs, pixel-scaled. Non-ASCII input
 * falls back to '?' — the ascii atlas is all vanilla uses for these strings.
 */
function McText({
  text,
  color = "#FFFFFF",
  scale = S,
  shadow = true,
  bold = false,
  className,
  style,
}: {
  text: string;
  color?: string;
  scale?: number;
  shadow?: boolean;
  bold?: boolean;
  className?: string;
  style?: React.CSSProperties;
}) {
  const ref = useRef<HTMLCanvasElement>(null);
  const [ready, setReady] = useState(fontImg !== null);
  useEffect(() => {
    if (!ready) void loadMcFont().then(() => setReady(true));
  }, [ready]);
  useEffect(() => {
    const canvas = ref.current;
    if (!canvas || !fontImg || !fontWidths) return;
    const codes = [...text].map((ch) => {
      const c = ch.charCodeAt(0);
      return c >= 32 && c < 127 ? c : 63;
    });
    const adv = codes.map((c) => (c === 32 ? 4 : fontWidths![c] + 1) + (bold ? 1 : 0));
    const total = adv.reduce((a, b) => a + b, 0) + 1;
    canvas.width = total * scale;
    canvas.height = 9 * scale;
    const ctx = canvas.getContext("2d");
    if (!ctx) return;
    ctx.imageSmoothingEnabled = false;
    const layer = (ox: number, oy: number, col: string) => {
      const tmp = document.createElement("canvas");
      tmp.width = canvas.width;
      tmp.height = canvas.height;
      const tctx = tmp.getContext("2d");
      if (!tctx) return;
      tctx.imageSmoothingEnabled = false;
      let x = ox;
      for (let i = 0; i < codes.length; i++) {
        const c = codes[i];
        if (c !== 32) {
          tctx.drawImage(fontImg!, (c % 16) * 8, (c >> 4) * 8, 8, 8, x * scale, oy * scale, 8 * scale, 8 * scale);
          if (bold) tctx.drawImage(fontImg!, (c % 16) * 8, (c >> 4) * 8, 8, 8, (x + 1) * scale, oy * scale, 8 * scale, 8 * scale);
        }
        x += adv[i];
      }
      tctx.globalCompositeOperation = "source-in";
      tctx.fillStyle = col;
      tctx.fillRect(0, 0, tmp.width, tmp.height);
      ctx.drawImage(tmp, 0, 0);
    };
    ctx.clearRect(0, 0, canvas.width, canvas.height);
    if (shadow) layer(1, 1, shadowColor(color));
    layer(0, 0, color);
  }, [text, color, scale, shadow, bold, ready]);
  return <canvas ref={ref} className={className} style={{ imageRendering: "pixelated", display: "block", ...style }} />;
}

// ---- legacy §-code rendering for custom item names --------------------------

const MC_COLORS: Record<string, string> = {
  "0": "#000000", "1": "#0000AA", "2": "#00AA00", "3": "#00AAAA",
  "4": "#AA0000", "5": "#AA00AA", "6": "#FFAA00", "7": "#AAAAAA",
  "8": "#555555", "9": "#5555FF", a: "#55FF55", b: "#55FFFF",
  c: "#FF5555", d: "#FF55FF", e: "#FFFF55", f: "#FFFFFF",
};

type NameFrag = { text: string; color?: string; bold?: boolean; italic?: boolean; underline?: boolean; strike?: boolean };

/** Parse a legacy §-formatted string into styled fragments (& accepted too). */
function parseLegacy(input: string): NameFrag[] {
  const frags: NameFrag[] = [];
  let cur: NameFrag = { text: "" };
  for (let i = 0; i < input.length; i++) {
    const ch = input[i];
    if ((ch === "§" || ch === "&") && i + 1 < input.length) {
      const code = input[i + 1].toLowerCase();
      if (MC_COLORS[code] || "klmnor".includes(code)) {
        if (cur.text) frags.push(cur);
        if (MC_COLORS[code]) cur = { text: "", color: MC_COLORS[code] };
        else if (code === "l") cur = { ...cur, text: "", bold: true };
        else if (code === "o") cur = { ...cur, text: "", italic: true };
        else if (code === "n") cur = { ...cur, text: "", underline: true };
        else if (code === "m") cur = { ...cur, text: "", strike: true };
        else if (code === "r") cur = { text: "" };
        i++;
        continue;
      }
    }
    cur.text += ch;
  }
  if (cur.text) frags.push(cur);
  return frags.length > 0 ? frags : [{ text: input }];
}

const ROMAN = ["I", "II", "III", "IV", "V", "VI", "VII", "VIII", "IX", "X"];

/** Enchants whose max level is 1 — vanilla shows them without a numeral. */
const SINGLE_LEVEL = new Set([
  "mending", "silk_touch", "flame", "infinity", "channeling", "multishot",
  "aqua_affinity", "binding_curse", "vanishing_curse",
]);

function enchantLabel(id: string, level: number): string {
  const name = prettyType(id);
  if (level === 1 && SINGLE_LEVEL.has(id.replace(/^minecraft:/, ""))) return name;
  return `${name} ${ROMAN[level - 1] ?? level}`;
}

/** The vanilla purple-bordered item tooltip, following the cursor. Rendered
 *  through a portal so no slot stacking context or overflow can clip it. */
function ItemTooltip({ item, at }: { item: InvItem; at: { x: number; y: number } }) {
  const left = Math.min(at.x + 14, (typeof window !== "undefined" ? window.innerWidth : 1200) - 250);
  const top = Math.max(8, at.y - 30);
  return createPortal(
    <div
      style={{
        position: "fixed",
        left,
        top,
        zIndex: 60,
        pointerEvents: "none",
        background: "rgba(16,0,16,0.95)",
        border: "1px solid rgba(80,0,255,0.45)",
        boxShadow: "0 0 0 2px rgba(16,0,16,0.95)",
        padding: "6px 8px",
        whiteSpace: "nowrap",
      }}
    >
      <div style={{ display: "flex" }}>
        {item.name ? (
          parseLegacy(item.name).map((f, i) => (
            <McText key={i} text={f.text} color={f.color ?? "#FFFFFF"} bold={f.bold} />
          ))
        ) : (
          <McText text={prettyType(item.type)} />
        )}
      </div>
      {item.enchants &&
        Object.entries(item.enchants).map(([e, lvl]) => (
          <McText key={e} text={enchantLabel(e, lvl)} color="#AAAAAA" style={{ marginTop: 2 }} />
        ))}
      {item.name && <McText text={prettyType(item.type)} color="#555555" style={{ marginTop: 2 }} />}
    </div>,
    document.body,
  );
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
  const [smooth, setSmooth] = useState(false);
  const type = item?.type;
  useEffect(() => {
    setBroken(false);
    setSmooth(false);
  }, [type]);
  const [tip, setTip] = useState<{ x: number; y: number } | null>(null);
  const enchanted = !!item && !!item.enchants && Object.keys(item.enchants).length > 0;
  const label = item ? prettyType(item.type) : undefined;
  const pos =
    x !== undefined && y !== undefined
      ? ({ position: "absolute", left: x * S, top: y * S } as const)
      : ({ position: "relative" } as const);
  return (
    <div
      onMouseEnter={(e) => setTip({ x: e.clientX, y: e.clientY })}
      onMouseMove={(e) => setTip({ x: e.clientX, y: e.clientY })}
      onMouseLeave={() => setTip(null)}
      style={{
        ...pos,
        width: CELL,
        height: CELL,
        background: "#8b8b8b",
        boxShadow: `inset ${S}px ${S}px 0 #373737, inset ${-S}px ${-S}px 0 #ffffff`,
        ...(held ? { zIndex: 1 } : {}),
      }}
    >
      {held && (
        <img
          src="/mc-tex/_hotbar_selection.png"
          alt=""
          style={{
            position: "absolute",
            left: -3 * S,
            top: -3 * S,
            width: 24 * S,
            height: 23 * S,
            maxWidth: "none",
            imageRendering: "pixelated",
            pointerEvents: "none",
            zIndex: 3,
          }}
        />
      )}
      {tip && (
        <span
          style={{
            position: "absolute",
            left: S,
            top: S,
            width: ITEM,
            height: ITEM,
            background: "rgba(255,255,255,0.45)",
            pointerEvents: "none",
            zIndex: 2,
          }}
        />
      )}
      {tip && item && <ItemTooltip item={item} at={tip} />}
      {!item && ghost && <GhostImg kind={ghost} />}
      {item && !broken && (
        <img
          src={texUrl(item.type)}
          alt={label}
          onError={() => setBroken(true)}
          onLoad={(e) => {
            // Pre-rendered isometric block icons are 128px — let those
            // downscale smoothly; only true 16px sprites want pixelated.
            if (e.currentTarget.naturalWidth >= 64) setSmooth(true);
          }}
          style={{
            position: "absolute",
            left: S,
            top: S,
            width: ITEM,
            height: ITEM,
            imageRendering: smooth ? "auto" : "pixelated",
          }}
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
        <McText
          text={String(item.count)}
          style={{ position: "absolute", right: -2 * S, bottom: 0, zIndex: 4 }}
        />
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
          marginLeft: "auto",
          marginRight: "auto",
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
            height: 72 * S,
            background: "#000",
            boxShadow: `inset ${S}px ${S}px 0 #373737, inset ${-S}px ${-S}px 0 #ffffff`,
          }}
        >
          <div style={{ position: "absolute", left: "50%", top: "50%", transform: "translate(-50%, -50%)" }}>
            <SkinBody uuid={uuid} scale={2 * S} />
          </div>
        </div>

        {/* offhand */}
        <GroupFrame x={77} y={61} w={18} h={18} />
        <Slot x={77} y={61} item={inventory.offHand} ghost="shield" />

        {/* crafting (decorative — a viewer can't craft) */}
        <McText
          text="Crafting"
          color="#404040"
          shadow={false}
          style={{ position: "absolute", left: 97 * S, top: 8 * S }}
        />
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
          display: "table",
          marginLeft: "auto",
          marginRight: "auto",
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
