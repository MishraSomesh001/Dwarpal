package database

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"os"
	"time"

	"golang.org/x/crypto/bcrypt"
	_ "github.com/lib/pq"
)

var (
	ErrInvalidKey = errors.New("Invalid API Key")
	ErrOutOfBudget = errors.New("Budget limit Exceed")
	ErrInvalidCredentials = errors.New("Invalid email or password")
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

// --- Struct Types ---

type User struct {
	ID           int
	TenantID     int
	Email        string
	Name         string
	Role         string // "admin" | "employee"
	PasswordHash string
}

type VirtualKey struct {
	ID        int
	UserID    sql.NullInt64
	UserEmail sql.NullString
	UserName  sql.NullString
	KeyPreview string // First 8 chars of raw key for display
	IsActive  bool
	BudgetUSD float64
	SpendUSD  float64
	ExpiresAt sql.NullTime
}

type RequestLog struct {
	ID               int64
	VirtualKey       string
	Model            string
	Provider         string
	PromptTokens     int
	CompletionTokens int
	CostUSD          float64
	LatencyMS        int
	Status           string
	CreatedAt        time.Time
}

type Provider struct {
	Name        string
	APIKey      string
	EndpointURL string
}

type ModelRoute struct {
	ModelName     string
	Provider      string
	UpstreamURL   string
	Weight        float64
	FallbackModel sql.NullString
}

func GetModelRoute(db *sql.DB, modelName string) (*ModelRoute, error) {
	rows, err := db.Query(
		`SELECT model_name, provider, upstream_url, weight, fallback_model 
		 FROM model_routes 
		 WHERE model_name = $1`,
		modelName,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var routes []ModelRoute
	for rows.Next() {
		var r ModelRoute
		if err := rows.Scan(&r.ModelName, &r.Provider, &r.UpstreamURL, &r.Weight, &r.FallbackModel); err != nil {
			return nil, err
		}
		routes = append(routes, r)
	}

	if len(routes) == 0 {
		return nil, fmt.Errorf("no route found for model: %s", modelName)
	}

	// Dynamic Weighted Selection
	r := rand.Float64()
	log.Printf("[ROUTER] Found %d routes for model '%s'. Rolled: %f", len(routes), modelName, r)

	var cumulativeWeight float64
	for _, route := range routes {
		cumulativeWeight += route.Weight
		if r <= cumulativeWeight {
			return &route, nil
		}
	}

	return &routes[0], nil
}

// Hash imcoming raw key
func HashKey(key string) string {
	hash := sha256.Sum256([]byte(key))
	hexstr:= hex.EncodeToString(hash[:])
	// log.Println(hexstr)
	return hexstr
}
// intialize DB connection 
func InitDB() (*sql.DB, error) {

	host := os.Getenv("DB_HOST")
	port := os.Getenv("DB_PORT")
	user := os.Getenv("DB_USER")
	password := os.Getenv("DB_PASSWORD")
	dbname := os.Getenv("DB_NAME")

	connStr := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		host,
		port,
		user,
		password,
		dbname,
	)

	var db *sql.DB
	var err error
	for i := 0; i < 10; i++ {
		db, err = sql.Open("postgres", connStr)
		if err == nil {
			err = db.Ping()
			if err == nil {
				// Connection successful! Break out of the retry loop.
				break
			}
		}
		
		log.Printf("Database not ready yet (attempt %d/10): %v. Retrying in 2 seconds...", i+1, err)
		time.Sleep(2 * time.Second)
	}
	if err != nil {
		return nil, fmt.Errorf("could not connect to database after 10 retries: %w", err)
	}

	// if err := db.Ping(); err != nil {
	// 	return nil, err
	// }

	// Acquire exclusive advisory lock so only one replica runs schema migration at a time.
	// The other replicas block here (not crash!) until the lock is released.
	// 12345 is our application's unique lock ID.
	if _, err := db.Exec("SELECT pg_advisory_lock(12345)"); err != nil {
		return nil, fmt.Errorf("could not acquire advisory lock: %w", err)
	}
	defer db.Exec("SELECT pg_advisory_unlock(12345)")

	usersTable := `
	CREATE TABLE IF NOT EXISTS users (
		id SERIAL PRIMARY KEY,
		tenant_id INTEGER REFERENCES tenants(id),
		email TEXT NOT NULL UNIQUE,
		name TEXT NOT NULL,
		role TEXT NOT NULL DEFAULT 'employee',
		password_hash TEXT NOT NULL
	);
	`

	tenantTable := `
	CREATE TABLE IF NOT EXISTS tenants (
		id SERIAL PRIMARY KEY,
		name TEXT NOT NULL
	);
	`

	keyTable := `
	CREATE TABLE IF NOT EXISTS virtual_keys (
		id SERIAL PRIMARY KEY,
		tenant_id INTEGER REFERENCES tenants(id),
		user_id INTEGER REFERENCES users(id),
		key_hash TEXT NOT NULL UNIQUE,
		key_preview TEXT NOT NULL DEFAULT '',
		is_active BOOLEAN DEFAULT TRUE,
		expires_at TIMESTAMP,
		budget_usd NUMERIC(10, 4) DEFAULT 10.00,
		spend_usd NUMERIC(10, 4) DEFAULT 0.00
	);
	`
	model_routes := `
	CREATE TABLE IF NOT EXISTS model_routes (
		id SERIAL PRIMARY KEY,
		model_name TEXT NOT NULL,
		provider TEXT NOT NULL,
		upstream_url TEXT NOT NULL,
		weight NUMERIC(3,2) DEFAULT 1.00,
		fallback_model TEXT
	);
	`
	providersTable := `
	CREATE TABLE IF NOT EXISTS providers (
		name TEXT PRIMARY KEY,
		api_key TEXT NOT NULL DEFAULT '',
		endpoint_url TEXT NOT NULL DEFAULT ''
	);
	`
	requestLogsTable := `
	CREATE TABLE IF NOT EXISTS request_logs (
		id BIGSERIAL PRIMARY KEY,
		virtual_key TEXT NOT NULL DEFAULT '',
		model TEXT NOT NULL DEFAULT '',
		provider TEXT NOT NULL DEFAULT '',
		prompt_tokens INT DEFAULT 0,
		completion_tokens INT DEFAULT 0,
		cost_usd NUMERIC(12,8) DEFAULT 0,
		latency_ms INT DEFAULT 0,
		status TEXT NOT NULL DEFAULT 'success',
		created_at TIMESTAMPTZ DEFAULT NOW()
	);
	`

	for _, ddl := range []string{tenantTable, usersTable, keyTable, model_routes, providersTable, requestLogsTable} {
		if _, err := db.Exec(ddl); err != nil {
			return nil, err
		}
	}

	// Seed data using ON CONFLICT DO NOTHING - safe to run from all 3 replicas concurrently
	_, err = db.Exec(`INSERT INTO tenants(name) VALUES('Default Tenant') ON CONFLICT DO NOTHING`)
	if err != nil {
		return nil, err
	}

	var tenantID int
	err = db.QueryRow("SELECT id FROM tenants WHERE name = 'Default Tenant'").Scan(&tenantID)
	if err != nil {
		return nil, err
	}

	// Seed admin user: admin@aegis.dev / admin123
	adminHash, _ := bcrypt.GenerateFromPassword([]byte("admin123"), bcrypt.DefaultCost)
	_, err = db.Exec(
		`INSERT INTO users (tenant_id, email, name, role, password_hash) VALUES($1,$2,$3,$4,$5) ON CONFLICT (email) DO NOTHING`,
		tenantID, "admin@aegis.dev", "Admin", "admin", string(adminHash),
	)
	if err != nil {
		return nil, err
	}

	// Seed employee user: employee@aegis.dev / employee123
	empHash, _ := bcrypt.GenerateFromPassword([]byte("employee123"), bcrypt.DefaultCost)
	_, err = db.Exec(
		`INSERT INTO users (tenant_id, email, name, role, password_hash) VALUES($1,$2,$3,$4,$5) ON CONFLICT (email) DO NOTHING`,
		tenantID, "employee@aegis.dev", "Employee One", "employee", string(empHash),
	)
	if err != nil {
		return nil, err
	}

	// Get employee ID for assigning test key
	var empID int
	db.QueryRow("SELECT id FROM users WHERE email = 'employee@aegis.dev'").Scan(&empID)

	hashedKey := HashKey("vk-testkey123")
	_, err = db.Exec(
		`INSERT INTO virtual_keys (tenant_id, user_id, key_hash, key_preview, is_active, expires_at, budget_usd, spend_usd)
		 VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT (key_hash) DO NOTHING`,
		tenantID, empID, hashedKey, "vk-testkey123", true,
		time.Now().Add(365*24*time.Hour), 10.00, 0.00,
	)
	if err != nil {
		return nil, err
	}

	_, err = db.Exec(`
		INSERT INTO model_routes (model_name, provider, upstream_url, weight, fallback_model)
		VALUES 
		('qwen2.5:7b',      'ollama',  'http://host.docker.internal:11434',         1.00, NULL),
		('gemini-2.5-flash','gemini',  'https://generativelanguage.googleapis.com',  1.00, 'gpt-4o-mini'),
		('gpt-4o-mini',     'openai',  'https://api.openai.com',                    1.00, NULL),
		('mixed-model',     'openai',  'https://api.openai.com',                    0.50, NULL),
		('mixed-model',     'gemini',  'https://generativelanguage.googleapis.com',  0.50, NULL)
		ON CONFLICT DO NOTHING
	`)
	if err != nil {
		return nil, err
	}

	return db, nil
}

// --- Auth Functions ---

func AuthenticateUser(db *sql.DB, email, password string) (*User, error) {
	var u User
	err := db.QueryRow(
		`SELECT id, tenant_id, email, name, role, password_hash FROM users WHERE email=$1`, email,
	).Scan(&u.ID, &u.TenantID, &u.Email, &u.Name, &u.Role, &u.PasswordHash)
	if err == sql.ErrNoRows {
		return nil, ErrInvalidCredentials
	}
	if err != nil {
		return nil, err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)); err != nil {
		return nil, ErrInvalidCredentials
	}
	return &u, nil
}

