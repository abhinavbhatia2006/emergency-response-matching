// Package coordinator routes versioned records to replicated storage shards.
package coordinator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/abhinavbhatia2006/emergency-response-matching/internal/storage"
)

const maxBody = 8 << 20

var ErrUnavailable = errors.New("storage unavailable; a write may require recovery")

type NodeConfig struct {
	ID    string `json:"id"`
	URL   string `json:"url"`
	Shard int    `json:"shard"`
}
type NodeState struct {
	NodeConfig
	Ready         bool      `json:"ready"`
	LastHeartbeat time.Time `json:"last_heartbeat"`
}

// Coordinator is a single writer. Its persistent journal must not be shared
// with another coordinator process. The mutex orders reads, writes and repair.
type Coordinator struct {
	mu      sync.Mutex
	journal *storage.Store
	nodes   []*NodeState
	token   string
	ttl     time.Duration
	client  *http.Client
	now     func() time.Time
}

func New(journal *storage.Store, nodes []NodeConfig, token string, ttl time.Duration, client *http.Client) (*Coordinator, error) {
	if journal == nil || strings.TrimSpace(token) == "" || strings.ContainsAny(token, "\r\n") || ttl <= 0 || len(nodes) != 6 {
		return nil, fmt.Errorf("%w: journal, token, positive heartbeat timeout and six nodes required", storage.ErrInvalid)
	}
	transport := http.Client{Timeout: 3 * time.Second}
	if client != nil {
		transport = *client
		if transport.Timeout <= 0 {
			transport.Timeout = 3 * time.Second
		}
	}
	transport.CheckRedirect = func(*http.Request, []*http.Request) error { return errors.New("redirects disabled") }
	c := &Coordinator{journal: journal, token: token, ttl: ttl, client: &transport, now: time.Now}
	counts := [3]int{}
	ids := map[string]bool{}
	urls := map[string]bool{}
	for _, n := range nodes {
		parsed, err := url.Parse(n.URL)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") || strings.TrimSpace(n.ID) == "" || n.Shard < 0 || n.Shard >= 3 {
			return nil, fmt.Errorf("%w: invalid node configuration", storage.ErrInvalid)
		}
		n.URL = strings.TrimRight(n.URL, "/")
		if ids[n.ID] || urls[n.URL] {
			return nil, fmt.Errorf("%w: duplicate node ID or URL", storage.ErrInvalid)
		}
		ids[n.ID] = true
		urls[n.URL] = true
		counts[n.Shard]++
		c.nodes = append(c.nodes, &NodeState{NodeConfig: n})
	}
	for _, count := range counts {
		if count != 2 {
			return nil, fmt.Errorf("%w: exactly two nodes per shard required", storage.ErrInvalid)
		}
	}
	sort.Slice(c.nodes, func(i, j int) bool { return c.nodes[i].ID < c.nodes[j].ID })
	return c, nil
}

func (c *Coordinator) ready(n *NodeState) bool {
	return n.Ready && !n.LastHeartbeat.IsZero() && c.now().Sub(n.LastHeartbeat) < c.ttl
}
func (c *Coordinator) Nodes() []NodeState {
	c.mu.Lock()
	defer c.mu.Unlock()
	states := make([]NodeState, 0, len(c.nodes))
	for _, n := range c.nodes {
		s := *n
		s.Ready = c.ready(n)
		states = append(states, s)
	}
	return states
}
func (c *Coordinator) candidates(shard int) []*NodeState {
	var result []*NodeState
	for _, n := range c.nodes {
		if n.Shard == shard && c.ready(n) {
			result = append(result, n)
		}
	}
	return result
}
func (c *Coordinator) snapshot(shard int) []storage.Record {
	records := make([]storage.Record, 0)
	for _, r := range c.journal.Snapshot() {
		index, _ := storage.Shard(r.Key, 3)
		if index == shard {
			records = append(records, r)
		}
	}
	return records
}

func (c *Coordinator) send(ctx context.Context, n *NodeState, method, path string, body []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, n.URL+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxBody || resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("node %s: status %d or oversized response", n.ID, resp.StatusCode)
	}
	return data, nil
}
func (c *Coordinator) syncNode(ctx context.Context, n *NodeState, data []byte) error {
	n.Ready = false
	if _, err := c.send(ctx, n, http.MethodPut, "/internal/snapshot", data); err != nil {
		return fmt.Errorf("%w: node %s", ErrUnavailable, n.ID)
	}
	n.Ready = true
	return nil
}

