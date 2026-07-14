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
	"time"

	"github.com/redis/go-redis/v9"
)

type ContextKey string
const VirtualKeyCtxKey ContextKey = "virtual_key"

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
                //[To Do]: Phase 4 we need to add fallback, 
                // cause we don't want the gateway to crash 
                // if redis goes down for some reason 
                http.Error(w, "Internal Server Error",http.StatusInternalServerError)
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
