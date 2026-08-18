import { useEffect, useMemo, useRef } from "react";
import { autocompletion, closeBrackets, closeBracketsKeymap } from "@codemirror/autocomplete";
import { defaultKeymap, history, historyKeymap, indentWithTab } from "@codemirror/commands";
import { css } from "@codemirror/lang-css";
import { html } from "@codemirror/lang-html";
import { javascript } from "@codemirror/lang-javascript";
import { json } from "@codemirror/lang-json";
import { markdown } from "@codemirror/lang-markdown";
import { python } from "@codemirror/lang-python";
import { sql } from "@codemirror/lang-sql";
import { xml } from "@codemirror/lang-xml";
import { yaml } from "@codemirror/lang-yaml";
import {
  bracketMatching,
  defaultHighlightStyle,
  foldGutter,
  foldKeymap,
  indentOnInput,
  indentUnit,
  StreamLanguage,
  syntaxHighlighting,
  HighlightStyle,
} from "@codemirror/language";
import { go } from "@codemirror/legacy-modes/mode/go";
import { lua } from "@codemirror/legacy-modes/mode/lua";
import { nginx } from "@codemirror/legacy-modes/mode/nginx";
import { properties } from "@codemirror/legacy-modes/mode/properties";
import { rust } from "@codemirror/legacy-modes/mode/rust";
import { shell } from "@codemirror/legacy-modes/mode/shell";
import { toml } from "@codemirror/legacy-modes/mode/toml";
import { highlightSelectionMatches, search, searchKeymap } from "@codemirror/search";
import { Compartment, EditorState, type Extension } from "@codemirror/state";
import {
  drawSelection,
  EditorView,
  highlightActiveLine,
  highlightActiveLineGutter,
  keymap,
  lineNumbers,
  placeholder as cmPlaceholder,
} from "@codemirror/view";
import { tags } from "@lezer/highlight";

/**
 * The utilities editor's engine: CodeMirror 6, themed with the app's own
 * design tokens so it belongs in both light and dark mode. Language is
 * picked from the file extension; unknown extensions edit as plain text
 * (still with full editing ergonomics: history, search, auto-indent,
 * bracket matching, Tab indents / Shift-Tab dedents).
 */

// extension (lowercased, no dot) → language + human label
const LANGS: Record<string, { label: string; ext: () => Extension }> = {
  js: { label: "JavaScript", ext: () => javascript() },
  mjs: { label: "JavaScript", ext: () => javascript() },
  cjs: { label: "JavaScript", ext: () => javascript() },
  jsx: { label: "JSX", ext: () => javascript({ jsx: true }) },
  ts: { label: "TypeScript", ext: () => javascript({ typescript: true }) },
  tsx: { label: "TSX", ext: () => javascript({ typescript: true, jsx: true }) },
  json: { label: "JSON", ext: () => json() },
  jsonc: { label: "JSON", ext: () => json() },
  html: { label: "HTML", ext: () => html() },
  htm: { label: "HTML", ext: () => html() },
  css: { label: "CSS", ext: () => css() },
  scss: { label: "SCSS", ext: () => css() },
  md: { label: "Markdown", ext: () => markdown() },
  markdown: { label: "Markdown", ext: () => markdown() },
  py: { label: "Python", ext: () => python() },
  sql: { label: "SQL", ext: () => sql() },
  xml: { label: "XML", ext: () => xml() },
  svg: { label: "SVG", ext: () => xml() },
  yml: { label: "YAML", ext: () => yaml() },
  yaml: { label: "YAML", ext: () => yaml() },
  go: { label: "Go", ext: () => StreamLanguage.define(go) },
  rs: { label: "Rust", ext: () => StreamLanguage.define(rust) },
  sh: { label: "Shell", ext: () => StreamLanguage.define(shell) },
  bash: { label: "Shell", ext: () => StreamLanguage.define(shell) },
  zsh: { label: "Shell", ext: () => StreamLanguage.define(shell) },
  lua: { label: "Lua", ext: () => StreamLanguage.define(lua) },
  toml: { label: "TOML", ext: () => StreamLanguage.define(toml) },
  ini: { label: "INI", ext: () => StreamLanguage.define(properties) },
  properties: { label: "Properties", ext: () => StreamLanguage.define(properties) },
  env: { label: "Env", ext: () => StreamLanguage.define(properties) },
  conf: { label: "Config", ext: () => StreamLanguage.define(nginx) },
  caddyfile: { label: "Caddyfile", ext: () => StreamLanguage.define(nginx) },
  dockerfile: { label: "Dockerfile", ext: () => StreamLanguage.define(shell) },
};

