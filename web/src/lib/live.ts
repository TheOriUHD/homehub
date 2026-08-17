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
