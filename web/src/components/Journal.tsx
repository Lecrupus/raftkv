import { useMemo, useState } from "react";
import type { RaftEvent } from "../api/types";
import { clockTime, eventWavelength, FAULT_KINDS, LEADERSHIP_KINDS, wl } from "../lib/format";
import { Panel, cx } from "./primitives";

type Filter = "all" | "leadership" | "faults";
const FILTERS: Array<{ id: Filter; label: string }> = [
  { id: "all", label: "All" },
  { id: "leadership", label: "Leadership" },
  { id: "faults", label: "Faults" },
];

/** The text record of every emission line on the spectrum, newest first. */
export function Journal({ events }: { events: RaftEvent[] }) {
  const [filter, setFilter] = useState<Filter>("all");

  const rows = useMemo(() => {
    const keep = (e: RaftEvent) =>
      filter === "all" || (filter === "leadership" ? LEADERSHIP_KINDS.has(e.kind) : FAULT_KINDS.has(e.kind));
    return events.filter(keep).slice(-120).reverse();
  }, [events, filter]);

  const tabs = (
    <div role="group" aria-label="Filter events" className="flex gap-px rounded-[3px] border border-seam-strong p-px">
      {FILTERS.map((f) => (
        <button
          key={f.id}
          type="button"
          aria-pressed={filter === f.id}
          data-testid={`journal-filter-${f.id}`}
          onClick={() => setFilter(f.id)}
          className={cx(
            "h-6 rounded-[2px] px-2 text-[12px] font-medium tracking-[0.02em] transition-colors",
            filter === f.id ? "bg-bone text-ground" : "text-bone-2 hover:bg-ground hover:text-bone",
          )}
        >
          {f.label}
        </button>
      ))}
    </div>
  );

  return (
    <Panel title="Journal" testId="journal" aside={tabs}>
      {rows.length === 0 ? (
        <p className="m-0 text-[13px] text-bone-3">
          {events.length ? "No events of this kind yet." : "Waiting for events… they appear here the moment the cluster changes."}
        </p>
      ) : (
        <ol className="m-0 -mr-2 max-h-[248px] list-none overflow-y-auto p-0 pr-2" data-testid="journal-list">
          {rows.map((e, i) => {
            const c = wl(eventWavelength(e.kind));
            return (
              <li
                key={`${e.time}-${e.node}-${e.kind}-${i}`}
                data-testid="journal-row"
                data-kind={e.kind}
                className="grid grid-cols-[88px_30px_minmax(0,1fr)] items-baseline gap-x-2 border-t border-seam py-1.5 text-[13px] first:border-t-0"
              >
                <time dateTime={e.time} className="text-[12px] text-bone-3">{clockTime(e.time)}</time>
                <span className="font-semibold">{e.node}</span>
                <span className="min-w-0 text-bone-2">
                  <span className="mr-1.5 inline-block h-2.5 w-px align-[-1px]" style={{ background: c }} aria-hidden="true" />
                  <b className="font-semibold" style={{ color: c }}>{e.kind}</b>
                  {e.term ? <span className="text-bone-3"> · term {e.term}</span> : null}
                  {e.detail ? <span> · {e.detail}</span> : null}
                </span>
              </li>
            );
          })}
        </ol>
      )}
    </Panel>
  );
}
