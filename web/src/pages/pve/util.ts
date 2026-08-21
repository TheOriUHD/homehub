import { useEffect, useState } from "react";
import { useRouterState } from "@tanstack/react-router";

/**
 * Storage types that are cluster-wide by nature. Name-based detection is
 * wrong here — "local" exists on every node but is a different disk each
 * time — so the type decides: these get deduplicated into one "Shared
 * storage" section, everything else lists under the node that owns it. The
 * resource tree needs the same set to know which anchor a pool links to.
 */
export const SHARED_STORAGE_TYPES = new Set([
  "nfs",
  "cifs",
  "glusterfs",
  "cephfs",
  "rbd",
  "iscsi",
  "iscsidirect",
  "pbs",
  "zfs",
]);

/** storageAnchor is the DOM id a storage row carries, and the hash the tree
 *  links to. Shared pools appear once under their own section, so they key on
 *  the pool name alone; per-node pools include the node to stay unique. */
export function storageAnchor(pool: { storage: string; node: string; type: string }): string {
  return SHARED_STORAGE_TYPES.has(pool.type)
    ? `storage-shared-${pool.storage}`
    : `storage-${pool.node}-${pool.storage}`;
}

/** nodeAnchor is the DOM id of a node's card on the summary tab. */
export function nodeAnchor(node: string): string {
  return `node-${node}`;
}

/**
 * useHashFocus scrolls the element with `id` into view when the URL hash
 * points at it and reports a short-lived highlight flag, so tree clicks land
 * on the right card or row instead of the top of a long page.
 */
export function useHashFocus(id: string): boolean {
  const hash = useRouterState({ select: (s) => s.location.hash });
  const [focused, setFocused] = useState(false);

  useEffect(() => {
    if (hash !== id) {
      setFocused(false);
      return;
    }
    setFocused(true);
    // A frame's grace: the target may render in the same commit as the hash.
    const raf = requestAnimationFrame(() => {
      document.getElementById(id)?.scrollIntoView({ block: "center", behavior: "smooth" });
    });
    const timer = window.setTimeout(() => setFocused(false), 2200);
    return () => {
      cancelAnimationFrame(raf);
      window.clearTimeout(timer);
    };
  }, [hash, id]);

  return focused;
}
