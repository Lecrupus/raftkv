import { useEffect, useState } from "react";
import { runBench } from "../api/client";
import type { BenchResult } from "../api/types";
import { int, ms } from "../lib/format";
import { Button, Field, Panel, Select, TextInput } from "./primitives";

type Wl = "nm546" | "nm589" | "nm615" | "nm656";
const MARKS: Array<{ key: keyof BenchResult; label: string; wl: Wl }> = [
  { key: "p50ms", label: "p50", wl: "nm546" },
  { key: "p95ms", label: "p95", wl: "nm589" },
  { key: "p99ms", label: "p99", wl: "nm615" },
  { key: "maxMs", label: "max", wl: "nm656" },
];

/**
 * Latency percentiles as emission lines on a log-ms rail (0.1 ms – 1 s).
 * Labels stagger onto two rows so close percentiles never collide.
 */
function LatencyRail({ r }: { r: BenchResult }) {
  const W = 320;
  const lo = -1;
  const hi = 3;
  const x = (v: number) => 6 + ((Math.log10(Math.max(0.1, Math.min(1000, v))) - lo) / (hi - lo)) * (W - 12);
  const decades = [0.1, 1, 10, 100, 1000];
  return (
    <svg viewBox={`0 0 ${W} 64`} className="block h-16 w-full" role="img" aria-label={`Latency: ${MARKS.map((m) => `${m.label} ${ms(r[m.key] as number)} ms`).join(", ")}`}>
      <line x1="0" x2={W} y1="44.5" y2="44.5" style={{ stroke: "var(--seam-strong)" }} />
      {decades.map((d) => (
        <g key={d}>
          <line x1={x(d)} x2={x(d)} y1="44" y2="48" style={{ stroke: "var(--seam-strong)" }} />
          <text x={x(d)} y="60" textAnchor="middle" className="legend" style={{ fill: "var(--bone-3)", letterSpacing: "0.04em" }}>
            {d < 1 ? d : int(d)}
          </text>
        </g>
      ))}
      <text x={W} y="60" textAnchor="end" className="legend" style={{ fill: "var(--bone-3)" }}>ms</text>
      {MARKS.map((m, i) => {
        const v = r[m.key] as number;
        const lx = x(v);
        const top = i % 2 === 0 ? 14 : 2;
        return (
          <g key={m.label} style={{ stroke: `var(--${m.wl})` }} data-testid={`latency-${m.label}`}>
            <line x1={lx} x2={lx} y1={top + 4} y2="44" strokeWidth="1.5" />
            <text x={lx + 3} y={top + 8} className="legend" style={{ fill: `var(--${m.wl})`, stroke: "none" }}>
              {m.label}
            </text>
          </g>
        );
      })}
    </svg>
  );
}

/** Load test over real HTTP. Try killing the leader mid-run: errors stay at 0 while a majority survives. */
export function LoadPanel() {
  const [ops, setOps] = useState(1000);
  const [clients, setClients] = useState(16);
  const [reads, setReads] = useState("0");
  const [running, setRunning] = useState(false);
  const [elapsed, setElapsed] = useState(0);
  const [result, setResult] = useState<BenchResult | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!running) return;
    const t0 = performance.now();
    const id = setInterval(() => setElapsed((performance.now() - t0) / 1000), 100);
    return () => clearInterval(id);
  }, [running]);

  async function start() {
    setRunning(true);
    setError(null);
    setElapsed(0);
    try {
      setResult(await runBench({ ops, clients, reads: Number(reads) }));
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setRunning(false);
    }
  }

  return (
    <Panel title="Load test" testId="bench-panel" aside={<span className="legend">fsync to a majority</span>}>
      <div className="grid grid-cols-3 gap-3">
        <Field label="Operations">
          <TextInput type="number" min={1} max={5000} value={ops} onChange={(e) => setOps(Number(e.target.value))} data-testid="bench-ops" />
        </Field>
        <Field label="Clients">
          <TextInput type="number" min={1} max={32} value={clients} onChange={(e) => setClients(Number(e.target.value))} data-testid="bench-clients" />
        </Field>
        <Field label="Reads">
          <Select value={reads} onChange={(e) => setReads(e.target.value)} data-testid="bench-reads">
            <option value="0">0%</option>
            <option value="0.5">50%</option>
            <option value="0.9">90%</option>
          </Select>
        </Field>
      </div>
      <Button
        variant="primary"
        className="mt-3 w-full"
        onClick={() => void start()}
        disabled={running}
        busy={running}
        busyLabel={`Running ${elapsed.toFixed(1)} s · kill the leader now`}
        data-testid="bench-run"
      >
        Run load test
      </Button>

      <div role="status" aria-live="polite" data-testid="bench-result" data-state={running ? "running" : result ? "done" : error ? "error" : "idle"} className="mt-3 border-t border-seam pt-3">
        {error ? (
          <p className="m-0 text-[13px] text-nm656">Load test failed: {error}</p>
        ) : result ? (
          <>
            <div className="flex items-baseline justify-between gap-3">
              <p className="m-0">
                <span className="readout" data-testid="bench-throughput">{int(result.opsPerSec)}</span>
                <span className="ml-1 text-[13px] text-bone-3">ops / s</span>
              </p>
              <p className="m-0 text-right text-[13px] text-bone-2">
                {int(result.ops)} ops · {result.seconds.toFixed(2)} s ·{" "}
                <span className={result.errors ? "text-nm656" : undefined} data-testid="bench-errors">
                  {int(result.errors)} errors
                </span>
              </p>
            </div>
            <LatencyRail r={result} />
          </>
        ) : (
          <p className="m-0 text-[13px] text-bone-3">
            Concurrent clients over real HTTP. Every write is fsynced to a majority's WAL before the reply. Try killing the leader mid-run.
          </p>
        )}
      </div>
    </Panel>
  );
}
