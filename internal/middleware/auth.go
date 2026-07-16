package middleware

import (
	"aegis/internal/cache"
	"aegis/internal/database"
	"context"
	"database/sql"
	"errors"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

type ContextKey string
const VirtualKeyCtxKey ContextKey = "virtual_key"

type tokenBucket struct {
	mu         sync.Mutex
	tokens     float64
	lastRefill time.Time
}

func (tb *tokenBucket) allow(limit int, window time.Duration) bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(tb.lastRefill)

	// Refill tokens
	refillRate := float64(limit) / window.Seconds()
	tb.tokens = tb.tokens + (elapsed.Seconds() * refillRate)
	if tb.tokens > float64(limit) {
		tb.tokens = float64(limit)
	}
	tb.lastRefill = now

	// Consume 1 token if available
	if tb.tokens >= 1.0 {
		tb.tokens -= 1.0
		return true
	}
	return false
}

// Thread-safe map to store token buckets: key_hash -> *tokenBucket
var localRateLimiter sync.Map


func AuthMiddleware(db *sql.DB, next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // 1. Get the "Authorization" header
        authHeader := r.Header.Get("Authorization")
        
        // 2. Check if it starts with "Bearer "
        if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
            http.Error(w, "Unauthorized: Missing or invalid token", http.StatusUnauthorized)
            return // Stop execution here!
        }
        
        // 3. Extract the virtual key
        virtualKey := strings.TrimPrefix(authHeader, "Bearer ")
        
        // 4. Validate key with database
        err := database.ValidateKey(db, virtualKey)
        if err != nil {
            if errors.Is(err, database.ErrInvalidKey){
                http.Error(w, "Unauthorized: Invalid API key", http.StatusUnauthorized)
                return
                }
                if errors.Is(err, database.ErrOutOfBudget){
                    http.Error(w, "Payment Required: Budget limit exceeded", http.StatusPaymentRequired)
                    return
                }
                log.Printf("Auth middleware database error: %v", err)
                http.Error(w, "Internal Server Error", http.StatusInternalServerError)
                return
        }
        
        ctx := context.WithValue(r.Context(), VirtualKeyCtxKey, virtualKey)
        // 5. If valid, call the next handler in the chain
        next.ServeHTTP(w, r.WithContext(ctx))
    })
}

func RateLimitMiddleware(rdb *redis.Client, limit int, window time.Duration) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler{
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request){
            // virtual key from request context
            virtualKey, ok:= r.Context().Value(VirtualKeyCtxKey).(string)
            if !ok{
                http.Error(w, "unauthorized: Context missing virtual key", http.StatusUnauthorized)
                return
            }
            // unique rate limit key
            redisKey:= "rate_limit:"+virtualKey

            // Ask redis if this request is allowed
            allowed,err := cache.AllowRequest(r.Context(),rdb,redisKey, limit, window)
            if err!=nil{
                // --- REDIS FALLBACK LAYER ---
			log.Printf("[WARNING] Redis is unreachable: %v. Falling back to local in-memory rate limiting.", err)
			// Get or initialize the token bucket for this virtual key
			val, _ := localRateLimiter.LoadOrStore(virtualKey, &tokenBucket{
				tokens:     float64(limit),
				lastRefill: time.Now(),
			})
			bucket := val.(*tokenBucket)
			// Check local rate limit
			if !bucket.allow(limit, window) {
				http.Error(w, "Too Many Requests: Rate Limit Exceeded (Local Fallback)", http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
			return
            }
            if !allowed{
                http.Error(w, "Too Many Requests: Rate Limit Exceeded", http.StatusTooManyRequests)
                return
            }
            next.ServeHTTP(w,r)
        })
    }
}
