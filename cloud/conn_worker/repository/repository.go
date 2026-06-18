package repository

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// Repository lets us interact with Redis
type Repository struct {
	rdb *redis.Client
	db  int
}

func NewRepository() *Repository {
	rdb := redis.NewClient(&redis.Options{
		Addr:     "localhost:6379", // Redis host and port
		Password: "",               // No password by default
		DB:       0,                // Default database ID
	})
	return &Repository{
		rdb: rdb,
		db:  0,
	}
}

func (r *Repository) Set(ctx context.Context, key string, value interface{}, timeout time.Duration) error {
	return r.rdb.Set(ctx, key, value, timeout).Err()
}

func (r *Repository) Get(ctx context.Context, key string) (string, error) {
	return r.rdb.Get(ctx, key).Result()
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
