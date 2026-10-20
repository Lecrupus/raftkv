---
name: RaftKV Live Cluster
description: A Raft cluster read like a spectrogram — state as line form, colour only as hairlines.
colors:
  ground: "#0d0e0f"
  continuum: "#111214"
  plate: "#16171a"
  seam: "#26282c"
  seam-strong: "#3a3d42"
  bone: "#e6e1d5"
  bone-2: "#b0ab9e"
  bone-3: "#8d897d"
  nm405-stale: "#b993ff"
  nm436-partition: "#8b96ff"
  nm486-follower: "#4fc4e8"
  nm546-commit: "#92dc6e"
  nm589-leader: "#ffc94a"
  nm615-election: "#ff9d50"
  nm656-down: "#ff6460"
  light-ground: "#e8e7e2"
  light-plate: "#f3f2ee"
  light-bone: "#16171a"
typography:
  readout:
    fontFamily: "Barlow Semi Condensed, ui-sans-serif, system-ui, sans-serif"
    fontSize: "34px"
    fontWeight: 300
    lineHeight: "38px"
    letterSpacing: "-0.01em"
  title:
    fontFamily: "Barlow Semi Condensed, ui-sans-serif, system-ui, sans-serif"
    fontSize: "26px"
    fontWeight: 500
    lineHeight: 1.1
  body:
    fontFamily: "Barlow Semi Condensed, ui-sans-serif, system-ui, sans-serif"
    fontSize: "15px"
    fontWeight: 400
    lineHeight: 1.45
  data:
    fontFamily: "Barlow Semi Condensed, ui-sans-serif, system-ui, sans-serif"
    fontSize: "13px"
    fontWeight: 400
    lineHeight: 1.4
  legend:
    fontFamily: "Barlow Semi Condensed, ui-sans-serif, system-ui, sans-serif"
    fontSize: "11px"
    fontWeight: 500
    lineHeight: "16px"
    letterSpacing: "0.14em"
  code:
    fontFamily: "JetBrains Mono, ui-monospace, monospace"
    fontSize: "12px"
    fontWeight: 400
    lineHeight: 1.6
rounded:
  control: "3px"
spacing:
  xs: "4px"
  sm: "8px"
  md: "12px"
  lg: "16px"
  xl: "24px"
  lane: "72px"
components:
  button-primary:
    backgroundColor: "{colors.bone}"
    textColor: "{colors.ground}"
    rounded: "{rounded.control}"
    height: "36px"
    padding: "0 14px"
  button-secondary:
    backgroundColor: "transparent"
    textColor: "{colors.bone}"
    rounded: "{rounded.control}"
    height: "36px"
    padding: "0 14px"
  button-destructive:
    backgroundColor: "transparent"
    textColor: "{colors.nm656-down}"
    rounded: "{rounded.control}"
    height: "32px"
    padding: "0 12px"
  input:
    backgroundColor: "{colors.ground}"
    textColor: "{colors.bone}"
    rounded: "{rounded.control}"
    height: "36px"
    padding: "0 10px"
  panel:
    backgroundColor: "{colors.plate}"
    textColor: "{colors.bone}"
    padding: "12px 16px 16px"
---

# Design System: RaftKV Live Cluster

## Overview

**Creative North Star: "The spectral rail."**

The cluster is read the way a spectrogram is read: a charcoal continuum crossed by needle-thin bright lines. Each node is a horizontal lane on one shared time rail whose right edge is *now*; every Raft event lands as a vertical emission line; node state is drawn as line **form**, never as fill. The surface is an operator console for a reviewer with two minutes: the action (Kill), its consequence (an election line landing) and the measurement (failover ms) share one viewport.

Light mode is the plate negative: bone ground, graphite lines, the same seven wavelengths darkened to hold 4.5:1.

**Key Characteristics:**
- Colour exists only as 1–1.5px hairlines and as text ink, never as a filled surface.
- State is line form: doubled = leader, solid = follower, dashed = candidate, dotted = partitioned, struck hatch = down.
- One semi-condensed grotesque; hierarchy by size, weight and tracking, not by a second face.
- Seams, not shadows: every division is a 1px rule.