/** Human-readable language name for a file name ("Plain text" fallback). */
export function languageFor(name: string): string {
  const base = name.toLowerCase();
  const dot = base.lastIndexOf(".");
  const ext = dot >= 0 ? base.slice(dot + 1) : base;
  return LANGS[ext]?.label ?? (dot < 0 && LANGS[base] ? LANGS[base].label : "Plain text");
}

function langExtension(name: string): Extension {
  const base = name.toLowerCase();
  const dot = base.lastIndexOf(".");
  const ext = dot >= 0 ? base.slice(dot + 1) : base;
  const hit = LANGS[ext] ?? (dot < 0 ? LANGS[base] : undefined);
  return hit ? hit.ext() : [];
}

// Syntax colors on the app's token palette: readable in both themes since
// they reference CSS variables resolved at paint time.
const highlight = HighlightStyle.define([
  { tag: [tags.keyword, tags.modifier, tags.operatorKeyword], color: "var(--hh-code-keyword, #c678dd)" },
  { tag: [tags.string, tags.special(tags.string)], color: "var(--hh-code-string, #98c379)" },
  { tag: [tags.number, tags.bool, tags.null, tags.atom], color: "var(--hh-code-number, #d19a66)" },
  { tag: [tags.comment, tags.blockComment], color: "var(--hh-code-comment, #7f848e)", fontStyle: "italic" },
  { tag: [tags.function(tags.variableName), tags.function(tags.propertyName)], color: "var(--hh-code-func, #61afef)" },
  { tag: [tags.typeName, tags.className, tags.namespace], color: "var(--hh-code-type, #e5c07b)" },
  { tag: [tags.propertyName, tags.attributeName], color: "var(--hh-code-prop, #e06c75)" },
  { tag: [tags.variableName, tags.definition(tags.variableName)], color: "var(--hh-code-var, inherit)" },
  { tag: [tags.tagName], color: "var(--hh-code-tag, #e06c75)" },
  { tag: [tags.meta, tags.processingInstruction], color: "var(--hh-code-meta, #56b6c2)" },
  { tag: tags.heading, color: "var(--hh-code-func, #61afef)", fontWeight: "700" },
  { tag: tags.emphasis, fontStyle: "italic" },
  { tag: tags.strong, fontWeight: "700" },
  { tag: tags.link, color: "var(--hh-code-meta, #56b6c2)", textDecoration: "underline" },
]);

const appTheme = EditorView.theme({
  "&": {
    height: "100%",
    fontSize: "13.5px",
    backgroundColor: "transparent",
    color: "var(--color-fg, inherit)",
  },
  ".cm-content": {
    fontFamily: "ui-monospace, SFMono-Regular, Menlo, Consolas, monospace",
    caretColor: "var(--color-accent, #7c86ff)",
    padding: "10px 0",
  },
  ".cm-cursor, .cm-dropCursor": { borderLeftColor: "var(--color-accent, #7c86ff)" },
  "&.cm-focused": { outline: "none" },
  ".cm-gutters": {
    backgroundColor: "transparent",
    color: "var(--color-fg-subtle, #888)",
    border: "none",
    fontFamily: "ui-monospace, SFMono-Regular, Menlo, Consolas, monospace",
    fontSize: "12px",
  },
  ".cm-activeLine": { backgroundColor: "color-mix(in oklab, var(--color-accent, #7c86ff) 6%, transparent)" },
  ".cm-activeLineGutter": {
    backgroundColor: "transparent",
    color: "var(--color-fg, inherit)",
  },
  ".cm-selectionBackground, &.cm-focused .cm-selectionBackground": {
    backgroundColor: "color-mix(in oklab, var(--color-accent, #7c86ff) 22%, transparent) !important",
  },
  ".cm-selectionMatch": { backgroundColor: "color-mix(in oklab, var(--color-accent, #7c86ff) 14%, transparent)" },
  ".cm-matchingBracket": {
    backgroundColor: "color-mix(in oklab, var(--color-accent, #7c86ff) 18%, transparent)",
    outline: "1px solid color-mix(in oklab, var(--color-accent, #7c86ff) 45%, transparent)",
  },
  ".cm-panels": {
    backgroundColor: "var(--color-surface, #1b1e24)",
    color: "var(--color-fg, inherit)",
    border: "none",
    borderTop: "1px solid var(--color-edge, #333)",
  },
  ".cm-panels.cm-panels-top": { borderBottom: "1px solid var(--color-edge, #333)", borderTop: "none" },
  ".cm-searchMatch": { backgroundColor: "color-mix(in oklab, #eab308 30%, transparent)" },
  ".cm-searchMatch-selected": { backgroundColor: "color-mix(in oklab, #eab308 55%, transparent)" },
  ".cm-panel button": { color: "inherit" },
  ".cm-panel input": {
    backgroundColor: "var(--color-bg-subtle, #22252c)",
    color: "inherit",
    border: "1px solid var(--color-edge, #333)",
    borderRadius: "6px",
  },
  ".cm-foldGutter span": { cursor: "pointer" },
  ".cm-tooltip": {
    backgroundColor: "var(--color-surface, #1b1e24)",
    border: "1px solid var(--color-edge, #333)",
    color: "var(--color-fg, inherit)",
  },
});

