import { useState } from "react";
import { nodeAction } from "../api/client";
import type { ClusterView, NodeAction } from "../api/types";
import type { Sample } from "../hooks/useCluster";
import type { NodeState } from "../api/types";
import { STATE_LABEL } from "../lib/format";
import { NodeStrip } from "./NodeStrip";
import { LineGlyph, Select } from "./primitives";
import { AXIS_H, Spectrum, TOP_H } from "./Spectrum";

interface ClusterSectionProps {
  view: ClusterView;
  history: Sample[];
  clockOffset: number;
  /** Re-poll immediately after an action so the result shows without waiting a tick. */
  refresh: () => Promise<void>;
}

const LEGEND: NodeState[] = ["leader", "follower", "candidate", "partition", "down"];
const WINDOWS = [
  { ms: 30_000, label: "30 s" },
  { ms: 60_000, label: "1 min" },
  { ms: 120_000, label: "2 min" },
];

export function ClusterSection({ view, history, clockOffset, refresh }: ClusterSectionProps) {
  // Narrow screens show a shorter band of time, never smaller type.
  const [windowMs, setWindowMs] = useState(() => (window.matchMedia("(max-width: 639px)").matches ? 30_000 : 60_000));
  const [pending, setPending] = useState<{ id: string; action: NodeAction } | null>(null);
  const [notice, setNotice] = useState<string | null>(null);

  const leaderCommit = view.nodes.find((n) => n.id === view.leader)?.status?.commitIndex ?? null;

  async function act(id: string, action: NodeAction) {
    setPending({ id, action });
    setNotice(null);
    try {
      await nodeAction(id, action);
    } catch (e) {
      // The server answers 409 with a reason (e.g. "n1 is not running"): show it inline, never alert().
      setNotice(`${id}: ${e instanceof Error ? e.message : String(e)}`);
    } finally {
      await refresh();
      setPending(null);
    }
  }

  return (
    <section aria-labelledby="cluster-h" className="border-t border-seam" data-testid="cluster">
      <header className="flex flex-wrap items-center justify-between gap-x-6 gap-y-2 px-4 pt-3 pb-2 lg:px-6">
        <div className="flex flex-wrap items-center gap-x-5 gap-y-1">
          <h2 id="cluster-h" className="legend !text-bone-2">Cluster</h2>
          <ul className="flex flex-wrap items-center gap-x-4 gap-y-1" aria-label="Line forms">
            {LEGEND.map((s) => (
              <li key={s} className="flex items-center gap-1.5 text-[13px] text-bone-2">
                <LineGlyph state={s} height={14} />
                {STATE_LABEL[s]}
              </li>
            ))}
            <li className="flex items-center gap-1.5 text-[13px] text-bone-2">
              <svg width="14" height="14" aria-hidden="true">
                <line x1="4" x2="4" y1="13" y2="6" style={{ stroke: "var(--nm546)" }} />
                <line x1="8" x2="8" y1="13" y2="3" style={{ stroke: "var(--nm546)" }} />
                <line x1="12" x2="12" y1="13" y2="8" style={{ stroke: "var(--nm546)" }} />
              </svg>
              Commits
            </li>
          </ul>
        </div>
        <label className="flex items-center gap-2">
          <span className="legend">Window</span>
          <Select
            value={windowMs}
            onChange={(e) => setWindowMs(Number(e.target.value))}
            className="!h-8 !w-24"
            data-testid="window-select"
          >
            {WINDOWS.map((w) => (
              <option key={w.ms} value={w.ms}>{w.label}</option>
            ))}
          </Select>
        </label>
      </header>

      <p role="status" data-testid="notice" className="min-h-0 px-4 text-[13px] text-nm656 empty:hidden lg:px-6">
        {notice}
      </p>

      <div className="grid gap-0 lg:grid-cols-[minmax(0,1fr)_400px]">
        <div className="min-w-0 px-4 lg:pr-0 lg:pl-6">
          <Spectrum
            nodeIds={view.nodes.map((n) => n.id)}
            history={history}
            events={view.events ?? []}
            clockOffset={clockOffset}
            windowMs={windowMs}
          />
        </div>
        {/* strips align row-for-row with the lanes on wide screens */}
        <div className="border-t border-seam lg:border-t-0" style={{ paddingTop: undefined }}>
          <div className="hidden lg:block" style={{ height: TOP_H }} />
          <div className="lg:border-t lg:border-seam">
            {view.nodes.map((n) => (
              <NodeStrip
                key={n.id}
                node={n}
                leaderCommit={leaderCommit}
                pending={pending?.id === n.id ? pending.action : null}
                onAction={act}
              />
            ))}
          </div>
          <div className="hidden lg:block" style={{ height: AXIS_H }} />
        </div>
      </div>
    </section>
  );
}