func CreateUser(db *sql.DB, tenantID int, email, name, role, password string) (*User, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	var u User
	err = db.QueryRow(
		`INSERT INTO users (tenant_id, email, name, role, password_hash) VALUES($1,$2,$3,$4,$5) RETURNING id, tenant_id, email, name, role`,
		tenantID, email, name, role, string(hash),
	).Scan(&u.ID, &u.TenantID, &u.Email, &u.Name, &u.Role)
	return &u, err
}

func GetAllUsers(db *sql.DB) ([]User, error) {
	rows, err := db.Query(`SELECT id, tenant_id, email, name, role FROM users ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var users []User
	for rows.Next() {
		var u User
		rows.Scan(&u.ID, &u.TenantID, &u.Email, &u.Name, &u.Role)
		users = append(users, u)
	}
	return users, nil
}

// --- Virtual Key Management ---

func GetAllVirtualKeys(db *sql.DB) ([]VirtualKey, error) {
	rows, err := db.Query(`
		SELECT vk.id, vk.user_id, u.email, u.name, vk.key_preview, vk.is_active, vk.budget_usd, vk.spend_usd, vk.expires_at
		FROM virtual_keys vk
		LEFT JOIN users u ON u.id = vk.user_id
		ORDER BY vk.id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var keys []VirtualKey
	for rows.Next() {
		var k VirtualKey
		rows.Scan(&k.ID, &k.UserID, &k.UserEmail, &k.UserName, &k.KeyPreview, &k.IsActive, &k.BudgetUSD, &k.SpendUSD, &k.ExpiresAt)
		keys = append(keys, k)
	}
	return keys, nil
}

func GetUserVirtualKeys(db *sql.DB, userID int) ([]VirtualKey, error) {
	rows, err := db.Query(`
		SELECT id, user_id, key_preview, is_active, budget_usd, spend_usd, expires_at
		FROM virtual_keys WHERE user_id = $1 ORDER BY id DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var keys []VirtualKey
	for rows.Next() {
		var k VirtualKey
		rows.Scan(&k.ID, &k.UserID, &k.KeyPreview, &k.IsActive, &k.BudgetUSD, &k.SpendUSD, &k.ExpiresAt)
		keys = append(keys, k)
	}
	return keys, nil
}

func CreateVirtualKey(db *sql.DB, tenantID, userID int, rawKey string, budgetUSD float64) error {
	hashed := HashKey(rawKey)
	_, err := db.Exec(
		`INSERT INTO virtual_keys (tenant_id, user_id, key_hash, key_preview, is_active, expires_at, budget_usd, spend_usd)
		 VALUES($1,$2,$3,$4,TRUE,$5,$6,0)`,
		tenantID, userID, hashed, rawKey, time.Now().Add(365*24*time.Hour), budgetUSD,
	)
	return err
}

func RevokeVirtualKey(db *sql.DB, keyID int) error {
	_, err := db.Exec(`UPDATE virtual_keys SET is_active = FALSE WHERE id = $1`, keyID)
	return err
}

func GetBudgetAlerts(db *sql.DB) ([]VirtualKey, error) {
	rows, err := db.Query(`
		SELECT vk.id, vk.user_id, u.email, u.name, vk.key_preview, vk.is_active, vk.budget_usd, vk.spend_usd, vk.expires_at
		FROM virtual_keys vk
		LEFT JOIN users u ON u.id = vk.user_id
		WHERE vk.spend_usd >= vk.budget_usd * 0.8 AND vk.is_active = TRUE
		ORDER BY (vk.spend_usd / vk.budget_usd) DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var keys []VirtualKey
	for rows.Next() {
		var k VirtualKey
		rows.Scan(&k.ID, &k.UserID, &k.UserEmail, &k.UserName, &k.KeyPreview, &k.IsActive, &k.BudgetUSD, &k.SpendUSD, &k.ExpiresAt)
		keys = append(keys, k)
	}
	return keys, nil
}

// --- Audit Logging ---

func LogRequest(db *sql.DB, virtualKey, model, provider string, promptTokens, completionTokens, latencyMS int, costUSD float64, status string) {
	// Fire and forget in a goroutine so it never blocks the response
	go func() {
		_, err := db.Exec(
			`INSERT INTO request_logs (virtual_key, model, provider, prompt_tokens, completion_tokens, cost_usd, latency_ms, status)
			 VALUES($1,$2,$3,$4,$5,$6,$7,$8)`,
			virtualKey, model, provider, promptTokens, completionTokens, costUSD, latencyMS, status,
		)
		if err != nil {
			log.Printf("[AUDIT] Failed to log request: %v", err)
		}
	}()
}

func GetRequestLogs(db *sql.DB, limit int) ([]RequestLog, error) {
	rows, err := db.Query(
		`SELECT id, virtual_key, model, provider, prompt_tokens, completion_tokens, cost_usd, latency_ms, status, created_at
		 FROM request_logs ORDER BY created_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var logs []RequestLog
	for rows.Next() {
		var l RequestLog
		rows.Scan(&l.ID, &l.VirtualKey, &l.Model, &l.Provider, &l.PromptTokens, &l.CompletionTokens, &l.CostUSD, &l.LatencyMS, &l.Status, &l.CreatedAt)
		logs = append(logs, l)
	}
	return logs, nil
}

func GetUserRequestLogs(db *sql.DB, virtualKeyHashes []string, limit int) ([]RequestLog, error) {
	if len(virtualKeyHashes) == 0 {
		return nil, nil
	}
	rows, err := db.Query(
		`SELECT id, virtual_key, model, provider, prompt_tokens, completion_tokens, cost_usd, latency_ms, status, created_at
		 FROM request_logs WHERE virtual_key = ANY($1) ORDER BY created_at DESC LIMIT $2`,
		virtualKeyHashes, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var logs []RequestLog
	for rows.Next() {
		var l RequestLog
		rows.Scan(&l.ID, &l.VirtualKey, &l.Model, &l.Provider, &l.PromptTokens, &l.CompletionTokens, &l.CostUSD, &l.LatencyMS, &l.Status, &l.CreatedAt)
		logs = append(logs, l)
	}
	return logs, nil
}

// --- Provider Key Management ---

func GetProviders(db *sql.DB) ([]Provider, error) {
	rows, err := db.Query(`SELECT name, api_key, endpoint_url FROM providers ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var providers []Provider
	for rows.Next() {
		var p Provider
		rows.Scan(&p.Name, &p.APIKey, &p.EndpointURL)
		providers = append(providers, p)
	}
	return providers, nil
}

func UpsertProvider(db *sql.DB, name, apiKey, endpointURL string) error {
	_, err := db.Exec(
		`INSERT INTO providers (name, api_key, endpoint_url) VALUES($1,$2,$3)
		 ON CONFLICT (name) DO UPDATE SET api_key=$2, endpoint_url=$3`,
		name, apiKey, endpointURL,
	)
	return err
}

// --- Validate virtual key (proxy auth) ---
func ValidateKey(db *sql.DB, rawKey string) error {
	hashed := HashKey(rawKey)
	var active bool
	var expiresAt sql.NullTime
	var budgetUsd float64
	var spendUsd float64

	err := db.QueryRow(
		`SELECT is_active, expires_at, budget_usd, spend_usd FROM virtual_keys WHERE key_hash=$1`,
		hashed,
	).Scan(&active, &expiresAt, &budgetUsd, &spendUsd)

	if err == sql.ErrNoRows {
		return ErrInvalidKey
	}
	if err != nil {
		return err
	}
	if !active {
		return ErrInvalidKey
	}
	if expiresAt.Valid && time.Now().After(expiresAt.Time) {
		return ErrInvalidKey
	}
	if spendUsd >= budgetUsd {
		return ErrOutOfBudget
	}
	return nil
}

func UpdateKeySpend(db *sql.DB, rawKey string, amount float64) error {
	hashed := HashKey(rawKey)
	_, err := db.Exec(
		`UPDATE virtual_keys SET spend_usd = spend_usd + $1 WHERE key_hash = $2`,
		amount, hashed,
	)
	return err
}

