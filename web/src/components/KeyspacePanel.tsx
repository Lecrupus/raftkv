import { useState, type FormEvent } from "react";
import { kv, type ApiResult } from "../api/client";
import type { KVOp, KVResponse } from "../api/types";
import { ms } from "../lib/format";
import { Button, Field, Panel, TextInput } from "./primitives";

interface KeyspacePanelProps {
  /** True while any node is down: lets the guide prove "write while it's down". */
  someNodeDown: boolean;
  onWroteWhileDown: () => void;
}

interface Result {
  op: KVOp;
  key: string;
  res: ApiResult<KVResponse | string>;
}

const OP_LABEL: Record<KVOp, string> = { PUT: "PUT", GET: "GET", GET_STALE: "GET stale", DELETE: "DELETE" };

/** Outcome wavelength: ok = mercury, stale read = violet, not found = bone, failure = H-alpha. */
function outcome(r: Result): { tone: string; word: string; note?: string } {
  const body = typeof r.res.body === "object" ? r.res.body : null;
  if (r.res.status === 404 && r.op !== "PUT") return { tone: "var(--bone-2)", word: "not found" };
  if (r.res.status === 503 && (r.op === "PUT" || r.op === "DELETE")) {
    // Raft semantics: a write that times out has an UNKNOWN outcome, not a failed one.
    return {
      tone: "var(--nm656)",
      word: "not confirmed",
      note: "No majority acknowledged this write in time. The leader may keep it and commit it once a majority is back; Raft never confirms a write it has not committed.",
    };
  }
  if (!r.res.ok) return { tone: "var(--nm656)", word: r.res.status === 503 ? "unavailable" : "failed" };
  if (r.op === "GET_STALE" || body?.stale) return { tone: "var(--nm405)", word: "stale read" };
  return { tone: "var(--nm546)", word: "ok" };
}

/**
 * Client API through the cluster gateway. Every response is shown verbatim
 * with its status, latency, serving node and log index, so a stalled write
 * (no majority) reads as correct CP behaviour, not a broken page.
 */
export function KeyspacePanel({ someNodeDown, onWroteWhileDown }: KeyspacePanelProps) {
  const [key, setKey] = useState("hello");
  const [value, setValue] = useState("world");
  const [busy, setBusy] = useState<KVOp | null>(null);
  const [result, setResult] = useState<Result | null>(null);
  const [failure, setFailure] = useState<string | null>(null);

  async function run(op: KVOp) {
    const k = key.trim();
    if (!k) return;
    setBusy(op);
    setFailure(null);
    const downAtStart = someNodeDown;
    try {
      const res = await kv(op, k, value);
      setResult({ op, key: k, res });
      if (op === "PUT" && res.ok && downAtStart) onWroteWhileDown();
    } catch (e) {
      setFailure(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(null);
    }
  }

  const submit = (e: FormEvent) => {
    e.preventDefault();
    void run("PUT");
  };

  const body = result && typeof result.res.body === "object" ? result.res.body : null;
  const o = result ? outcome(result) : null;

  return (
    <Panel title="Keyspace" testId="kv-panel" aside={<span className="legend">via the leader</span>}>
      <form onSubmit={submit} className="grid gap-3" aria-label="Key-value operation">
        <div className="grid grid-cols-2 gap-3">
          <Field label="Key">
            <TextInput value={key} onChange={(e) => setKey(e.target.value)} className="font-mono text-[13px]" data-testid="kv-key" required />
          </Field>
          <Field label="Value">
            <TextInput value={value} onChange={(e) => setValue(e.target.value)} className="font-mono text-[13px]" data-testid="kv-value" />
          </Field>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <Button type="submit" variant="primary" data-testid="kv-put" disabled={busy !== null} busy={busy === "PUT"} busyLabel="Writing…">Put</Button>
          <Button data-testid="kv-get" disabled={busy !== null} onClick={() => void run("GET")}>Get</Button>
          <Button
            data-testid="kv-get-stale"
            disabled={busy !== null}
            onClick={() => void run("GET_STALE")}
            title="Read one replica's local copy without going through consensus"
          >
            Get stale
          </Button>
          <span aria-hidden="true" className="mx-1 h-6 w-px bg-seam" />
          <Button variant="destructive" data-testid="kv-delete" disabled={busy !== null} onClick={() => void run("DELETE")}>Delete</Button>
        </div>
      </form>

      <div
        role="status"
        aria-live="polite"
        data-testid="kv-result"
        data-status={result?.res.status ?? ""}
        className="mt-3 min-h-[92px] border-t border-seam pt-3"
      >
        {failure ? (
          <p className="m-0 text-[13px] text-nm656">Request failed before reaching the cluster: {failure}</p>
        ) : result && o ? (
          <>
            <p className="m-0 flex flex-wrap items-baseline gap-x-2 text-[14px]">
              <svg width="6" height="12" aria-hidden="true" className="self-center" style={{ stroke: o.tone }}>
                <line x1="1" x2="1" y1="0" y2="12" />
                <line x1="4" x2="4" y1="0" y2="12" />
              </svg>
              <span className="font-medium">
                {OP_LABEL[result.op]} {result.key} → {result.res.status}
              </span>
              <span style={{ color: o.tone }}>{o.word}</span>
              <span className="text-bone-3">· {ms(result.res.ms)} ms</span>
              {body?.servedBy && <span className="text-bone-3">· served by {body.servedBy}</span>}
              {body?.index ? <span className="text-bone-3">· index {body.index}</span> : null}
            </p>
            {o.note && <p className="mt-1.5 mb-0 text-[13px] text-bone-2" data-testid="kv-note">{o.note}</p>}
            <pre className="mt-2 mb-0 max-h-28 overflow-auto font-mono text-[12px] leading-relaxed whitespace-pre-wrap text-bone-2">
              {typeof result.res.body === "string" ? result.res.body : JSON.stringify(result.res.body, null, 2)}
            </pre>
          </>
        ) : (
          <p className="m-0 text-[13px] text-bone-3">Writes return only after a majority has them on disk.</p>
        )}
      </div>
    </Panel>
  );
}
