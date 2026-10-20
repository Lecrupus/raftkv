import type { EventKind, NodeState } from "../api/types";

/** CSS variable for each spectral wavelength. One wavelength, one job. */
export type Wavelength = "nm405" | "nm436" | "nm486" | "nm546" | "nm589" | "nm615" | "nm656";

export const wl = (w: Wavelength) => `var(--${w})`;

export const STATE_WAVELENGTH: Record<NodeState, Wavelength> = {
  leader: "nm589",
  follower: "nm486",
  candidate: "nm615",
  partition: "nm436",
  down: "nm656",
};

export const STATE_LABEL: Record<NodeState, string> = {
  leader: "Leader",
  follower: "Follower",
  candidate: "Candidate",
  partition: "Partitioned",
  down: "Down",
};

export const EVENT_WAVELENGTH: Record<string, Wavelength> = {
  leader: "nm589",
  election: "nm615",
  stepdown: "nm615",
  killed: "nm656",
  started: "nm546",
  isolated: "nm436",
  healed: "nm436",
  follower: "nm486",
};

export const eventWavelength = (k: EventKind): Wavelength => EVENT_WAVELENGTH[k] ?? "nm486";

/** Leadership events vs faults, for the journal filter. */
export const LEADERSHIP_KINDS = new Set<EventKind>(["leader", "election", "stepdown", "follower"]);
export const FAULT_KINDS = new Set<EventKind>(["killed", "started", "isolated", "healed"]);

const pad = (n: number, w = 2) => String(n).padStart(w, "0");

/** 14:03:22.140 in the viewer's local time. */
export function clockTime(t: number | string): string {
  const d = new Date(t);
  return `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}.${pad(d.getMilliseconds(), 3)}`;
}

export const int = (n: number) => Math.round(n).toLocaleString("en-US");

/** Milliseconds with precision that matches the magnitude. */
export function ms(n: number): string {
  if (n < 1) return n.toFixed(2);
  if (n < 10) return n.toFixed(1);
  return int(n);
}
