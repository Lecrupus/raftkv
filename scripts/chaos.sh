#!/usr/bin/env bash
# Chaos test against the docker-compose cluster.
#
# Each round: SIGKILL a random node (often the leader), keep writing through
# the survivors, restart the node, then verify that EVERY write ever
# acknowledged is still readable, through every node. A single lost
# acknowledged write fails the run.
#
#   docker compose up -d --build
#   ROUNDS=100 ./scripts/chaos.sh
set -uo pipefail

ROUNDS=${ROUNDS:-20}
WRITES=${WRITES:-10}
NODES=(n1 n2 n3)
declare -A PORT=([n1]=8081 [n2]=8082 [n3]=8083)
declare -A ACKED=()   # key -> value for every acknowledged write
failures=0

say() { printf '\033[1m[chaos]\033[0m %s\n' "$*"; }

leader() {
  for n in "${NODES[@]}"; do
    local s
    s=$(curl -fs -m 1 "localhost:${PORT[$n]}/status" 2>/dev/null) || continue
    if [[ $s == *'"role":"leader"'* ]]; then echo "$n"; return; fi
  done
}

wait_healthy() {
  for _ in $(seq 1 50); do
    curl -fs -m 1 "localhost:${PORT[$1]}/healthz" >/dev/null 2>&1 && return 0
    sleep 0.2
  done
  return 1
}

# put KEY VALUE via any live node (they forward to the leader); retries
put() {
  for attempt in $(seq 1 30); do
    for n in "${NODES[@]}"; do
      [[ " ${DOWN:-} " == *" $n "* ]] && continue
      local code
      code=$(curl -s -o /dev/null -w '%{http_code}' -m 3 -X PUT --data "$2" "localhost:${PORT[$n]}/kv/$1")
      if [[ $code == 200 ]]; then ACKED[$1]=$2; return 0; fi
    done
    sleep 0.1
  done
  return 1
}

# get KEY NODE [stale] -> prints value; exit 1 if the key is missing (404).
# Transient errors (node just restarted, election in progress) are retried;
# only a definitive answer counts.
get() {
  local q="" out code
  [[ ${3:-} == stale ]] && q="?consistency=stale"
  for attempt in $(seq 1 30); do
    out=$(mktemp)
    code=$(curl -s -m 3 -o "$out" -w '%{http_code}' "localhost:${PORT[$2]}/kv/$1$q")
    if [[ $code == 200 ]]; then sed -n 's/.*"value":"\([^"]*\)".*/\1/p' "$out"; rm -f "$out"; return 0; fi
    rm -f "$out"
    if [[ $code == 404 ]]; then return 1; fi
    sleep 0.2
  done
  echo "<unavailable>"
}

say "waiting for cluster…"
for n in "${NODES[@]}"; do wait_healthy "$n" || { echo "node $n not healthy; is 'docker compose up' running?"; exit 1; }; done

start=$(date +%s)
for round in $(seq 1 "$ROUNDS"); do
  L=$(leader)
  if (( RANDOM % 2 == 0 )) && [[ -n $L ]]; then victim=$L; else victim=${NODES[$((RANDOM % 3))]}; fi
  docker compose kill -s SIGKILL "$victim" >/dev/null 2>&1
  DOWN=$victim

  ok=0
  for i in $(seq 1 "$WRITES"); do
    put "r${round}-k${i}" "v-${round}-${i}-${RANDOM}" && ok=$((ok + 1))
  done
  newL=$(leader)
  docker compose start "$victim" >/dev/null 2>&1
  DOWN=""
  wait_healthy "$victim" || { say "round $round: $victim failed to restart"; failures=$((failures + 1)); }

  # verify every acknowledged write, reading through each node
  sleep 0.5
  lost=0
  for k in "${!ACKED[@]}"; do
    n=${NODES[$((RANDOM % 3))]}
    got=$(get "$k" "$n")
    if [[ $got != "${ACKED[$k]}" ]]; then
      say "LOST WRITE: $k expected '${ACKED[$k]}' got '$got' (via $n)"
      lost=$((lost + 1))
    fi
  done
  failures=$((failures + lost))
  say "round $round/$ROUNDS: killed $victim (leader was ${L:-none}), new leader ${newL:-?}, $ok/$WRITES writes acknowledged while it was down, ${#ACKED[@]} acked keys verified, lost=$lost"
done

# final check: every replica converged to the same data
sleep 1
for n in "${NODES[@]}"; do
  bad=0
  for k in "${!ACKED[@]}"; do
    got=$(get "$k" "$n" stale)
    [[ $got == "${ACKED[$k]}" ]] || bad=$((bad + 1))
  done
  say "replica $n: $(( ${#ACKED[@]} - bad ))/${#ACKED[@]} keys match"
  failures=$((failures + bad))
done

say "done in $(( $(date +%s) - start ))s: $ROUNDS rounds, ${#ACKED[@]} acknowledged writes, failures=$failures"
(( failures == 0 )) && say "PASS: no acknowledged write was lost" || { say "FAIL"; exit 1; }
