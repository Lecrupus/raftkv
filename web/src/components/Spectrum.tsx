import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import type { NodeState, RaftEvent } from "../api/types";
import type { Sample } from "../hooks/useCluster";
import { eventWavelength, STATE_LABEL, STATE_WAVELENGTH, wl } from "../lib/format";

/** Vertical rhythm shared with the node strips so lanes and strips align row for row. */
export const LANE_H = 72;
export const TOP_H = 20;
export const AXIS_H = 22;

interface SpectrumProps {
  nodeIds: string[];
  history: Sample[];
  events: RaftEvent[];
  /** client − server clock, from useCluster. */
  clockOffset: number;
  windowMs: number;
}

interface Segment {
  state: NodeState;
  t0: number;
  t1: number;
}

/** Merge consecutive samples with the same state into drawable runs. */
function segmentsFor(id: string, history: Sample[]): Segment[] {
  const out: Segment[] = [];
  for (let i = 0; i < history.length; i++) {
    const s = history[i]!;
    const state = s.nodes[id]?.state ?? "down";
    const tNext = history[i + 1]?.t ?? s.t + 60_000; // last run extends past the clip edge
    const prev = out.at(-1);
    if (prev && prev.state === state) prev.t1 = tNext;
    else out.push({ state, t0: s.t, t1: tNext });
  }
  return out;
}

/** Deterministic banding for the continuum, so the plate never shimmers between renders. */
function continuumBands(width: number): Array<{ x: number; w: number; a: number }> {
  let seed = 0x2f6e2b1;
  const rand = () => ((seed = (seed * 1103515245 + 12345) & 0x7fffffff) / 0x7fffffff);
  const bands: Array<{ x: number; w: number; a: number }> = [];
  for (let x = 0; x < width; ) {
    const w = 2 + rand() * 14;
    bands.push({ x, w, a: 0.012 + rand() * 0.035 });
    x += w;
  }
  return bands;
}

function useWidth<T extends HTMLElement>() {
  const ref = useRef<T>(null);
  const [width, setWidth] = useState(0);
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    const ro = new ResizeObserver(([entry]) => setWidth(Math.round(entry!.contentRect.width)));
    ro.observe(el);
    return () => ro.disconnect();
  }, []);
  return [ref, width] as const;
}

/**
 * The cluster as a spectrogram.
 *
 * Each node is a horizontal lane on one shared time rail (right edge = now).
 * Its trace is drawn in line FORM: doubled = leader, solid = follower,
 * dashed = candidate, dotted = partitioned, struck hatch = down. Commit
 * progress rises from the lane floor as thin mercury ticks, so a load test
 * becomes a dense continuum. Raft events land as vertical emission lines;
 * a new leader's line lands with one damped overshoot.
 *
 * Performance: content is laid out once per poll against an anchor time; a
 * single rAF loop slides one <g> left so the rail scrolls smoothly at 60 fps
 * without re-rendering React.
 */
