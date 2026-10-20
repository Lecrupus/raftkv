import { useCallback, useEffect, useRef, useState } from "react";
import { getCluster } from "../api/client";
import { stateOf, type ClusterView, type NodeState } from "../api/types";

/** One node's state at one poll, reduced to what the spectrum draws. */
export interface NodeSample {
  state: NodeState;
  term: number;
  commit: number;
  lastLog: number;
}

/** A poll of the whole cluster, stamped with the CLIENT clock. */
export interface Sample {
  t: number;
  leader: string;
  nodes: Record<string, NodeSample>;
}

export interface ClusterFeed {
  view: ClusterView | null;
  /** Rolling client-side history since the page opened (max HISTORY_MS). */
  history: Sample[];
  /** client clock − server clock, so server event times land on the client rail. */
  clockOffset: number;
  /** Set while the gateway is unreachable; the last good view stays on screen. */
  error: string | null;
  /** Polite announcement for screen readers when leadership changes. */
  announcement: string;
  refresh: () => Promise<void>;
}

const HISTORY_MS = 125_000;

function sampleOf(v: ClusterView, t: number): Sample {
  const nodes: Record<string, NodeSample> = {};
  for (const n of v.nodes) {
    nodes[n.id] = {
      state: stateOf(n),
      term: n.status?.term ?? 0,
      commit: n.status?.commitIndex ?? 0,
      lastLog: n.status?.lastLogIndex ?? 0,
    };
  }
  return { t, leader: v.leader, nodes };
}

/**
 * Polls /api/cluster and keeps a short client-side history for the spectrum.
 *
 * Uses a self-rescheduling timeout rather than setInterval so a slow gateway
 * never stacks requests, and keeps the last good view on failure so the page
 * degrades to "stale, retrying" instead of blanking.
 */
export function useCluster(intervalMs = 400): ClusterFeed {
  const [view, setView] = useState<ClusterView | null>(null);
  const [history, setHistory] = useState<Sample[]>([]);
  const [clockOffset, setClockOffset] = useState(0);
  const [error, setError] = useState<string | null>(null);
  const [announcement, setAnnouncement] = useState("");
  const lastLeader = useRef<string | null>(null);

  const apply = useCallback((v: ClusterView) => {
    const now = Date.now();
    setView(v);
    setError(null);
    setClockOffset(now - Date.parse(v.now));
    setHistory((h) => {
      const next = h.concat(sampleOf(v, now));
      const cutoff = now - HISTORY_MS;
      let i = 0;
      while (i < next.length - 1 && next[i]!.t < cutoff) i++;
      return i ? next.slice(i) : next;
    });

    if (lastLeader.current !== null && v.leader !== lastLeader.current) {
      const fo = v.failovers?.at(-1);
      if (v.leader) {
        const term = v.nodes.find((n) => n.id === v.leader)?.status?.term;
        const ms = fo && fo.newLeader === v.leader ? `, ${Math.round(fo.ms)} milliseconds after ${fo.cause}` : "";
        setAnnouncement(`${v.leader} elected leader${term ? ` for term ${term}` : ""}${ms}.`);
      } else {
        setAnnouncement(`No leader. ${lastLeader.current} is no longer leading; an election is due.`);
      }
    }
    lastLeader.current = v.leader;
  }, []);

  const refresh = useCallback(async () => {
    try {
      apply(await getCluster());
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    }
  }, [apply]);

  useEffect(() => {
    let timer: ReturnType<typeof setTimeout> | undefined;
    let alive = true;
    const ctrl = new AbortController();
    const tick = async () => {
      try {
        apply(await getCluster(ctrl.signal));
      } catch (e) {
        if (!alive) return;
        setError(e instanceof Error ? e.message : String(e));
      }
      if (alive) timer = setTimeout(tick, intervalMs);
    };
    void tick();
    return () => {
      alive = false;
      ctrl.abort();
      clearTimeout(timer);
    };
  }, [apply, intervalMs]);

  return { view, history, clockOffset, error, announcement, refresh };
}
