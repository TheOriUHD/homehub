import { useEffect, useRef } from "react";
import { Crepe } from "@milkdown/crepe";
import "@milkdown/crepe/theme/common/style.css";
import "@milkdown/crepe/theme/frame.css";
import "./milkdown-theme.css";

/**
 * WYSIWYG markdown editing via Milkdown Crepe: slash-command block insertion,
 * a selection toolbar, live tables, task lists and code blocks — editing in
 * the rendered document instead of the source. The buffer stays the truth:
 * every document change serializes back to markdown through onChange, so save
 * and dirty-tracking work exactly like the source editor.
 *
 * Imported lazily (like CodeMirror) — Crepe is heavy and only .md files in
 * rich mode ever need it.
 */
export function MilkdownEditor({
  initialValue,
  readOnly = false,
  onChange,
}: {
  initialValue: string;
  readOnly?: boolean;
  onChange?: (markdown: string) => void;
}) {
  const rootRef = useRef<HTMLDivElement>(null);
  const onChangeRef = useRef(onChange);
  onChangeRef.current = onChange;

  useEffect(() => {
    const root = rootRef.current;
    if (!root) return;
    const crepe = new Crepe({
      root,
      defaultValue: initialValue,
      // Sync features that need a backend (image upload, latex) stay off;
      // links, tables, lists, code blocks and the toolbar are the point.
      features: {
        [Crepe.Feature.ImageBlock]: false,
        [Crepe.Feature.Latex]: false,
      },
    });
    // Mounting normalizes the document (Crepe re-serializes what it parsed),
    // which fires markdownUpdated before the user has touched anything. A
    // timing guard is racy, so compare against the post-mount serialization:
    // until the first real divergence, an update equal to the mount baseline
    // is just the normalization echo — a mere mode switch never dirties the
    // buffer.
    let baseline: string | null = null;
    let sawEdit = false;
    crepe.on((api) => {
      api.markdownUpdated((_ctx, markdown) => {
        if (baseline === null) return; // still mounting
        if (!sawEdit && markdown === baseline) return; // normalization echo
        sawEdit = true;
        onChangeRef.current?.(markdown);
      });
    });
    let alive = true;
    void crepe.create().then(() => {
      if (!alive) return;
      crepe.setReadonly(readOnly);
      baseline = crepe.getMarkdown();
    });
    return () => {
      alive = false;
      void crepe.destroy();
    };
    // Recreate only when the underlying buffer identity changes — the parent
    // remounts us with a new key per file/mode, so initialValue is stable for
    // the lifetime of one mount.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  return <div ref={rootRef} className="hh-crepe h-full min-h-0 overflow-y-auto" data-readonly={readOnly || undefined} />;
}

export default MilkdownEditor;
