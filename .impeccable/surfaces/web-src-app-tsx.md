---
version: 1
slug: "web-src-app-tsx"
primary_target: "web/src/App.tsx"
related_targets: ["cluster/web/index.html"]
---

Scope: the live cluster console (single page, served from cluster/web by the Go binary). Mode: Operate.
Audience/job: reviewers with ~2 minutes; kill the leader and see the new one elected, with the failover time, in one viewport.

## Direction contract

THESIS: The cluster is read like a spectrogram. Each node is a horizontal lane on one shared time rail; every Raft event lands as a thin vertical emission line across the lanes; node state is line form, not hue. Refuses the category default: a marketing hero over three identical node cards and a decorative topology triangle.

OWN-WORLD: Charcoal continuum ground with faint procedural banding inside the spectrum field only; bone legends; ash plates for prose. Colour exists only as hairlines at seven wavelengths: sodium 589 = leader (doubled line), H-beta 486 = follower, mercury 546 = commit/ok, 615 = election/candidate, H-alpha 656 = down/error, 436 = partition, 405 = stale read. Line forms: solid full-height = up, dashed = candidate, half-height = partitioned, struck = down, doubled = leader. Light theme = the plate negative: bone ground, graphite lines. One semi-condensed grotesque; legends small, uppercase and tracked; readout numerals light and large.

STORY: The visitor sees who leads and how healthy the quorum is, kills the leader, watches an election line land on the rail with the measured failover ms, writes a key while a node is down, and checks off the guided steps as the cluster proves each one.

FIRST VIEWPORT: Left rail (brand, h1 "Break it and watch it heal.", guided steps that check themselves off from real events). Top band: readouts for leader, term, quorum (majority line), commit index, last failover. Centre: the spectrum: lanes n1..nN over a 60 s rail, each with its control strip on the right (role, lag, uncommitted, Kill/Start isolated from Partition/Heal). Bottom row: keyspace console, load test (p50/p95/p99/max as emission lines on a log-ms rail), event journal. Primary action: Kill on the leader's strip.

FORM: challenger operate-c-emission-line-rail (won on audience identification and product clarity over grounded candidate 7, tide almanac). Raises: from dark developer console, destructive actions isolated by space; from terminal-yellow wayfinding, the accent highlights only the next guided step; from character catalog, down nodes print desaturated. seed:5396a4e8c855eab7

FINISH: unreviewed and undocumented is unfinished; this build ends with the finish review, the verdict, DESIGN.md, and every shipping raster carrying its provenance
