package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/ambientlabscomputing/underleaf_v2/shared/utils"
)

// Repository lets us interact with Redis
type Repository struct {
	rdb *redis.Client
	db  int
}

func NewRepository(config utils.Config) *Repository {
	rdb := redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%d", config.Redis.Host, config.Redis.Port),
		Password: config.Redis.Password,
		DB:       config.Redis.DB,
	})
	return &Repository{
		rdb: rdb,
		db:  config.Redis.DB,
	}
}

func (r *Repository) Set(ctx context.Context, key string, value interface{}, timeout time.Duration) error {
	return r.rdb.Set(ctx, key, value, timeout).Err()
}

type RepositoryResult string

func (rr RepositoryResult) Parse(v any) error {
	return json.Unmarshal([]byte(rr), v)
}

func (r *Repository) Get(ctx context.Context, key string) (RepositoryResult, error) {
	val, err := r.rdb.Get(ctx, key).Result()
	return RepositoryResult(val), err
}

func (r *Repository) Delete(ctx context.Context, key string) error {
	return r.rdb.Del(ctx, key).Err()
}

// KeyIterator is a lazy iterator over Redis keys returned by SCAN.
// Drive iteration with Next, read each key with Val, and check Err after the loop.
type KeyIterator struct {
	iter *redis.ScanIterator
}

func (i *KeyIterator) Next(ctx context.Context) bool { return i.iter.Next(ctx) }
func (i *KeyIterator) Val() string                   { return i.iter.Val() }
func (i *KeyIterator) Err() error                    { return i.iter.Err() }

// Scan returns a lazy KeyIterator over keys matching pattern.
// No keys are fetched until the caller calls Next.
func (r *Repository) Scan(ctx context.Context, pattern string) *KeyIterator {
	return &KeyIterator{iter: r.rdb.Scan(ctx, 0, pattern, 0).Iterator()}
}

// List collects all keys matching pattern into a slice.
// Prefer Scan when streaming results to avoid holding all keys in memory.
func (r *Repository) List(ctx context.Context, pattern string) ([]string, error) {
	var keys []string
	iter := r.Scan(ctx, pattern)
	for iter.Next(ctx) {
		keys = append(keys, iter.Val())
	}
	return keys, iter.Err()
}
