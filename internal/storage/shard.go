package storage

import "hash/fnv"

// Shard partitions the complete namespaced key. The count is fixed for the
// lifetime of a dataset; changing it requires explicit data migration.
func Shard(key string, count int) (int, error) {
	if !validKey(key) || count <= 0 {
		return 0, ErrInvalid
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	return int(uint64(h.Sum32()) % uint64(count)), nil
}
