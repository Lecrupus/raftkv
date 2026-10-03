package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/Lecrupus/raftkv/raft"
)

var errIsolated = errors.New("node is isolated (simulated partition)")

// HTTPTransport sends Raft RPCs as JSON over HTTP with keep-alive connections.
type HTTPTransport struct {
	peers    map[string]string // id -> base URL
	client   *http.Client
	isolated *atomic.Bool
	rpcs     atomic.Uint64
	failures atomic.Uint64
}

func NewHTTPTransport(peers map[string]string, isolated *atomic.Bool) *HTTPTransport {
	return &HTTPTransport{
		peers:    peers,
		isolated: isolated,
		client: &http.Client{
			Timeout: 2 * time.Second,
			Transport: &http.Transport{
				MaxIdleConnsPerHost: 16,
				IdleConnTimeout:     30 * time.Second,
			},
		},
	}
}

func (t *HTTPTransport) RequestVote(ctx context.Context, peer string, args *raft.RequestVoteArgs) (*raft.RequestVoteReply, error) {
	var reply raft.RequestVoteReply
	if err := t.call(ctx, peer, "/raft/vote", args, &reply); err != nil {
		return nil, err
	}
	return &reply, nil
}

func (t *HTTPTransport) AppendEntries(ctx context.Context, peer string, args *raft.AppendEntriesArgs) (*raft.AppendEntriesReply, error) {
	var reply raft.AppendEntriesReply
	if err := t.call(ctx, peer, "/raft/append", args, &reply); err != nil {
		return nil, err
	}
	return &reply, nil
}

func (t *HTTPTransport) call(ctx context.Context, peer, path string, args, reply any) error {
	t.rpcs.Add(1)
	if t.isolated.Load() {
		t.failures.Add(1)
		return errIsolated
	}
	base, ok := t.peers[peer]
	if !ok {
		return fmt.Errorf("unknown peer %q", peer)
	}
	body, err := json.Marshal(args)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := t.client.Do(req)
	if err != nil {
		t.failures.Add(1)
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.failures.Add(1)
		return fmt.Errorf("%s%s: %s", peer, path, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(reply)
}