export function CodeEditor({
  fileName,
  value,
  onChange,
  onSave,
  onCursor,
  placeholder,
  autoFocus,
}: {
  fileName: string;
  value: string;
  onChange: (next: string) => void;
  onSave: () => void;
  onCursor?: (line: number, col: number) => void;
  placeholder?: string;
  autoFocus?: boolean;
}) {
  const hostRef = useRef<HTMLDivElement>(null);
  const viewRef = useRef<EditorView | null>(null);
  const langCompartment = useRef(new Compartment());
  // Fresh callbacks without rebuilding the editor.
  const cbRef = useRef({ onChange, onSave, onCursor });
  cbRef.current = { onChange, onSave, onCursor };

  const baseExtensions = useMemo<Extension[]>(
    () => [
      lineNumbers(),
      highlightActiveLineGutter(),
      foldGutter(),
      history(),
      drawSelection(),
      EditorState.allowMultipleSelections.of(true),
      indentUnit.of("    "),
      indentOnInput(),
      bracketMatching(),
      closeBrackets(),
      autocompletion(),
      highlightActiveLine(),
      highlightSelectionMatches(),
      search({ top: true }),
      syntaxHighlighting(highlight),
      syntaxHighlighting(defaultHighlightStyle, { fallback: true }),
      appTheme,
      EditorView.lineWrapping,
      keymap.of([
        {
          key: "Mod-s",
          preventDefault: true,
          run: () => {
            cbRef.current.onSave();
            return true;
          },
        },
        ...closeBracketsKeymap,
        ...defaultKeymap,
        ...searchKeymap,
        ...historyKeymap,
        ...foldKeymap,
        indentWithTab,
      ]),
      EditorView.updateListener.of((update) => {
        if (update.docChanged) cbRef.current.onChange(update.state.doc.toString());
        if (update.selectionSet || update.docChanged) {
          const head = update.state.selection.main.head;
          const line = update.state.doc.lineAt(head);
          cbRef.current.onCursor?.(line.number, head - line.from + 1);
        }
      }),
      ...(placeholder ? [cmPlaceholder(placeholder)] : []),
    ],
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [],
  );

  // Create once per mounted file (key on file id at the call site).
  useEffect(() => {
    if (!hostRef.current) return;
    const state = EditorState.create({
      doc: value,
      extensions: [...baseExtensions, langCompartment.current.of(langExtension(fileName))],
    });
    const view = new EditorView({ state, parent: hostRef.current });
    viewRef.current = view;
    if (autoFocus) view.focus();
    return () => {
      view.destroy();
      viewRef.current = null;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Language follows renames live.
  useEffect(() => {
    const view = viewRef.current;
    if (!view) return;
    view.dispatch({ effects: langCompartment.current.reconfigure(langExtension(fileName)) });
  }, [fileName]);

  // External value changes (e.g. reverting) sync in without recreating.
  useEffect(() => {
    const view = viewRef.current;
    if (!view) return;
    if (view.state.doc.toString() !== value) {
      view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: value } });
    }
  }, [value]);

  return <div ref={hostRef} className="h-full min-h-0 [&_.cm-editor]:h-full [&_.cm-scroller]:overflow-auto" />;
}
