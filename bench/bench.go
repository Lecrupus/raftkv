// Package bench is a closed-loop load generator for RaftKV's HTTP API.
package bench

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

type Config struct {
	Targets   []string // node base URLs; requests are spread across them
	Clients   int
	Duration  time.Duration
	MaxOps    int     // stop after this many ops (0 = duration only)
	ReadRatio float64 // fraction of operations that are linearizable GETs
	ValueSize int
	Keys      int
}

type Result struct {
	Ops        int     `json:"ops"`
	Errors     int     `json:"errors"`
	Seconds    float64 `json:"seconds"`
	Throughput float64 `json:"opsPerSec"`
	P50ms      float64 `json:"p50ms"`
	P95ms      float64 `json:"p95ms"`
	P99ms      float64 `json:"p99ms"`
	MaxMs      float64 `json:"maxMs"`
	Clients    int     `json:"clients"`
	ReadRatio  float64 `json:"readRatio"`
}

func (r Result) String() string {
	return fmt.Sprintf("%d ops in %.1fs  →  %.0f ops/s  |  latency p50 %.2fms  p95 %.2fms  p99 %.2fms  max %.2fms  |  errors %d",
		r.Ops, r.Seconds, r.Throughput, r.P50ms, r.P95ms, r.P99ms, r.MaxMs, r.Errors)
}

func Run(ctx context.Context, cfg Config) Result {
	if cfg.Clients <= 0 {
		cfg.Clients = 8
	}
	if cfg.Keys <= 0 {
		cfg.Keys = 1000
	}
	if cfg.ValueSize <= 0 {
		cfg.ValueSize = 64
	}
	if cfg.Duration <= 0 {
		cfg.Duration = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, cfg.Duration)
	defer cancel()

	client := &http.Client{
		Timeout:   5 * time.Second,
		Transport: &http.Transport{MaxIdleConnsPerHost: cfg.Clients * 2},
	}
	value := strings.Repeat("x", cfg.ValueSize)

	var (
		mu        sync.Mutex
		latencies []time.Duration
		errors    int
		issued    int
	)
	take := func() bool {
		mu.Lock()
		defer mu.Unlock()
		if cfg.MaxOps > 0 && issued >= cfg.MaxOps {
			return false
		}
		issued++
		return true
	}

	start := time.Now()
	var wg sync.WaitGroup
	for c := 0; c < cfg.Clients; c++ {
		wg.Add(1)
		go func(c int) {
			defer wg.Done()
			for i := 0; ctx.Err() == nil && take(); i++ {
				target := cfg.Targets[(c+i)%len(cfg.Targets)]
				key := fmt.Sprintf("bench-%d", (c*7919+i)%cfg.Keys)
				var req *http.Request
				if float64((c*31+i)%100)/100 < cfg.ReadRatio {
					req, _ = http.NewRequestWithContext(ctx, http.MethodGet, target+"/kv/"+key, nil)
				} else {
					req, _ = http.NewRequestWithContext(ctx, http.MethodPut, target+"/kv/"+key, strings.NewReader(value))
				}
				t0 := time.Now()
				resp, err := client.Do(req)
				d := time.Since(t0)
				ok := err == nil && (resp.StatusCode == 200 || resp.StatusCode == 404)
				if resp != nil {
					io.Copy(io.Discard, resp.Body)
					resp.Body.Close()
				}
				if ctx.Err() != nil {
					return // don't count requests cut off by the deadline
				}
				mu.Lock()
				if ok {
					latencies = append(latencies, d)
				} else {
					errors++
				}
				mu.Unlock()
			}
		}(c)
	}
	wg.Wait()
	elapsed := time.Since(start).Seconds()

	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	pct := func(p float64) float64 {
		if len(latencies) == 0 {
			return 0
		}
		return float64(latencies[int(p*float64(len(latencies)-1))].Microseconds()) / 1000
	}
	return Result{
		Ops: len(latencies), Errors: errors, Seconds: elapsed,
		Throughput: float64(len(latencies)) / elapsed,
		P50ms:      pct(0.50), P95ms: pct(0.95), P99ms: pct(0.99), MaxMs: pct(1),
		Clients: cfg.Clients, ReadRatio: cfg.ReadRatio,
	}
}
