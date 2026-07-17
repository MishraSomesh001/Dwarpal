package api

import (
	"aegis/internal/database"
	"aegis/internal/middleware"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func jsonResponse(w http.ResponseWriter, code int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(data)
}

func jsonError(w http.ResponseWriter, code int, msg string) {
	jsonResponse(w, code, map[string]string{"error": msg})
}

// POST /api/auth/login
func HandleLogin(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type,Authorization")
			w.Header().Set("Access-Control-Allow-Methods", "POST,OPTIONS")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		var req struct {
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		user, err := database.AuthenticateUser(db, req.Email, req.Password)
		if errors.Is(err, database.ErrInvalidCredentials) {
			jsonError(w, http.StatusUnauthorized, "invalid credentials")
			return
		}
		if err != nil {
			jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}

		secret := os.Getenv("JWT_SECRET")
		if secret == "" {
			secret = "aegis-default-secret-change-in-prod"
		}
		claims := &middleware.Claims{
			UserID: user.ID,
			Email:  user.Email,
			Role:   user.Role,
			RegisteredClaims: jwt.RegisteredClaims{
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
				IssuedAt:  jwt.NewNumericDate(time.Now()),
			},
		}
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		signed, err := token.SignedString([]byte(secret))
		if err != nil {
			jsonError(w, http.StatusInternalServerError, "failed to sign token")
			return
		}
		jsonResponse(w, http.StatusOK, map[string]any{
			"token": signed,
			"user": map[string]any{
				"id":    user.ID,
				"email": user.Email,
				"name":  user.Name,
				"role":  user.Role,
			},
		})
	}
}

// GET /api/admin/keys
func HandleAdminGetKeys(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		keys, err := database.GetAllVirtualKeys(db)
		if err != nil {
			jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if keys == nil {
			keys = []database.VirtualKey{}
		}
		jsonResponse(w, http.StatusOK, keys)
	}
}

// POST /api/admin/keys
func HandleAdminCreateKey(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		var req struct {
			UserID    int     `json:"user_id"`
			BudgetUSD float64 `json:"budget_usd"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		rawKey := fmt.Sprintf("vk-%d-%d", req.UserID, time.Now().UnixNano())
		if err := database.CreateVirtualKey(db, 1, req.UserID, rawKey, req.BudgetUSD); err != nil {
			jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
		jsonResponse(w, http.StatusCreated, map[string]string{"key": rawKey, "message": "Virtual key created"})
	}
}

// DELETE /api/admin/keys?id=X
func HandleAdminRevokeKey(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		idStr := r.URL.Query().Get("id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			jsonError(w, http.StatusBadRequest, "invalid key id")
			return
		}
		if err := database.RevokeVirtualKey(db, id); err != nil {
			jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
		jsonResponse(w, http.StatusOK, map[string]string{"message": "Key revoked"})
	}
}

// GET /api/admin/users
func HandleAdminGetUsers(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		users, err := database.GetAllUsers(db)
		if err != nil {
			jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if users == nil {
			users = []database.User{}
		}
		jsonResponse(w, http.StatusOK, users)
	}
}

// POST /api/admin/users
func HandleAdminCreateUser(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		var req struct {
			Email    string `json:"email"`
			Name     string `json:"name"`
			Role     string `json:"role"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		user, err := database.CreateUser(db, 1, req.Email, req.Name, req.Role, req.Password)
		if err != nil {
			jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
		jsonResponse(w, http.StatusCreated, user)
	}
}

// POST /api/admin/providers
func HandleAdminUpsertProvider(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		var req struct {
			Name        string `json:"name"`
			APIKey      string `json:"api_key"`
			EndpointURL string `json:"endpoint_url"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if err := database.UpsertProvider(db, req.Name, req.APIKey, req.EndpointURL); err != nil {
			jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
		jsonResponse(w, http.StatusOK, map[string]string{"message": "Provider updated"})
	}
}

// GET /api/admin/providers
func HandleAdminGetProviders(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		providers, err := database.GetProviders(db)
		if err != nil {
			jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if providers == nil {
			providers = []database.Provider{}
		}
		jsonResponse(w, http.StatusOK, providers)
	}
}

// GET /api/admin/logs
func HandleAdminGetLogs(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		limit := 100
		if l := r.URL.Query().Get("limit"); l != "" {
			if n, err := strconv.Atoi(l); err == nil {
				limit = n
			}
		}
		logs, err := database.GetRequestLogs(db, limit)
		if err != nil {
			jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if logs == nil {
			logs = []database.RequestLog{}
		}
		jsonResponse(w, http.StatusOK, logs)
	}
}

// GET /api/admin/alerts
func HandleAdminGetAlerts(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		alerts, err := database.GetBudgetAlerts(db)
		if err != nil {
			jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if alerts == nil {
			alerts = []database.VirtualKey{}
		}
		jsonResponse(w, http.StatusOK, alerts)
	}
}

// GET /api/employee/dashboard
func HandleEmployeeDashboard(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		claims := middleware.GetClaims(r)
		if claims == nil {
			jsonError(w, http.StatusUnauthorized, "missing claims")
			return
		}
		keys, err := database.GetUserVirtualKeys(db, claims.UserID)
		if err != nil {
			jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if keys == nil {
			keys = []database.VirtualKey{}
		}
		jsonResponse(w, http.StatusOK, map[string]any{
			"user": map[string]any{"id": claims.UserID, "email": claims.Email, "role": claims.Role},
			"keys": keys,
		})
	}
}

// GET /api/employee/logs
func HandleEmployeeLogs(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		claims := middleware.GetClaims(r)
		if claims == nil {
			jsonError(w, http.StatusUnauthorized, "missing claims")
			return
		}
		keys, err := database.GetUserVirtualKeys(db, claims.UserID)
		if err != nil {
			jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
		var keyHashes []string
		for _, k := range keys {
			keyHashes = append(keyHashes, database.HashKey(k.KeyPreview))
		}
		logs, err := database.GetUserRequestLogs(db, keyHashes, 50)
		if err != nil {
			jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if logs == nil {
			logs = []database.RequestLog{}
		}
		jsonResponse(w, http.StatusOK, logs)
	}
}

// CORS preflight handler
func CORSHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type,Authorization")
	w.Header().Set("Access-Control-Allow-Methods", "GET,POST,DELETE,OPTIONS")
	w.WriteHeader(http.StatusNoContent)
}
