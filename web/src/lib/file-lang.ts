/**
 * File-extension → language label + tone, WITHOUT importing CodeMirror (the
 * editor chunk stays lazy). Must agree with the LANGS table in
 * components/code-editor.tsx.
 */
const LABELS: Record<string, string> = {
  js: "JavaScript", mjs: "JavaScript", cjs: "JavaScript", jsx: "JSX",
  ts: "TypeScript", tsx: "TSX", json: "JSON", jsonc: "JSON",
  html: "HTML", htm: "HTML", css: "CSS", scss: "SCSS",
  md: "Markdown", markdown: "Markdown", py: "Python", sql: "SQL",
  xml: "XML", svg: "SVG", yml: "YAML", yaml: "YAML",
  go: "Go", rs: "Rust", sh: "Shell", bash: "Shell", zsh: "Shell",
  lua: "Lua", toml: "TOML", ini: "INI", properties: "Properties",
  env: "Env", conf: "Config", caddyfile: "Caddyfile", dockerfile: "Dockerfile",
  txt: "Plain text", log: "Log", csv: "CSV",
};

export function fileLanguage(name: string): string {
  const base = name.toLowerCase();
  const dot = base.lastIndexOf(".");
  const ext = dot >= 0 ? base.slice(dot + 1) : base;
  return LABELS[ext] ?? (dot < 0 && LABELS[base] ? LABELS[base] : "Plain text");
}

export function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / 1024 / 1024).toFixed(1)} MB`;
}

/** "2m ago" style relative time for file lists. */
export function timeAgo(iso: string): string {
  const s = Math.max(0, (Date.now() - new Date(iso).getTime()) / 1000);
  if (s < 60) return "just now";
  if (s < 3600) return `${Math.floor(s / 60)}m ago`;
  if (s < 86400) return `${Math.floor(s / 3600)}h ago`;
  return `${Math.floor(s / 86400)}d ago`;
}
