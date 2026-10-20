# RaftKV dashboard

Vite + React 19 + strict TypeScript + Tailwind v4. Builds to **one self-contained
`../cluster/web/index.html`** (JS, CSS and fonts inlined), which the Go binary
embeds with `go:embed`. The built file is committed, so `go build` never needs
Node; CI fails if it is out of date with this source.

```bash
npm ci
npm run dev        # http://localhost:5173, proxies /api to a cluster on :8080 (raftkv cluster -port 8080)
npm run build      # typecheck + write ../cluster/web/index.html
npm run test:e2e   # Playwright against a real 3-node cluster it starts itself
```

## Layout

```
src/
  api/types.ts, client.ts     wire types mirroring the Go JSON tags; typed fetch helpers
  hooks/useCluster.ts         polls /api/cluster, keeps client-side history for the spectrum
  hooks/useGuide.ts           "Try this" steps that check themselves off from real events
  components/Spectrum.tsx     the signature: node lanes + event emission lines (SVG, rAF-scrolled)
  components/NodeStrip.tsx    per-node controls aligned to its lane
  components/Readouts.tsx     leader, term, quorum, commit, failover
  components/{Keyspace,Load}Panel.tsx, Journal.tsx, Rail.tsx, primitives.tsx
tests/                        Playwright specs + page object
```

## Testability contract

The DOM is built for automation first and styled second:

- **Every interactive or asserted element has a `data-testid`**, named by
  role and identity, never by appearance: `node-strip-n2`, `node-n2-power`,
  `node-n2-network`, `kv-put`, `kv-result`, `readout-failover-value`,
  `guide-step-kill-leader`, `journal-row`, `spectrum-event`.
- **State is exposed as data attributes**, so tests assert behaviour, not
  colour or copy: `data-state="leader|follower|candidate|partition|down"` on
  node strips and spectrum lanes, `data-kind` on events and journal rows,
  `data-status` (HTTP code) on the key-value result, `data-done` on guide
  steps, `data-state="running|done|error"` on the load-test result.
- **Semantic HTML and ARIA carry meaning**: one `h1`, labelled `section`s,
  `role="status"` / `aria-live` regions for results and leadership changes,
  `aria-pressed` on filter toggles, `aria-current="step"` on the next guide
  step. Locators like `getByRole` work as well as test ids.
- **No `alert()` or modal dialogs**: errors render inline, so a test never has
  to handle a dialog event.
- **Tests share one real cluster** and run serially; each restores every node
  in `afterEach` through the control API (`healCluster`).

Run one spec headed while debugging:

```bash
npx playwright test tests/failover.spec.ts --headed --project=desktop
```
