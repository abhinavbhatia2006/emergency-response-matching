package application

import (
	"context"
	"net/http"
	"sort"
	"strings"
	"time"
)

type developmentNode struct {
	ID        string     `json:"id"`
	Ready     bool       `json:"ready"`
	Heartbeat *time.Time `json:"last_heartbeat"`
}
type developmentShard struct {
	ID     int                     `json:"id"`
	Nodes  []developmentNode       `json:"nodes"`
	Counts *developmentShardCounts `json:"counts"`
}

type developmentShardCounts struct {
	Requests  *int `json:"requests"`
	Resources *int `json:"resources"`
}

func (s *Service) shardDetails(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	var nodes []struct {
		ID        string     `json:"id"`
		Shard     *int       `json:"shard"`
		Ready     *bool      `json:"ready"`
		Heartbeat *time.Time `json:"last_heartbeat"`
	}
	if err := s.call(ctx, http.MethodGet, "/internal/nodes", nil, &nodes); err != nil || len(nodes) != 6 {
		failure(w, ErrUnavailable)
		return
	}
	shards := []developmentShard{{ID: 0, Nodes: []developmentNode{}}, {ID: 1, Nodes: []developmentNode{}}, {ID: 2, Nodes: []developmentNode{}}}
	seen := map[string]bool{}
	for _, node := range nodes {
		if strings.TrimSpace(node.ID) == "" || len(node.ID) > 200 || seen[node.ID] || node.Shard == nil || *node.Shard < 0 || *node.Shard > 2 || node.Ready == nil || node.Heartbeat == nil {
			failure(w, ErrUnavailable)
			return
		}
		seen[node.ID] = true
		heartbeat := node.Heartbeat
		if heartbeat.IsZero() {
			heartbeat = nil
		}
		shards[*node.Shard].Nodes = append(shards[*node.Shard].Nodes, developmentNode{node.ID, *node.Ready, heartbeat})
	}
	for i := range shards {
		if len(shards[i].Nodes) != 2 {
			failure(w, ErrUnavailable)
			return
		}
		sort.Slice(shards[i].Nodes, func(a, b int) bool { return shards[i].Nodes[a].ID < shards[i].Nodes[b].ID })
	}
	var counts []developmentShardCounts
	if err := s.call(ctx, http.MethodGet, "/internal/shard-counts", nil, &counts); err == nil && len(counts) == len(shards) {
		for i, count := range counts {
			if count.Requests != nil && count.Resources != nil && *count.Requests >= 0 && *count.Resources >= 0 {
				shards[i].Counts = &counts[i]
			}
		}
	}
	respond(w, 200, struct {
		Shards []developmentShard `json:"shards"`
	}{shards})
}