export function Spectrum({ nodeIds, history, events, clockOffset, windowMs }: SpectrumProps) {
  const [wrapRef, W] = useWidth<HTMLDivElement>();
  const slideRef = useRef<SVGGElement>(null);
  const H = TOP_H + nodeIds.length * LANE_H + AXIS_H;
  const lanesBottom = TOP_H + nodeIds.length * LANE_H;
  const pxPerMs = W / windowMs;

  // Everything inside the sliding group is positioned relative to this anchor.
  const anchor = useMemo(() => history.at(-1)?.t ?? Date.now(), [history]);
  const x = (t: number) => W - (anchor - t) * pxPerMs;

  useEffect(() => {
    if (!W) return;
    let raf = 0;
    const frame = () => {
      const dx = (Date.now() - anchor) * pxPerMs;
      slideRef.current?.setAttribute("transform", `translate(${-dx.toFixed(2)} 0)`);
      raf = requestAnimationFrame(frame);
    };
    raf = requestAnimationFrame(frame);
    return () => cancelAnimationFrame(raf);
  }, [anchor, pxPerMs, W]);

  const bands = useMemo(() => continuumBands(W), [W]);
  const windowStart = anchor - windowMs - 2_000;
  const laneY = (i: number) => TOP_H + i * LANE_H;

  const current = history.at(-1);
  const summary = current
    ? nodeIds.map((id) => `${id} ${STATE_LABEL[current.nodes[id]?.state ?? "down"].toLowerCase()}`).join(", ")
    : "waiting for the first poll";

  const visibleEvents = events
    .map((e) => ({ e, t: Date.parse(e.time) + clockOffset }))
    .filter(({ e, t }) => t >= windowStart && e.kind !== "follower" && nodeIds.includes(e.node));

  const tickStep = windowMs <= 30_000 ? 5_000 : windowMs <= 60_000 ? 10_000 : 20_000;
  const ticks: number[] = [];
  for (let s = 0; s <= windowMs; s += tickStep) ticks.push(s);

  return (
    <div ref={wrapRef} className="relative min-w-0" data-testid="spectrum">
      {W > 0 && (
        <svg
          width={W}
          height={H}
          viewBox={`0 0 ${W} ${H}`}
          role="img"
          aria-label={`Cluster timeline, last ${Math.round(windowMs / 1000)} seconds. Now: ${summary}. Every event is also listed in the journal.`}
          className="block"
        >
          <defs>
            <clipPath id="rail-clip">
              <rect x="0" y="0" width={W} height={lanesBottom} />
            </clipPath>
            <pattern id="struck" width="9" height="12" patternUnits="userSpaceOnUse">
              <line x1="1" y1="11" x2="8" y2="1" style={{ stroke: wl("nm656") }} strokeWidth="1.25" />
            </pattern>
          </defs>

          {/* the continuum: a static banded plate behind the moving rail */}
          <rect x="0" y={TOP_H} width={W} height={lanesBottom - TOP_H} style={{ fill: "var(--continuum)" }} />
          {bands.map((b, i) => (
            <rect key={i} x={b.x} y={TOP_H} width={b.w} height={lanesBottom - TOP_H} style={{ fill: `rgb(var(--band) / ${b.a})` }} />
          ))}

          {/* lane seams and ids (static) */}
          {nodeIds.map((id, i) => (
            <g key={id}>
              <line x1="0" x2={W} y1={laneY(i) + 0.5} y2={laneY(i) + 0.5} style={{ stroke: "var(--seam)" }} />
              <text x="10" y={laneY(i) + 16} className="legend" style={{ fill: "var(--bone-3)", textTransform: "none", letterSpacing: "0.06em" }}>{id}</text>
            </g>
          ))}
          <line x1="0" x2={W} y1={lanesBottom - 0.5} y2={lanesBottom - 0.5} style={{ stroke: "var(--seam)" }} />

          <g clipPath="url(#rail-clip)">
            <g ref={slideRef}>
              {nodeIds.map((id, i) => {
                const mid = laneY(i) + LANE_H / 2;
                const segs = segmentsFor(id, history);
                const state = current?.nodes[id]?.state ?? "down";
                return (
                  <g key={id} data-testid={`spectrum-lane-${id}`} data-state={state}>
                    {segs.map((s, k) => {
                      const x0 = Math.max(x(s.t0), -20);
                      const x1 = x(s.t1);
                      if (x1 < -20) return null;
                      const c = wl(STATE_WAVELENGTH[s.state]);
                      switch (s.state) {
                        case "leader":
                          return (
                            <g key={k} style={{ stroke: c }} strokeWidth="1.25">
                              <line x1={x0} x2={x1} y1={mid - 2.5} y2={mid - 2.5} />
                              <line x1={x0} x2={x1} y1={mid + 2.5} y2={mid + 2.5} />
                            </g>
                          );
                        case "follower":
                          return <line key={k} x1={x0} x2={x1} y1={mid} y2={mid} style={{ stroke: c }} strokeWidth="1.5" />;
                        case "candidate":
                          return <line key={k} x1={x0} x2={x1} y1={mid} y2={mid} style={{ stroke: c }} strokeWidth="1.5" strokeDasharray="5 4" />;
                        case "partition":
                          return <line key={k} x1={x0} x2={x1} y1={mid + 9} y2={mid + 9} style={{ stroke: c }} strokeWidth="1.5" strokeDasharray="1.5 3.5" />;
                        case "down":
                          return <rect key={k} x={x0} y={mid - 6} width={Math.max(0, x1 - x0)} height="12" fill="url(#struck)" opacity="0.7" />;
                      }
                    })}

                    {/* commit progress: mercury ticks rising from the lane floor */}
                    {history.map((s, k) => {
                      if (k === 0) return null;
                      const a = history[k - 1]!.nodes[id];
                      const b = s.nodes[id];
                      if (!a || !b || b.state === "down" || b.commit <= a.commit) return null;
                      const tx = x(s.t);
                      if (tx < -4) return null;
                      const h = Math.min(LANE_H * 0.34, 3 + 5 * Math.log10(1 + b.commit - a.commit));
                      const floor = laneY(i) + LANE_H - 6;
                      return <line key={`c${k}`} x1={tx} x2={tx} y1={floor} y2={floor - h} style={{ stroke: wl("nm546") }} strokeWidth="1" opacity="0.8" />;
                    })}
                  </g>
                );
              })}

              {/* where this page's client-side history begins: lane traces before it are unknown, not empty */}
              {history[0] && x(history[0].t) > 8 && (
                <g data-testid="spectrum-opened">
                  <line x1={x(history[0].t)} x2={x(history[0].t)} y1={TOP_H} y2={lanesBottom} style={{ stroke: "var(--seam-strong)" }} strokeDasharray="1 3" />
                  <text x={x(history[0].t) - 6} y={lanesBottom - 8} textAnchor="end" className="legend" style={{ fill: "var(--bone-3)" }}>
                    page opened
                  </text>
                </g>
              )}

              {/* emission lines: one per Raft event */}
              {visibleEvents.map(({ e, t }) => {
                const lane = nodeIds.indexOf(e.node);
                const ex = x(t);
                const c = wl(eventWavelength(e.kind));
                const key = `${e.time}-${e.node}-${e.kind}`;
                if (e.kind === "leader") {
                  const fresh = anchor - t < 1_500;
                  const nearEdge = ex > W - 110;
                  return (
                    <g key={key} className={fresh ? "landing" : undefined} data-testid="spectrum-event" data-kind={e.kind} style={{ stroke: c }}>
                      <line x1={ex - 1.5} x2={ex - 1.5} y1={TOP_H} y2={lanesBottom} strokeWidth="1" />
                      <line x1={ex + 1.5} x2={ex + 1.5} y1={TOP_H} y2={lanesBottom} strokeWidth="1" />
                      <text
                        x={nearEdge ? ex - 6 : ex + 6}
                        y={TOP_H + 12}
                        textAnchor={nearEdge ? "end" : "start"}
                        className="legend"
                        style={{ fill: c, stroke: "none" }}
                      >
                        {`Term ${e.term ?? "?"} · `}
                        <tspan style={{ textTransform: "none" }}>{e.node}</tspan>
                      </text>
                    </g>
                  );
                }
                const y0 = laneY(lane) + 6;
                const y1 = laneY(lane) + LANE_H - 6;
                const dash =
                  e.kind === "election" || e.kind === "stepdown" ? "4 3" : e.kind === "isolated" ? "1.5 3" : undefined;
                return (
                  <line
                    key={key}
                    data-testid="spectrum-event"
                    data-kind={e.kind}
                    x1={ex}
                    x2={ex}
                    y1={e.kind === "stepdown" ? (y0 + y1) / 2 : y0}
                    y2={y1}
                    style={{ stroke: c }}
                    strokeWidth="1.5"
                    strokeDasharray={dash}
                  />
                );
              })}
            </g>
          </g>

          {/* now edge and the time axis (static: labels are relative to now) */}
          <line x1={W - 0.5} x2={W - 0.5} y1={TOP_H - 6} y2={lanesBottom + 4} style={{ stroke: "var(--bone-3)" }} />
          {ticks.map((s) => {
            const tx = W - s * pxPerMs;
            return (
              <g key={s}>
                <line x1={tx} x2={tx} y1={lanesBottom} y2={lanesBottom + 4} style={{ stroke: "var(--seam-strong)" }} />
                <text
                  x={s === 0 ? tx - 2 : tx}
                  y={lanesBottom + 16}
                  textAnchor={s === 0 ? "end" : s === windowMs ? "start" : "middle"}
                  className="legend"
                  style={{ fill: "var(--bone-3)", letterSpacing: "0.08em" }}
                >
                  {s === 0 ? "now" : `−${s / 1000}s`}
                </text>
              </g>
            );
          })}
        </svg>
      )}
      {history.length < 2 && (
        <p className="legend absolute inset-x-0 top-1/2 -translate-y-1/2 text-center">Listening to the cluster…</p>
      )}
    </div>
  );
}
