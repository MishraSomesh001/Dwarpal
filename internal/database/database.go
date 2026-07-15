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

	_ "github.com/lib/pq"
)

var(
	ErrInvalidKey = errors.New("Invalid API Key")
	ErrOutOfBudget = errors.New("Budget limit Exceed")
)


func init() {
	rand.Seed(time.Now().UnixNano())
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
		key_hash TEXT NOT NULL UNIQUE,
		is_active BOOLEAN DEFAULT TRUE,
		expires_at TIMESTAMP,
		budget_usd NUMERIC(10, 4) DEFAULT 10.00,
		spend_usd NUMERIC(10, 4) DEFAULT 0.00
	);
	`
	model_routes:=`
	CREATE TABLE IF NOT EXISTS model_routes (
		id SERIAL PRIMARY KEY,
		model_name TEXT NOT NULL,
		provider TEXT NOT NULL,
		upstream_url TEXT NOT NULL,
		weight NUMERIC(3,2) DEFAULT 1.00,
		fallback_model TEXT
	);
	`

	if _, err := db.Exec(tenantTable); err != nil {
		return nil, err
	}

	if _, err := db.Exec(keyTable); err != nil {
		return nil, err
	}
	if _, err := db.Exec(model_routes); err != nil {
		return nil, err
	}

	var count int

	err = db.QueryRow("SELECT COUNT(*) FROM tenants").Scan(&count)
	if err != nil {
		return nil, err
	}

	if count == 0 {
		var tenantID int
		err = db.QueryRow(
			"INSERT INTO tenants(name) VALUES($1) RETURNING id",
			"Default Tenant",
		).Scan(&tenantID)

		if err != nil {
			return nil, err
		}

		hashedKey := HashKey("vk-testkey123")

		_, err = db.Exec(
			`INSERT INTO virtual_keys
			(tenant_id, key_hash, is_active, expires_at, budget_usd, spend_usd)
			VALUES($1,$2,$3,$4,$5,$6)`,
			tenantID,
			hashedKey,
			true,
			time.Now().Add(365*24*time.Hour),
			10.00, // Budget
			0.00,  // Start Spend at 0
		)

		if err != nil {
			return nil, err
		}
		var routeCount int
		err = db.QueryRow("SELECT COUNT(*) FROM model_routes").Scan(&routeCount)
		if err == nil && routeCount == 0 {
			_, err = db.Exec(`
				INSERT INTO model_routes (model_name, provider, upstream_url, weight, fallback_model)
				VALUES 
				('qwen2.5:7b', 'ollama', 'http://host.docker.internal:11434', 1.00, NULL),
				('gemini-2.5-flash', 'gemini', 'https://generativelanguage.googleapis.com', 1.00, 'gpt-4o-mini'),
				('gpt-4o-mini', 'openai', 'https://api.openai.com', 1.00, NULL),
				-- Load balancing split: 50% OpenAI, 50% Gemini
				('mixed-model', 'openai', 'https://api.openai.com', 0.50, NULL),
				('mixed-model', 'gemini', 'https://generativelanguage.googleapis.com', 0.50, NULL)
			`)
			if err != nil {
				return nil, err
			}
		}
	}

	return db, nil
}
// Validate virtual key
func ValidateKey(db *sql.DB, rawKey string) error {

	hashed := HashKey(rawKey)

	var active bool
	var expiresAt sql.NullTime
	var budgetUsd float64
	var spendUsd float64

	err := db.QueryRow(
		`SELECT is_active, expires_at, budget_usd, spend_usd
		 FROM virtual_keys
		 WHERE key_hash=$1`,
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

	if spendUsd>=budgetUsd {
		return ErrOutOfBudget
	}

	return nil
}

func UpdateKeySpend(db *sql.DB, rawKey string, amount float64) error{
	hashed := HashKey(rawKey)

	_,err := db.Exec(
		`UPDATE virtual_keys 
		 SET spend_usd = spend_usd + $1 
		 WHERE key_hash = $2`,
		 amount,
		 hashed,
	)
	return err

}