## Colors

Restrained ground, spectral accents. Each wavelength has exactly one job, everywhere (spectrum, glyphs, journal, readouts).

### Primary
- **Sodium 589** (`nm589-leader`): the leader, and the single "do this next" marker in the guide. Doubled wherever it appears.

### Secondary
- **H-beta 486** (`nm486-follower`) followers · **Mercury 546** (`nm546-commit`) commits and OK results · **615** (`nm615-election`) elections and candidates · **H-alpha 656** (`nm656-down`) down nodes, errors, destructive ink · **436** (`nm436-partition`) partitions · **405** (`nm405-stale`) stale reads.

### Neutral
- **Ground / Continuum / Plate** (`ground`, `continuum`, `plate`): page, spectrum field, panels — three steps of charcoal.
- **Bone 1–3** (`bone`, `bone-2`, `bone-3`): text; `bone-3` is the floor for legends and still clears 4.5:1 on `plate`.
- **Seam / Seam-strong**: 1px rules and control borders.

### Named Rules
**The One Wavelength, One Job Rule.** A wavelength never decorates. If a colour appears, it names a Raft state or an outcome.
**The Doubled Line Rule.** Doubling outranks everything: only the leader (and the next guide step) is ever drawn doubled.

## Typography

**Display/Body Font:** Barlow Semi Condensed (300–600, self-hosted Latin woff2)
**Code Font:** JetBrains Mono (key/value payloads only)

### Hierarchy
- **Readout** (300, 34px/38px, tabular): the five headline numbers.
- **Title** (500, 26px): the one h1 in the rail.
- **Body** (400, 15px/1.45): prose, controls.
- **Data** (400, 13px): node strip detail, journal rows, results.
- **Legend** (500, 11px, +0.14em, uppercase): every label, axis and panel heading.

### Named Rules
**The Legend Voice Rule.** Labels are always the legend style; never invent a second label size.

## Layout

≥1024px: a 300px sticky rail (thesis + self-checking guide) and one working column: readouts band → spectrum with node strips (lanes and strips share a 72px row so they align) → keyspace | load test | journal. Below 1024px the rail stacks above with the guide folded, and the colophon moves to the page end so readouts reach the first screen. Narrow screens show a shorter time window, never smaller type.

## Elevation & Depth

Flat. Depth is three charcoal steps plus 1px seams; there are no shadows anywhere.

## Shapes

Controls have a 3px radius; everything else is square. Lines are 1–1.5px.

## Components

### Buttons
- **Primary**: bone fill, ground ink — the one filled control per panel (Put, Run load test, Start).
- **Secondary**: transparent, `seam-strong` border, bone ink.
- **Destructive**: transparent outline with 656 ink; border turns 656 only on hover/focus. Always separated from neighbours by a 1px seam spacer.
- States: hover lightens border, active nudges 1px, disabled 45% opacity, busy shows a verb ("Writing…").

### Inputs / Fields
Ground-coloured wells inside plates, `seam-strong` border, sodium focus border. Label above in the legend voice.

### Navigation
Single page; the journal filter is a three-way pressed-button group.

### Spectrum (signature)
SVG lanes over a deterministic banded continuum; one rAF loop slides the content group so the rail scrolls at 60fps without React re-renders. Commit progress rises from each lane floor as mercury ticks; a new leader's line lands with one damped overshoot (`--ease-land`).

## Do's and Don'ts

### Do:
- **Do** print every state as text beside its line form (the glyph is decorative).
- **Do** show divergence (lag, uncommitted tail) rather than totals that are equal on every node.
- **Do** label a timed-out write "not confirmed" — its outcome is unknown, not failed.

### Don't:
- **Don't** fill a surface with a wavelength colour.
- **Don't** add shadows, gradients or rounded cards.
- **Don't** use `alert()` or modals for control-API errors; show them inline in a status region.
