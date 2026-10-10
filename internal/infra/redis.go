package infra

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
)

func NewRedis(redisURL string) (*redis.Client, error) {
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse Redis URL: %w", err)
	}

	client := redis.NewClient(opts)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		// Return the client even when the initial ping fails: go-redis reconnects internally.
		// The caller (rate limiter) fails open until Redis recovers, then works again with no process restart.
		return client, fmt.Errorf("failed to ping Redis: %w", err)
	}

	log.Println("Redis connected")
	return client, nil
}

func CloseRedis(client *redis.Client) {
	if err := client.Close(); err != nil {
		log.Printf("close redis failed: %v", err)
		return
	}
	log.Println("Redis connection closed")
}
