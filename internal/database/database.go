package database

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	_ "github.com/lib/pq"
)

var(
	ErrInvalidKey = errors.New("Invalid API Key")
	ErrOutOfBudget = errors.New("Budget limit Exceed")
)

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

	if _, err := db.Exec(tenantTable); err != nil {
		return nil, err
	}

	if _, err := db.Exec(keyTable); err != nil {
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
