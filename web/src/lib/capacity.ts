// Shared "how full is it" semantics for capacity meters (storage, memory,
// CPU headroom): below 70% is fine, 70–90% warns, 90%+ is danger. TrueNAS
// pools, Proxmox nodes, and any future module read the same thresholds so a
// yellow bar means the same thing everywhere.

/** Badge/bar tone for a fill level: <70% ok, <90% warning, ≥90% danger. */
export function capacityTone(usedPct: number): "success" | "warning" | "danger" {
  if (usedPct >= 90) return "danger";
  if (usedPct >= 70) return "warning";
  return "success";
}

/** Solid fill class for a capacity bar, matching {@link capacityTone}. */
export function capacityBarClass(usedPct: number): string {
  if (usedPct >= 90) return "bg-danger";
  if (usedPct >= 70) return "bg-warning";
  return "bg-success";
}

/** Text class carrying the same tone as the bar, for the value beside it. */
export const capacityTextClass: Record<ReturnType<typeof capacityTone>, string> = {
  success: "text-success-soft-fg",
  warning: "text-warning-soft-fg",
  danger: "text-danger-soft-fg",
};
