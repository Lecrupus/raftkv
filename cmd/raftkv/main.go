// Command raftkv runs a RaftKV node, an in-process demo cluster, or a
// benchmark client.
//
//	raftkv node    -id n1 -listen :8080 -peers n1=http://n1:8080,n2=http://n2:8080,n3=http://n3:8080 -data /data
//	raftkv cluster -port 8080 -nodes 3
//	raftkv bench   -targets http://localhost:8081,http://localhost:8082 -clients 16 -duration 10s
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/Lecrupus/raftkv/bench"
	"github.com/Lecrupus/raftkv/cluster"
	"github.com/Lecrupus/raftkv/server"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	switch os.Args[1] {
	case "node":
		runNode(os.Args[2:])
	case "cluster":
		runCluster(os.Args[2:])
	case "bench":
		runBench(os.Args[2:])
	default:
		usage()
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: raftkv <node|cluster|bench> [flags]   (raftkv <cmd> -h for flags)")
	os.Exit(2)
}

func runNode(args []string) {
	fs := flag.NewFlagSet("node", flag.ExitOnError)
	id := fs.String("id", "", "this node's ID (must appear in -peers)")
	listen := fs.String("listen", ":8080", "HTTP listen address")
	peers := fs.String("peers", "", "comma-separated id=url list of ALL members, e.g. n1=http://n1:8080,n2=...")
	data := fs.String("data", "data", "directory for the write-ahead log")
	noSync := fs.Bool("no-fsync", false, "skip fsync on WAL writes (faster, unsafe on power loss)")
	fs.Parse(args)

	peerMap, err := parsePeers(*peers)
	if err != nil || *id == "" {
		log.Fatalf("invalid -id/-peers: %v", err)
	}
	logger := log.New(os.Stdout, "", log.LstdFlags|log.Lmicroseconds)
	node, err := server.NewNode(server.Config{
		ID: *id, Peers: peerMap, DataDir: *data, SyncWrites: !*noSync, Logger: logger,
	})
	if err != nil {
		log.Fatal(err)
	}
	srv := &http.Server{Addr: *listen, Handler: node.Handler(), ReadHeaderTimeout: 5 * time.Second}
	logger.Printf("[%s] listening on %s, peers %v", *id, *listen, peerMap)
	serveUntilSignal(srv, node.Stop)
}

func runCluster(args []string) {
	fs := flag.NewFlagSet("cluster", flag.ExitOnError)
	port := fs.String("port", envOr("PORT", "8080"), "public port for the dashboard (env PORT)")
	nodes := fs.Int("nodes", 3, "number of nodes (3 or 5 recommended)")
	basePort := fs.Int("base-port", 9001, "first loopback port for the nodes")
	data := fs.String("data", filepath.Join(os.TempDir(), "raftkv-demo"), "data root for node WALs")
	keep := fs.Bool("keep-data", false, "keep WALs from a previous run instead of starting fresh")
	quiet := fs.Bool("quiet", false, "don't log Raft events to stdout")
	fs.Parse(args)

	if !*keep {
		cluster.ResetData(*data)
	}
	var logger *log.Logger
	if !*quiet {
		logger = log.New(os.Stdout, "", log.LstdFlags|log.Lmicroseconds)
	}
	m := cluster.NewManager(*nodes, *basePort, *data, true, logger)
	if err := m.StartAll(); err != nil {
		log.Fatal(err)
	}
	srv := &http.Server{Addr: ":" + *port, Handler: m.Handler(), ReadHeaderTimeout: 5 * time.Second}
	log.Printf("RaftKV demo cluster: %d nodes, dashboard on http://localhost:%s", *nodes, *port)
	serveUntilSignal(srv, func() { m.Shutdown(context.Background()) })
}

func runBench(args []string) {
	fs := flag.NewFlagSet("bench", flag.ExitOnError)
	targets := fs.String("targets", "http://localhost:8081,http://localhost:8082,http://localhost:8083", "comma-separated node URLs")
	clients := fs.Int("clients", 16, "concurrent closed-loop clients")
	duration := fs.Duration("duration", 10*time.Second, "test duration")
	reads := fs.Float64("reads", 0, "fraction of ops that are linearizable reads (0..1)")
	size := fs.Int("value-size", 64, "value size in bytes")
	keys := fs.Int("keys", 1000, "key space size")
	fs.Parse(args)

	cfg := bench.Config{
		Targets: strings.Split(*targets, ","), Clients: *clients, Duration: *duration,
		ReadRatio: *reads, ValueSize: *size, Keys: *keys,
	}
	fmt.Printf("benchmarking %v with %d clients for %v (reads=%.0f%%, value=%dB)\n", cfg.Targets, cfg.Clients, cfg.Duration, *reads*100, *size)
	fmt.Println(bench.Run(context.Background(), cfg))
}

func serveUntilSignal(srv *http.Server, cleanup func()) {
	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	select {
	case err := <-errCh:
		log.Fatal(err)
	case <-sig:
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	srv.Shutdown(ctx)
	cleanup()
}

func parsePeers(s string) (map[string]string, error) {
	m := map[string]string{}
	for _, part := range strings.Split(s, ",") {
		id, url, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok || id == "" || url == "" {
			return nil, fmt.Errorf("bad peer %q (want id=url)", part)
		}
		m[id] = strings.TrimRight(url, "/")
	}
	return m, nil
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
