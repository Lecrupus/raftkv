import type { NodeAction, NodeView } from "../api/types";
import { stateOf } from "../api/types";
import { int, STATE_LABEL, STATE_WAVELENGTH, wl } from "../lib/format";
import { Button, LineGlyph, cx } from "./primitives";
import { LANE_H } from "./Spectrum";

interface NodeStripProps {
  node: NodeView;
  /** The leader's commit index, to show how far behind this node is. */
  leaderCommit: number | null;
  pending: NodeAction | null;
  onAction: (id: string, action: NodeAction) => void;
}

/**
 * One node's controls, aligned to its spectrum lane.
 *
 * Shows divergence rather than totals: lag behind the leader's commit and
 * the uncommitted log tail, which are the numbers that actually differ
 * between nodes. Network control sits first; the destructive power control
 * is isolated at the far edge.
 */
export function NodeStrip({ node, leaderCommit, pending, onAction }: NodeStripProps) {
  const state = stateOf(node);
  const s = node.status;
  const lag = s && leaderCommit !== null ? Math.max(0, leaderCommit - s.commitIndex) : null;
  const tail = s ? Math.max(0, s.lastLogIndex - s.commitIndex) : null;
  const powerAction: NodeAction = node.up ? "kill" : "start";
  const netAction: NodeAction = s?.isolated ? "heal" : "isolate";

  return (
    <article
      data-testid={`node-strip-${node.id}`}
      data-node-id={node.id}
      data-state={state}
      aria-label={`Node ${node.id}, ${STATE_LABEL[state]}`}
      style={{ minHeight: LANE_H }}
      className={cx(
        "grid grid-cols-[auto_minmax(0,1fr)_auto] items-center gap-x-3 border-b border-seam px-3 py-2 lg:h-[72px] lg:py-0",
        state === "down" && "grayscale-[0.6]",
      )}
    >
      <div className="flex items-center gap-2">
        <LineGlyph state={state} height={28} />
        <span className="text-[17px] font-semibold tracking-[0.01em]">{node.id}</span>
      </div>

      <div className="min-w-0">
        <p className="legend" style={{ color: wl(STATE_WAVELENGTH[state]) }} data-testid={`node-${node.id}-role`}>
          {STATE_LABEL[state]}
          {s && <span className="text-bone-3"> · term {s.term}</span>}
        </p>
        <p className="truncate text-[13px] text-bone-2">
          {s ? (
            <>
              <span data-testid={`node-${node.id}-lag`}>{lag === 0 ? "in sync" : lag === null ? "no leader" : `${int(lag)} behind`}</span>
              {tail ? ` · ${int(tail)} uncommitted` : ""}
              {" · "}
              {int(s.kv.keys)} {s.kv.keys === 1 ? "key" : "keys"}
            </>
          ) : (
            "stopped · WAL kept on disk"
          )}
        </p>
      </div>

      <div className="flex items-center gap-2">
        <Button
          size="sm"
          data-testid={`node-${node.id}-network`}
          disabled={!node.up || pending !== null}
          busy={pending === "isolate" || pending === "heal"}
          busyLabel="…"
          onClick={() => onAction(node.id, netAction)}
          className="w-[84px]"
        >
          {s?.isolated ? "Heal" : "Partition"}
        </Button>
        <span aria-hidden="true" className="h-6 w-px bg-seam" />
        <Button
          size="sm"
          variant={node.up ? "destructive" : "primary"}
          data-testid={`node-${node.id}-power`}
          disabled={pending !== null}
          busy={pending === "kill" || pending === "start"}
          busyLabel="…"
          onClick={() => onAction(node.id, powerAction)}
          className="w-[64px]"
        >
          {node.up ? "Kill" : "Start"}
        </Button>
      </div>
    </article>
  );
}
