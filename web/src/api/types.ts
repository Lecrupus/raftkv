/**
 * Wire types for the RaftKV control API (cluster/gateway.go).
 * Field names mirror the Go JSON tags exactly; keep them in sync with
 * server.NodeStatus, cluster.View, cluster.Failover, raft.Event and bench.Result.
 */

export type RaftRole = "leader" | "follower" | "candidate";

/** What the UI draws for a node: its Raft role, or why it has none. */
export type NodeState = RaftRole | "down" | "partition";

export interface KVStats {
  keys: number;
  appliedIndex: number;
  dedupHits: number;
}

export interface NodeStatus {
  id: string;
  role: RaftRole;
  term: number;
  leader: string;
  votedFor: string;
  commitIndex: number;
  lastApplied: number;
  lastLogIndex: number;
  lastLogTerm: number;
  elections: number;
  termsAsLeader: number;
  kv: KVStats;
  isolated: boolean;
  uptime: string;
  rpcs: number;
}

export interface NodeView {
  id: string;
  up: boolean;
  address: string;
  /** Absent while the node is down. */
  status?: NodeStatus;
}

/** Raft and cluster-manager event kinds. Open-ended on the server, so unknown kinds still type-check. */
export type EventKind =
  | "leader" | "follower" | "election" | "stepdown"
  | "killed" | "started" | "isolated" | "healed"
  | (string & {});

export interface RaftEvent {
  time: string;
  node: string;
  kind: EventKind;
  term?: number;
  detail?: string;
}

export interface Failover {
  oldLeader: string;
  newLeader: string;
  term: number;
  ms: number;
  /** "leader killed" | "leader partitioned" | "quorum restored" */
  cause: string;
  at: string;
}

export interface ClusterView {
  nodes: NodeView[];
  /** Empty string while no leader is known. */
  leader: string;
  events: RaftEvent[] | null;
  failovers: Failover[] | null;
  now: string;
}

export type NodeAction = "kill" | "start" | "isolate" | "heal";

export interface BenchResult {
  ops: number;
  errors: number;
  seconds: number;
  opsPerSec: number;
  p50ms: number;
  p95ms: number;
  p99ms: number;
  maxMs: number;
  clients: number;
  readRatio: number;
}

export interface BenchParams {
  ops: number;
  clients: number;
  reads: number;
}

/** Body of /api/kv/{key} responses (server.kvResponse). */
export interface KVResponse {
  key: string;
  value?: string;
  found: boolean;
  index?: number;
  servedBy: string;
  leader?: string;
  stale?: boolean;
  error?: string;
}

export type KVOp = "PUT" | "GET" | "GET_STALE" | "DELETE";

/** Derive the drawn state of a node. Down beats partitioned beats role. */
export function stateOf(n: NodeView): NodeState {
  if (!n.up || !n.status) return "down";
  if (n.status.isolated) return "partition";
  return n.status.role;
}
