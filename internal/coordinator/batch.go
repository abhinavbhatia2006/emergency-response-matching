package coordinator

import (
	"context"
	"encoding/json"
	"math"

	"github.com/abhinavbhatia2006/emergency-response-matching/internal/storage"
)

// Batch persists an atomic journal transaction and then replicates each touched
// shard. A missing shard acknowledgement means the committed outcome is
// uncertain to the caller; registration repairs that shard from the journal.
func (c *Coordinator) Batch(ctx context.Context, mutations []storage.Mutation) ([]storage.Record, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := storage.ValidateMutations(mutations); err != nil {
		return nil, err
	}
	normalized := append([]storage.Mutation(nil), mutations...)
	touched := [3]bool{}
	var nodes [3][]*NodeState
	for i, m := range normalized {
		if m.Expected == math.MaxInt64 {
			return nil, storage.ErrConflict
		}
		shard, _ := storage.Shard(m.Key, 3)
		touched[shard] = true
		if !m.Delete {
			value, err := c.journal.NormalizeJSON(m.Value, maxBody)
			if err != nil {
				return nil, err
			}
			normalized[i].Value = value
		} else {
			normalized[i].Value = nil
		}
	}
	for shard, yes := range touched {
		if yes {
			nodes[shard] = c.candidates(shard)
			if len(nodes[shard]) == 0 {
				return nil, ErrUnavailable
			}
		}
	}
	all, err := c.journal.Records()
	if err != nil {
		return nil, err
	}
	replacements := make(map[string]storage.Record, len(normalized))
	for _, m := range normalized {
		replacements[m.Key] = storage.Record{Key: m.Key, Value: m.Value, Version: m.Expected + 1, Deleted: m.Delete}
	}
	sets := [3][]storage.Record{}
	for _, record := range all {
		shard, _ := storage.Shard(record.Key, 3)
		if !touched[shard] {
			continue
		}
		if replacement, ok := replacements[record.Key]; ok {
			record = replacement
			delete(replacements, record.Key)
		}
		sets[shard] = append(sets[shard], record)
	}
	for _, m := range normalized {
		if record, ok := replacements[m.Key]; ok {
			shard, _ := storage.Shard(m.Key, 3)
			sets[shard] = append(sets[shard], record)
		}
	}
	var envelopes [3][]byte
	for shard, yes := range touched {
		if yes {
			data, err := json.Marshal(sets[shard])
			if err != nil || len(data) > maxBody {
				return nil, storage.ErrInvalid
			}
			envelopes[shard] = data
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	records, err := c.journal.Apply(normalized)
	if err != nil {
		return nil, err
	}
	// Invalidate every old copy before any replication attempt. Even an expired
	// lease cannot become usable again without reconciliation after this commit.
	for _, node := range c.nodes {
		if touched[node.Shard] {
			node.Ready = false
		}
	}
	unavailable := false
	for shard, yes := range touched {
		if !yes {
			continue
		}
		acks := 0
		for _, node := range nodes[shard] {
			if err := c.syncNode(ctx, node, envelopes[shard]); err == nil {
				acks++
			}
		}
		if acks == 0 {
			unavailable = true
		}
	}
	if unavailable {
		return nil, ErrUnavailable
	}
	return records, nil
}
