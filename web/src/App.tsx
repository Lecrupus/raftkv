import { useState } from "react";
import { ClusterSection } from "./components/ClusterSection";
import { Journal } from "./components/Journal";
import { KeyspacePanel } from "./components/KeyspacePanel";
import { LoadPanel } from "./components/LoadPanel";
import { Colophon, Rail } from "./components/Rail";
import { Readouts } from "./components/Readouts";
import { useCluster } from "./hooks/useCluster";
import { useGuide } from "./hooks/useGuide";

/**
 * RaftKV live console.
 *
 * Layout (≥1024px): a sticky rail on the left (thesis + self-checking guide),
 * and one working column: readouts → spectrum with node strips → keyspace,
 * load test and journal side by side. Cause and effect share one viewport:
 * the Kill button, the election line it causes and the measured failover all
 * sit above the fold at 1280×800.
 */
export function App() {
  const { view, history, clockOffset, error, announcement, refresh } = useCluster();
  const [wroteWhileDown, setWroteWhileDown] = useState(false);
  const { steps, next } = useGuide(view, history, wroteWhileDown);
  const someNodeDown = view?.nodes.some((n) => !n.up) ?? false;

  return (
    <div className="min-h-dvh lg:grid lg:grid-cols-[300px_minmax(0,1fr)]">
      <Rail steps={steps} next={next} />

      <main className="min-w-0" aria-label="Live cluster">
        {/* screen-reader narration of leadership changes */}
        <p aria-live="polite" className="sr-only" data-testid="announcer">{announcement}</p>

        {error && (
          <p role="alert" data-testid="connection-error" className="m-0 border-b border-seam px-4 py-2 text-[13px] text-nm656 lg:px-6">
            Lost contact with the cluster gateway ({error}). Retrying; the figures below are the last ones received.
          </p>
        )}

        {view ? (
          <>
            <Readouts view={view} history={history} stale={error !== null} />
            <ClusterSection view={view} history={history} clockOffset={clockOffset} refresh={refresh} />
            <div className="grid border-t border-seam xl:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_minmax(0,1.15fr)] [&>section]:border-t-0 xl:[&>section+section]:border-l [&>section+section]:border-t">
              <KeyspacePanel someNodeDown={someNodeDown} onWroteWhileDown={() => setWroteWhileDown(true)} />
              <LoadPanel />
              <Journal events={view.events ?? []} />
            </div>
            <Colophon className="grid px-4 pb-8 lg:hidden" />
          </>
        ) : (
          <div className="grid min-h-[60dvh] place-items-center px-6" data-testid="loading">
            <p className="legend">{error ? "Cluster gateway unreachable · retrying" : "Connecting to the cluster…"}</p>
          </div>
        )}
      </main>
    </div>
  );
}
