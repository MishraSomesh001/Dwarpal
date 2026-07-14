package main

import (
	"aegis/internal/database"
	"aegis/internal/middleware"
	"bufio"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"aegis/internal/cache"
	"time"
	"bytes"
	"encoding/json"
	"io"
	"database/sql"
)

// OpenAI Response Struct
type OpenAIResponse struct {
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

// Load env
func loadEnv(filepath string) (error){
	file, err := os.Open(filepath)
	if err!=nil{
		if os.IsNotExist(err){
			return nil
		}
		return err
	}
	defer file.Close()

	scanner:= bufio.NewScanner(file)
	for scanner.Scan(){
		line:= scanner.Text()
		line = strings.TrimSpace(line)

		if len(line) ==0 || strings.HasPrefix(line, "#"){
			continue
		}

		parts := strings.SplitN(line, "=",2)

		if len(parts)!=2{
			continue
		}

		key := strings.TrimSpace(parts[0])
		value:= strings.TrimSpace(parts[1])
		if (strings.HasPrefix(value, "\"") && strings.HasSuffix(value, "\"")) || 
   (strings.HasPrefix(value, "'") && strings.HasSuffix(value, "'")) {
    	value = value[1 : len(value)-1]
	}

		os.Setenv(key,value)

	}

	if err := scanner.Err(); err !=nil {
		return err
	}

	return nil
	
}

// reverse proxy -> OpenAI
func newOpenAIProxy(apiKey string, db *sql.DB) (*httputil.ReverseProxy, error) {

	// Parse target URL
	target, err := url.Parse("https://api.openai.com")
	if err != nil {
		return nil, err
	}

	// proxy
	proxy := httputil.NewSingleHostReverseProxy(target)

	// Modify Request
	proxy.Director = func(req *http.Request) {

		// Forward requests -> OpenAI
		req.URL.Scheme = target.Scheme
		req.URL.Host = target.Host

		// Host header
		req.Host = target.Host

		// Insert OpenAI API key
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	// Modify incoming response
	proxy.ModifyResponse = func(resp *http.Response) error {

		if resp.StatusCode != http.StatusOK {
			return nil
		}

		// Read response body
		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			log.Printf("Failed to read response body: %v", err)
			return nil
		}

		// Restore body because it can only be read once
		resp.Body.Close()
		resp.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

		// Parse JSON response
		var openAIResp OpenAIResponse
		if err := json.Unmarshal(bodyBytes, &openAIResp); err != nil {
			// Ignore responses that don't match OpenAI schema
			return nil
		}

		// Retrieve virtual key from request context
		virtualKey, ok := resp.Request.Context().Value(middleware.VirtualKeyCtxKey).(string)
		if !ok {
			log.Println("Virtual key not found in request context")
			return nil
		}

		// Pricing for GPT-4o Mini
		inputCost := float64(openAIResp.Usage.PromptTokens) * 0.00000015
		outputCost := float64(openAIResp.Usage.CompletionTokens) * 0.00000060

		totalCost := inputCost + outputCost


		if totalCost > 0 {

			err := database.UpdateKeySpend(db,virtualKey,totalCost)
			if err != nil {
				log.Printf("Failed to update spend: %v", err)
			}
		}

		return nil
	}

	return proxy, nil
}

func main() {
	// Loading env variables
	if err := loadEnv(".env"); err != nil {
    	log.Fatalf("Error loading .env file: %v", err)
	}
	apiKey := os.Getenv("OPENAI_API_KEY")

	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY not found")
	}

	// Init postgreDB
	db, err := database.InitDB()
	if err != nil {
		log.Fatalf("Error initializing database: %v", err)
	}
	defer db.Close()

	//init Redis
	rdb, err := cache.InitRedis()
	if err!= nil{
		log.Fatalf("Error initializing Redis: %v", err)
	}
	defer rdb.Close()
	// Create proxy
	proxy, err := newOpenAIProxy(apiKey, db)
	if err != nil {
		log.Fatal(err)
	}
	// proxy Handler
	proxyHandler:= http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxy.ServeHTTP(w, r)
	})
	//Rate Limithandler
	limiter:= middleware.RateLimitMiddleware(rdb,5,1*time.Minute)
	rateLimitedHandler := limiter(proxyHandler)

	// Auth Handler
	authedHandler := middleware.AuthMiddleware(db,rateLimitedHandler)

	http.Handle("/v1/", authedHandler)

	log.Println("Proxy running on :8080")

	log.Fatal(http.ListenAndServe(":8080", nil))
}