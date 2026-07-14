package cache

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"time"

	"github.com/redis/go-redis/v9"
)

// Embed the Lua script into the binary.
//
//go:embed sliding_window.lua
var slidingWindowScript string

var (
	redisClient *redis.Client
	script      *redis.Script
)

func InitRedis() (*redis.Client, error) {

	host := os.Getenv("REDIS_HOST")
	port := os.Getenv("REDIS_PORT")
	password := os.Getenv("REDIS_PASSWORD")

	redisClient = redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%s", host, port),
		Password: password,
		DB:       0,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := redisClient.Ping(ctx).Err(); err != nil {
		return nil, err
	}

	// Prepare the Lua script.
	script = redis.NewScript(slidingWindowScript)

	return redisClient, nil
}

func AllowRequest(
	ctx context.Context,
	rdb *redis.Client,
	key string,
	limit int,
	window time.Duration,
) (bool, error) {

	now := time.Now().UnixMilli()

	windowMs := window.Milliseconds()

	result, err := script.Run(
		ctx,
		rdb,
		[]string{key},
		now,
		limit,
		windowMs,
	).Int()

	if err != nil {
		return false, err
	}

	return result == 1, nil
}