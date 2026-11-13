// Package coordinator routes versioned records to replicated storage shards.
package coordinator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"sort"
	"strconv"
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
func (c *Coordinator) records(shard int) ([]storage.Record, error) {
	records := make([]storage.Record, 0)
	all, err := c.journal.Records()
	if err != nil {
		return nil, err
	}
	for _, r := range all {
		index, _ := storage.Shard(r.Key, 3)
		if index == shard {
			records = append(records, r)
		}
	}
	return records, nil
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
	if _, err := c.send(ctx, n, http.MethodPut, "/internal/records", data); err != nil {
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
			n.Ready = false
			records, err := c.records(n.Shard)
			if err != nil {
				return err
			}
			data, err := json.Marshal(records)
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
	decode := func(raw json.RawMessage) (any, error) {
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber() // Preserve integers beyond float64 precision.
		var value any
		if err := decoder.Decode(&value); err != nil {
			return nil, err
		}
		var extra any
		if err := decoder.Decode(&extra); err != io.EOF {
			return nil, storage.ErrInvalid
		}
		return value, nil
	}
	x, err := decode(a)
	if err != nil {
		return false
	}
	y, err := decode(b)
	return err == nil && equalJSONValue(x, y)
}

func equalJSONValue(a, b any) bool {
	switch x := a.(type) {
	case json.Number:
		y, ok := b.(json.Number)
		if !ok {
			return false
		}
		left, lok := boundedNumber(x)
		right, rok := boundedNumber(y)
		return lok && rok && left.Cmp(right) == 0
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if !equalJSONValue(x[i], y[i]) {
				return false
			}
		}
		return true
	case map[string]any:
		y, ok := b.(map[string]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for key, value := range x {
			other, ok := y[key]
			if !ok || !equalJSONValue(value, other) {
				return false
			}
		}
		return true
	default:
		return a == b
	}
}

// PostgreSQL numeric supports at most 131072 digits before the decimal point
// and 16383 after it. Bound untrusted exponents before allocating a big.Rat.
func boundedNumber(number json.Number) (*big.Rat, bool) {
	raw := string(number)
	if len(raw) > 147460 {
		return nil, false
	}
	if index := strings.IndexAny(raw, "eE"); index >= 0 {
		exponent, err := strconv.ParseInt(raw[index+1:], 10, 32)
		if err != nil || exponent > 131072 || exponent < -131072 {
			return nil, false
		}
	}
	return new(big.Rat).SetString(raw)
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
	all, err := c.journal.Records()
	if err != nil {
		return nil, err
	}
	for _, r := range all {
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
	// Size the database-normalized value before committing any journal change.
	if !deleted {
		value, err = c.journal.NormalizeJSON(value, maxBody)
		if err != nil {
			return storage.Record{}, err
		}
	}
	// Reject oversized record sets before persisting a journal change.
	candidate := storage.Record{Key: key, Value: value, Version: expected + 1, Deleted: deleted}
	recordSet, err := c.records(shard)
	if err != nil {
		return storage.Record{}, err
	}
	found := false
	for i, r := range recordSet {
		if r.Key == key {
			recordSet[i] = candidate
			found = true
			break
		}
	}
	if !found {
		recordSet = append(recordSet, candidate)
	}
	data, err := json.Marshal(recordSet)
	if err != nil || len(data) > maxBody {
		return storage.Record{}, fmt.Errorf("%w: shard record set exceeds limit", storage.ErrInvalid)
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
