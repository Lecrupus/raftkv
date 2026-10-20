import { useEffect, useMemo, useState } from "react";
import type { ClusterView } from "../api/types";
import type { Sample } from "./useCluster";

export type StepId = "kill-leader" | "write-while-down" | "restart" | "partition-leader" | "kill-two";

export interface GuideStep {
  id: StepId;
  title: string;
  body: string;
  done: boolean;
}

const STEPS: ReadonlyArray<Omit<GuideStep, "done">> = [
  { id: "kill-leader", title: "Kill the leader.", body: "A follower times out and wins an election in a few hundred ms." },
  { id: "write-while-down", title: "Write a key", body: "while it's down. Two of three nodes is still a majority." },
  { id: "restart", title: "Start it again.", body: "It replays its WAL from disk and catches up from the new leader." },
  { id: "partition-leader", title: "Partition the leader.", body: "It can't reach a majority, so it steps down. No split-brain." },
  { id: "kill-two", title: "Kill two nodes.", body: "Writes stall rather than return wrong data. Consistency over availability." },
];

const STORE_KEY = "raftkv.guide.v1";

/** Per-viewer convenience only: progress survives a reload; the page works without storage. */
function loadDone(): ReadonlySet<StepId> {
  try {
    const raw = localStorage.getItem(STORE_KEY);
    const ids = raw ? (JSON.parse(raw) as unknown) : [];
    return new Set(Array.isArray(ids) ? ids.filter((i): i is StepId => STEPS.some((s) => s.id === i)) : []);
  } catch {
    return new Set();
  }
}

function saveDone(ids: ReadonlySet<StepId>) {
  try {
    localStorage.setItem(STORE_KEY, JSON.stringify([...ids]));
  } catch {
    // private window or blocked storage: progress just won't persist
  }
}

/**
 * The guided steps check themselves off from what the cluster actually did,
 * never from which button was pressed: a step is done when the server's
 * events, failovers or observed state prove it. Completion is sticky because
 * the server only keeps a bounded event window.
 *
 * @param wroteWhileDown set by the keyspace panel when a PUT succeeded while a node was down
 */
export function useGuide(view: ClusterView | null, history: Sample[], wroteWhileDown: boolean) {
  const [done, setDone] = useState<ReadonlySet<StepId>>(loadDone);

  const proven = useMemo(() => {
    const p = new Set<StepId>();
    if (!view) return p;
    const fo = view.failovers ?? [];
    const ev = view.events ?? [];
    if (fo.some((f) => f.cause === "leader killed")) p.add("kill-leader");
    if (wroteWhileDown) p.add("write-while-down");
    if (ev.some((e) => e.kind === "started")) p.add("restart");
    if (fo.some((f) => f.cause === "leader partitioned") || ev.some((e) => e.kind === "stepdown")) p.add("partition-leader");
    const last = history.at(-1);
    if (last) {
      const states = Object.values(last.nodes);
      const healthy = states.filter((s) => s.state !== "down" && s.state !== "partition").length;
      const down = states.filter((s) => s.state === "down").length;
      if (down >= 2 && healthy <= states.length / 2) p.add("kill-two");
    }
    return p;
  }, [view, history, wroteWhileDown]);

  useEffect(() => {
    setDone((prev) => {
      let changed = false;
      for (const id of proven) if (!prev.has(id)) changed = true;
      if (!changed) return prev;
      const next = new Set([...prev, ...proven]);
      saveDone(next);
      return next;
    });
  }, [proven]);

  const steps: GuideStep[] = STEPS.map((s) => ({ ...s, done: done.has(s.id) }));
  const next = steps.find((s) => !s.done)?.id ?? null;
  return { steps, next };
}
