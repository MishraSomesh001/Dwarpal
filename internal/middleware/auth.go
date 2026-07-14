package middleware

import (
	"aegis/internal/database"
	"database/sql"
	"log"
	"net/http"
	"strings"
)

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
        valid, err := database.ValidateKey(db, virtualKey)
        if err != nil {
            log.Printf("Auth middleware database error: %v", err)
            http.Error(w, "Internal Server Error", http.StatusInternalServerError)
            return
        }
        
        if !valid {
            http.Error(w, "Unauthorized: Invalid API key", http.StatusUnauthorized)
            return
        }
        
        // 5. If valid, call the next handler in the chain
        next.ServeHTTP(w, r)
    })
}
