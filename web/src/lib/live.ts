import { useEffect } from "react";
import { useQueryClient, type QueryKey } from "@tanstack/react-query";
import { live } from "./ws";

/**
 * useLiveInvalidate subscribes to a WS topic for the component's lifetime and
 * invalidates the given query keys when events arrive — push-driven refresh
 * without client polling loops.
 */
export function useLiveInvalidate(topic: string | null, keys: QueryKey[], events?: string[]) {
  const queryClient = useQueryClient();
  useEffect(() => {
    if (!topic) return;
    return live.subscribe(topic, (data) => {
      const event = (data as { event?: string } | undefined)?.event;
      if (events && event && !events.includes(event)) return;
      for (const key of keys) {
        void queryClient.invalidateQueries({ queryKey: key });
      }
    });
    // keys are stable arrays declared at call sites.
  }, [topic, queryClient, JSON.stringify(keys), JSON.stringify(events)]);
}

/**
 * useAgentLive keeps the page in step with whatever the assistant just did.
 *
 * The agent reaches the same API a click does, so the honest fix for any one
 * screen is a topic for that domain — and calendars now have one. But the
 * agent can touch anything, and a rule per endpoint is a list that falls out
 * of step with the next tool added. So the server announces every change the
 * agent makes on a per-user topic, and this invalidates the queries that are
 * actually on screen: React Query refetches only the mounted ones, which is
 * exactly the set the reader is looking at.
 *
 * Coalesced, because one turn can run several actions and the page should
 * settle once rather than flicker through each.
 */
export function useAgentLive() {
  const queryClient = useQueryClient();
  useEffect(() => {
    let timer: number | undefined;
    const unsubscribe = live.subscribe("agent", () => {
      window.clearTimeout(timer);
      timer = window.setTimeout(() => {
        void queryClient.invalidateQueries();
      }, 250);
    });
    return () => {
      window.clearTimeout(timer);
      unsubscribe();
    };
  }, [queryClient]);
}

export type LiveSample = { resource: string; metric: string; ts: number; value: number };

/** useLiveSamples streams metric samples for one instance topic. */
export function useLiveSamples(topic: string | null, onSamples: (samples: LiveSample[]) => void) {
  useEffect(() => {
    if (!topic) return;
    return live.subscribe(topic, (data) => {
      const payload = data as { samples?: LiveSample[] } | undefined;
      if (payload?.samples?.length) onSamples(payload.samples);
    });
  }, [topic, onSamples]);
}
