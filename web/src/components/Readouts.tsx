import type { ReactNode } from "react";
import { stateOf, type ClusterView } from "../api/types";
import type { Sample } from "../hooks/useCluster";
import { clockTime, int, ms } from "../lib/format";
import { LineGlyph, cx } from "./primitives";

interface ReadoutsProps {
  view: ClusterView;
  history: Sample[];
  stale: boolean;
}

function Cell({ label, children, sub, testId, className }: { label: string; children: ReactNode; sub: ReactNode; testId: string; className?: string }) {
  return (
    <div data-testid={testId} className={cx("min-w-0 border-seam px-4 py-3 lg:px-6", className)}>
      <dt className="legend">{label}</dt>
      <dd className="m-0">
        <div className="readout flex items-center gap-2 truncate" data-testid={`${testId}-value`}>{children}</div>
        <div className="mt-0.5 truncate text-[13px] text-bone-2">{sub}</div>
      </dd>
    </div>
  );
}

/** Commits per second over the last ~5 s of client history, measured on the leader's index. */
function commitRate(history: Sample[]): number | null {
  const last = history.at(-1);
  if (!last) return null;
  const from = history.find((s) => s.t >= last.t - 5_000);
  if (!from || from === last) return null;
  const c = (s: Sample) => Math.max(0, ...Object.values(s.nodes).map((n) => n.commit));
  return ((c(last) - c(from)) / (last.t - from.t)) * 1000;
}

/**
 * Failovers drawn as emission lines on a log-ms rail (10 ms – 3 s), so the
 * spread of measured election times reads at a glance. Newest is doubled.
 */
function FailoverRail({ values }: { values: number[] }) {
  const W = 132;
  const lo = Math.log10(10);
  const hi = Math.log10(3000);
  const x = (v: number) => 2 + ((Math.log10(Math.min(3000, Math.max(10, v))) - lo) / (hi - lo)) * (W - 4);
  return (
    <svg width={W} height="22" aria-hidden="true" className="shrink-0">
      <line x1="0" x2={W} y1="21.5" y2="21.5" style={{ stroke: "var(--seam-strong)" }} />
      {values.map((v, i) => {
        const last = i === values.length - 1;
        return last ? (
          <g key={i} style={{ stroke: "var(--nm589)" }}>
            <line x1={x(v) - 1.5} x2={x(v) - 1.5} y1="2" y2="21" />
            <line x1={x(v) + 1.5} x2={x(v) + 1.5} y1="2" y2="21" />
          </g>
        ) : (
          <line key={i} x1={x(v)} x2={x(v)} y1="8" y2="21" style={{ stroke: "var(--nm615)" }} opacity="0.7" />
        );
      })}
    </svg>
  );
}

/**
 * The five numbers a reviewer reads first. Quorum is the hinge of the
 * whole demo, so it says in words whether writes are being accepted.
 */
export function Readouts({ view, history, stale }: ReadoutsProps) {
  const leader = view.nodes.find((n) => n.id === view.leader);
  const healthy = view.nodes.filter((n) => {
    const s = stateOf(n);
    return s !== "down" && s !== "partition";
  }).length;
  const total = view.nodes.length;
  const majority = Math.floor(total / 2) + 1;
  const hasQuorum = healthy >= majority;
  const fo = view.failovers ?? [];
  const lastFo = fo.at(-1);
  const rate = commitRate(history);
  const maxCommit = Math.max(0, ...view.nodes.map((n) => n.status?.commitIndex ?? 0));
  const elections = view.nodes.reduce((a, n) => a + (n.status?.elections ?? 0), 0);

  return (
    <dl
      aria-label="Cluster readouts"
      aria-busy={stale || undefined}
      className={cx(
        "m-0 grid grid-cols-2 border-seam md:grid-cols-3 xl:grid-cols-5",
        "[&>div]:border-t xl:[&>div]:border-t-0 xl:[&>div+div]:border-l",
        stale && "opacity-60",
      )}
    >
      <Cell
        label="Leader"
        testId="readout-leader"
        sub={leader ? `since ${lastFo && lastFo.newLeader === view.leader ? clockTime(Date.parse(lastFo.at)) : "boot"}` : hasQuorum ? "election in progress" : "no majority, no election"}
      >
        {leader ? (
          <>
            <LineGlyph state="leader" height={28} />
            <span>{view.leader}</span>
          </>
        ) : (
          <span className="text-nm615">none</span>
        )}
      </Cell>

      <Cell label="Term" testId="readout-term" sub={`${int(elections)} election${elections === 1 ? "" : "s"} won`}>
        {leader?.status ? int(leader.status.term) : "—"}
      </Cell>

      <Cell
        label="Quorum"
        testId="readout-quorum"
        sub={
          <span className={hasQuorum ? undefined : "text-nm656"} data-testid="quorum-verdict">
            {hasQuorum ? `majority is ${majority} · writes accepted` : `needs ${majority} · writes stall (by design)`}
          </span>
        }
      >
        <span>{healthy}</span>
        <span className="text-[17px] text-bone-3">/ {total}</span>
        <svg width={total * 8 + 4} height="24" aria-hidden="true" className="ml-1">
          {view.nodes.map((n, i) => {
            const s = stateOf(n);
            const ok = s !== "down" && s !== "partition";
            return (
              <line
                key={n.id}
                x1={4 + i * 8}
                x2={4 + i * 8}
                y1={ok ? 2 : 12}
                y2="22"
                style={{ stroke: ok ? "var(--nm546)" : "var(--nm656)" }}
                strokeWidth="1.5"
              />
            );
          })}
          {/* the majority line */}
          <line x1="0" x2={total * 8 + 4} y1="8" y2="8" style={{ stroke: "var(--bone-3)" }} strokeDasharray="2 2" />
        </svg>
      </Cell>

      <Cell
        label="Committed"
        testId="readout-commit"
        sub={rate === null ? "measuring…" : rate >= 1 ? `+${int(rate)} entries / s` : "idle"}
      >
        {int(leader?.status?.commitIndex ?? maxCommit)}
      </Cell>

      <Cell
        label="Last failover"
        testId="readout-failover"
        className="col-span-2 md:col-span-1"
        sub={lastFo ? `${lastFo.oldLeader} → ${lastFo.newLeader} · ${lastFo.cause}` : "kill the leader to measure one"}
      >
        {lastFo ? (
          <>
            <span>
              {ms(lastFo.ms)}
              <span className="text-[17px] text-bone-3"> ms</span>
            </span>
            {fo.length > 1 && <FailoverRail values={fo.map((f) => f.ms)} />}
          </>
        ) : (
          "—"
        )}
      </Cell>
    </dl>
  );
}