// Register and heartbeat share reconciliation; a live process alone is not proof
// that its disk contains the latest committed version.
func (c *Coordinator) Register(ctx context.Context, id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, n := range c.nodes {
		if n.ID == id {
			data, err := json.Marshal(c.snapshot(n.Shard))
			if err != nil {
				return err
			}
			if len(data) > maxBody {
				return ErrUnavailable
			}
			if err := c.syncNode(ctx, n, data); err != nil {
				return err
			}
			n.LastHeartbeat = c.now().UTC()
			return nil
		}
	}
	return storage.ErrNotFound
}

func (c *Coordinator) Get(ctx context.Context, key string) (storage.Record, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.get(ctx, key)
}
func sameJSON(a, b json.RawMessage) bool {
	canonical := func(raw json.RawMessage) ([]byte, error) {
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber() // Never round large integer IDs through float64.
		var value any
		if err := decoder.Decode(&value); err != nil {
			return nil, err
		}
		var extra any
		if err := decoder.Decode(&extra); err != io.EOF {
			return nil, storage.ErrInvalid
		}
		return json.Marshal(value)
	}
	x, err := canonical(a)
	if err != nil {
		return false
	}
	y, err := canonical(b)
	return err == nil && bytes.Equal(x, y)
}
func (c *Coordinator) get(ctx context.Context, key string) (storage.Record, error) {
	if err := ctx.Err(); err != nil {
		return storage.Record{}, err
	}
	shard, err := storage.Shard(key, 3)
	if err != nil {
		return storage.Record{}, err
	}
	nodes := c.candidates(shard)
	if len(nodes) == 0 {
		return storage.Record{}, ErrUnavailable
	}
	expected, err := c.journal.Get(key)
	if err != nil {
		return storage.Record{}, err
	}
	for _, n := range nodes {
		data, err := c.send(ctx, n, http.MethodGet, "/internal/data/"+url.PathEscape(key), nil)
		var got storage.Record
		if err == nil && json.Unmarshal(data, &got) == nil && got.Key == expected.Key && got.Version == expected.Version && !got.Deleted && sameJSON(got.Value, expected.Value) {
			return got, nil
		}
		n.Ready = false
	}
	return storage.Record{}, ErrUnavailable
}

func (c *Coordinator) List(ctx context.Context) ([]storage.Record, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for shard := 0; shard < 3; shard++ {
		if len(c.candidates(shard)) == 0 {
			return nil, ErrUnavailable
		}
	}
	result := make([]storage.Record, 0)
	for _, r := range c.journal.Snapshot() {
		if !r.Deleted {
			got, err := c.get(ctx, r.Key)
			if err != nil {
				return nil, err
			}
			result = append(result, got)
		}
	}
	return result, nil
}

func (c *Coordinator) Put(ctx context.Context, key string, value json.RawMessage, expected uint64) (storage.Record, error) {
	return c.mutate(ctx, key, value, expected, false)
}
func (c *Coordinator) Delete(ctx context.Context, key string, expected uint64) (storage.Record, error) {
	return c.mutate(ctx, key, nil, expected, true)
}
func (c *Coordinator) mutate(ctx context.Context, key string, value json.RawMessage, expected uint64, deleted bool) (storage.Record, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return storage.Record{}, err
	}
	shard, err := storage.Shard(key, 3)
	if err != nil {
		return storage.Record{}, err
	}
	if !deleted && !json.Valid(value) {
		return storage.Record{}, storage.ErrInvalid
	}
	nodes := c.candidates(shard)
	if len(nodes) == 0 {
		return storage.Record{}, ErrUnavailable
	}
	// Reject oversized snapshots before persisting a journal change.
	candidate := storage.Record{Key: key, Value: value, Version: expected + 1, Deleted: deleted}
	snapshot := c.snapshot(shard)
	found := false
	for i, r := range snapshot {
		if r.Key == key {
			snapshot[i] = candidate
			found = true
			break
		}
	}
	if !found {
		snapshot = append(snapshot, candidate)
	}
	data, err := json.Marshal(snapshot)
	if err != nil || len(data) > maxBody {
		return storage.Record{}, fmt.Errorf("%w: shard snapshot exceeds limit", storage.ErrInvalid)
	}
	var record storage.Record
	if deleted {
		record, err = c.journal.Delete(key, expected)
	} else {
		record, err = c.journal.Put(key, value, expected)
	}
	if err != nil {
		return storage.Record{}, err
	}
	// Candidate is now the journal version, including versioned tombstones.
	acknowledgements := 0
	for _, n := range nodes {
		if err := c.syncNode(ctx, n, data); err == nil {
			acknowledgements++
		}
	}
	if acknowledgements == 0 {
		return storage.Record{}, ErrUnavailable
	}
	return record, nil
}
