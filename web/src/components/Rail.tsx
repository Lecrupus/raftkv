import { ArrowUpRight } from "lucide-react";
import { useState } from "react";
import type { GuideStep, StepId } from "../hooks/useGuide";
import { cx } from "./primitives";

interface RailProps {
  steps: GuideStep[];
  next: StepId | null;
}

/**
 * The off-centre calibrated rail: brand, thesis, and the guided steps hung
 * off one vertical hairline. Each step's tick is a line form: done = mercury,
 * next = doubled sodium (the only place the accent marks "do this"), pending
 * = seam. Steps check themselves off from the cluster's real behaviour.
 */
/** Stack facts and source link; in the rail on wide screens, at the page end on phones. */
export function Colophon({ className }: { className?: string }) {
  return (
    <footer className={cx("gap-3 border-t border-seam pt-4 text-[13px] text-bone-3", className)}>
      <p className="m-0">Raft consensus · write-ahead log · Pre-Vote · linearizable reads · idempotent retries · Go, zero dependencies</p>
      <a href="https://github.com/Lecrupus/raftkv" target="_blank" rel="noopener" className="inline-flex items-center gap-1.5 text-bone-2">
        Source on GitHub
        <ArrowUpRight size={14} strokeWidth={1.5} aria-hidden="true" />
      </a>
    </footer>
  );
}

export function Rail({ steps, next }: RailProps) {
  // On phones the guide starts folded so the live readouts make the first screen.
  const [open] = useState(() => window.matchMedia("(min-width: 1024px)").matches);
  const doneCount = steps.filter((s) => s.done).length;
  return (
    <aside className="flex flex-col gap-6 border-seam px-4 pt-5 pb-6 lg:sticky lg:top-0 lg:h-dvh lg:overflow-y-auto lg:border-r lg:px-6" aria-label="About this demo">
      <div className="flex items-center gap-2.5">
        <svg width="22" height="22" viewBox="0 0 22 22" aria-hidden="true">
          <line x1="4" x2="4" y1="2" y2="20" style={{ stroke: "var(--nm589)" }} strokeWidth="1.5" />
          <line x1="7.5" x2="7.5" y1="2" y2="20" style={{ stroke: "var(--nm589)" }} strokeWidth="1.5" />
          <line x1="13" x2="13" y1="2" y2="20" style={{ stroke: "var(--nm486)" }} strokeWidth="1.5" />
          <line x1="18" x2="18" y1="2" y2="20" style={{ stroke: "var(--nm486)" }} strokeWidth="1.5" strokeDasharray="3 2.5" />
        </svg>
        <span className="text-[19px] font-semibold tracking-[0.01em]">RaftKV</span>
        <span className="legend mt-0.5">live cluster</span>
      </div>

      <div>
        <h1 className="m-0 text-[26px] leading-[1.1] font-medium tracking-[-0.01em] text-balance">Break it and watch it heal.</h1>
        <p className="mt-2 mb-0 max-w-[46ch] text-[14px] text-bone-2">
          A distributed key-value store where every write is agreed on by a majority via Raft. Kill nodes or cut them off from the
          network: the cluster re-elects a leader and never loses a committed write.
        </p>
      </div>

      <details open={open} className="group" data-testid="guide">
        <summary className="flex cursor-pointer list-none items-center justify-between">
          <span className="legend !text-bone-2">Try this</span>
          <span className="legend" data-testid="guide-progress">{doneCount} / {steps.length} proven</span>
        </summary>
        <ol className="relative mt-3 mb-0 grid list-none gap-3 p-0 pl-5">
          {/* the rail itself */}
          <span aria-hidden="true" className="absolute top-1 bottom-1 left-[5px] w-px bg-seam" />
          {steps.map((s) => {
            const isNext = s.id === next;
            return (
              <li
                key={s.id}
                data-testid={`guide-step-${s.id}`}
                data-done={s.done}
                aria-current={isNext ? "step" : undefined}
                className={cx("relative text-[14px] leading-snug", s.done ? "text-bone-3" : isNext ? "text-bone" : "text-bone-2")}
              >
                <svg width="12" height="18" aria-hidden="true" className="absolute top-0.5 -left-5">
                  {s.done ? (
                    <line x1="6" x2="6" y1="0" y2="18" style={{ stroke: "var(--nm546)" }} strokeWidth="2" />
                  ) : isNext ? (
                    <g style={{ stroke: "var(--nm589)" }} strokeWidth="1.5">
                      <line x1="3.5" x2="3.5" y1="0" y2="18" />
                      <line x1="8.5" x2="8.5" y1="0" y2="18" />
                    </g>
                  ) : (
                    <line x1="6" x2="6" y1="5" y2="13" style={{ stroke: "var(--seam-strong)" }} strokeWidth="1.5" />
                  )}
                </svg>
                <b className={cx("font-semibold", s.done ? "text-bone-2" : "text-bone")}>{s.title}</b> {s.body}
                {s.done && <span className="sr-only"> (done)</span>}
              </li>
            );
          })}
        </ol>
      </details>

      <Colophon className="mt-auto hidden lg:grid" />
    </aside>
  );
}
