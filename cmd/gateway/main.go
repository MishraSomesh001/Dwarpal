package main

import (
	"aegis/internal/api"
	"aegis/internal/cache"
	"aegis/internal/database"
	"aegis/internal/guardrails"
	"aegis/internal/metrics"
	"aegis/internal/middleware"
	"aegis/internal/provider"
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const ProviderCtxKey contextKey = "provider_name"
const IsStreamingKey contextKey = "is_streaming"

// OpenAI Response Struct
type OpenAIResponse struct {
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

type contextKey string
const OriginalBodyKey contextKey = "original_body"

// Model struct
type RequestModel struct {
	Model  string `json:"model"`
	Stream bool   `json:"stream"`
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

func peekModel(req *http.Request) (string, []byte, bool, error) {
	if req.Body == nil {
		return "", nil, false, nil
	}

	bodyBytes, err := io.ReadAll(req.Body)
	if err != nil {
		return "", nil, false, err
	}

	req.Body.Close()
	req.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	var reqModel RequestModel
	if err := json.Unmarshal(bodyBytes, &reqModel); err != nil {
		return "", nil, false, err
	}
	return reqModel.Model, bodyBytes, reqModel.Stream, nil
}

// reverse proxy -> OpenAI
func newDynamicProxy(apiKey string, geminiKey string, db *sql.DB) (*httputil.ReverseProxy, error) {

	// Parse target URL
	openAITarget, _ := url.Parse("https://api.openai.com")
	// Breakers map
	breakers:= map[string]*provider.CircuitBreaker{
			"openai": provider.NewCircuitBreaker(5, 30*time.Second),
			"gemini": provider.NewCircuitBreaker(5, 30*time.Second),
			"ollama": provider.NewCircuitBreaker(5, 30*time.Second),
		}
	// proxy
	proxy := httputil.NewSingleHostReverseProxy(openAITarget)

	// Register custom resilience transport
	proxy.Transport = &AegisTransport{
		db:        db,
		apiKey:    apiKey,
		geminiKey: geminiKey,
		transport: http.DefaultTransport,
		breakers: breakers,
	}

	// Modify Request
	proxy.Director = func(req *http.Request) {
		model, bodyBytes, isStreaming, err := peekModel(req)
		if err != nil {
			log.Printf("Failed to peek model: %v", err)
			return
		}

		// Apply PII guardrails — mask sensitive data before sending to LLM
		bodyBytes = guardrails.MaskJSONBody(bodyBytes)
		req.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

		// Save original body + streaming flag in context
		ctx := context.WithValue(req.Context(), OriginalBodyKey, bodyBytes)
		ctx = context.WithValue(ctx, IsStreamingKey, isStreaming)
		*req = *req.WithContext(ctx)

		// 1. Look up route in PostgreSQL
		route, err := database.GetModelRoute(db, model)
		if err != nil {
			log.Printf("Route not found for model '%s': %v. Falling back to OpenAI", model, err)
			route = &database.ModelRoute{
				ModelName:   model,
				Provider:    "openai",
				UpstreamURL: "https://api.openai.com",
			}
		}
		if cb, ok := breakers[route.Provider]; ok && !cb.AllowRequest() {
			if route.FallbackModel.Valid && route.FallbackModel.String != "" {
				fallbackModel := route.FallbackModel.String
				log.Printf("[BREAKER] Circuit for provider '%s' is OPEN. Bypassing to fallback model '%s'", route.Provider, fallbackModel)
				
				// Overwrite route with the fallback model's route
				fbRoute, fbErr := database.GetModelRoute(db, fallbackModel)
				if fbErr == nil {
					route = fbRoute
				}
			}
		}
		ctx = context.WithValue(ctx, ProviderCtxKey, route.Provider) // Save provider name
		*req = *req.WithContext(ctx)
		// 2. Parse the upstream target URL
		target, err := url.Parse(route.UpstreamURL)
		if err != nil {
			log.Printf("Failed to parse upstream URL '%s': %v", route.UpstreamURL, err)
			return
		}

		// 3. Rewrite request host/scheme
		req.URL.Scheme = target.Scheme
		req.URL.Host = target.Host
		req.Host = target.Host

		// 4. Provider-specific configuration
		if route.Provider == "ollama" {
			req.Header.Del("Authorization")
		} else if route.Provider == "gemini" {
			upstreamModel := route.ModelName
			if upstreamModel == "mixed-model" {
				upstreamModel = "gemini-2.5-flash"
			}
			// Use streaming endpoint when client requests stream:true
			geminiEndpoint := "generateContent"
			if isStreaming {
				geminiEndpoint = "streamGenerateContent"
			}
			req.URL.Path = "/v1beta/models/" + upstreamModel + ":" + geminiEndpoint

			// Inject Gemini Key
			q := req.URL.Query()
			q.Set("key", geminiKey)
			req.URL.RawQuery = q.Encode()
			req.Header.Del("Authorization")

			// Translate request body
			geminiBytes, err := provider.TranslateOpenAIToGemini(bodyBytes)
			if err == nil {
				req.Body = io.NopCloser(bytes.NewBuffer(geminiBytes))
				req.ContentLength = int64(len(geminiBytes))
				req.Header.Set("Content-Length", strconv.Itoa(len(geminiBytes)))
			}
		} else {
			// OpenAI Default
			req.Header.Set("Authorization", "Bearer "+apiKey)
		}
	}
	// Modify incoming response
	proxy.ModifyResponse = func(resp *http.Response) error {
		// For streaming requests, pass raw SSE bytes directly to client — do not buffer!
		if isStreaming, ok := resp.Request.Context().Value(IsStreamingKey).(bool); ok && isStreaming {
			return nil
		}

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

		// 1. Detect if this was a Gemini request by inspecting the URL Path
		isGemini := strings.Contains(resp.Request.URL.Path, "generateContent")
		
		var promptTokens, completionTokens int
		var totalCost float64
		if isGemini {
			// 2. Translate Gemini JSON response to OpenAI JSON response
			translatedBytes, err := provider.TranslateGeminiToOpenAI(bodyBytes)
			if err != nil {
				log.Printf("Failed to translate Gemini response: %v", err)
				return err
			}
			// 3. Replace response body and update headers
			resp.Body.Close()
			resp.Body = io.NopCloser(bytes.NewBuffer(translatedBytes))
			resp.ContentLength = int64(len(translatedBytes))
			resp.Header.Set("Content-Length", strconv.Itoa(len(translatedBytes)))
			// 4. Decode the translated response to extract token usage for billing
			var openAIResp OpenAIResponse
			json.Unmarshal(translatedBytes, &openAIResp)
			promptTokens = openAIResp.Usage.PromptTokens
			completionTokens = openAIResp.Usage.CompletionTokens
		} else {
			// Standard OpenAI/Ollama parsing
			var openAIResp OpenAIResponse
			json.Unmarshal(bodyBytes, &openAIResp)
			promptTokens = openAIResp.Usage.PromptTokens
			completionTokens = openAIResp.Usage.CompletionTokens
		}
		
		// 5. Calculate Cost based on routed provider/host
		if isGemini {
			// Gemini pricing (e.g. $0.075 per 1M input, $0.30 per 1M output)
			totalCost = (float64(promptTokens) * 0.000000075) + (float64(completionTokens) * 0.00000030)
		} else if strings.Contains(resp.Request.URL.Host, "host.docker.internal") {
			// Ollama requests are self-hosted (free)
			totalCost = 0.0
		} else {
			// OpenAI pricing
			totalCost = (float64(promptTokens) * 0.00000015) + (float64(completionTokens) * 0.00000060)
		}
		// 6. Update database spend
		if totalCost > 0 {
			err := database.UpdateKeySpend(db, virtualKey, totalCost)
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
	geminiKey := os.Getenv("GEMINI_API_KEY")

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
	proxy, err := newDynamicProxy(apiKey,geminiKey, db)
	if err != nil {
		log.Fatal(err)
	}
	// proxy Handler
	proxyHandler:= http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxy.ServeHTTP(w, r)
	})
	//Rate Limithandler
	limiter:= middleware.RateLimitMiddleware(rdb,20,1*time.Minute)
	rateLimitedHandler := limiter(proxyHandler)

	// Auth Handler
	authedHandler := middleware.AuthMiddleware(db,rateLimitedHandler)

	http.Handle("/v1/", authedHandler)

	// --- Health & Readiness Endpoints ---
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		dbOK := db.Ping() == nil
		redisOK := rdb.Ping(r.Context()).Err() == nil
		status := "ok"
		code := http.StatusOK
		if !dbOK || !redisOK {
			status = "degraded"
			code = http.StatusServiceUnavailable
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		json.NewEncoder(w).Encode(map[string]any{
			"status": status,
			"db":     map[string]bool{"connected": dbOK},
			"redis":  map[string]bool{"connected": redisOK},
		})
	})

	// --- REST API Routes ---
	// Auth (public)
	http.HandleFunc("/api/auth/login", api.HandleLogin(db))

	// Admin routes — JWT required + admin role
	adminMux := http.NewServeMux()
	adminMux.HandleFunc("/api/admin/keys", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		if r.Method == http.MethodOptions {
			api.CORSHandler(w, r); return
		}
		switch r.Method {
		case http.MethodGet:
			api.HandleAdminGetKeys(db)(w, r)
		case http.MethodPost:
			api.HandleAdminCreateKey(db)(w, r)
		case http.MethodDelete:
			api.HandleAdminRevokeKey(db)(w, r)
		}
	})
	adminMux.HandleFunc("/api/admin/users", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		if r.Method == http.MethodOptions {
			api.CORSHandler(w, r); return
		}
		switch r.Method {
		case http.MethodGet:
			api.HandleAdminGetUsers(db)(w, r)
		case http.MethodPost:
			api.HandleAdminCreateUser(db)(w, r)
		}
	})
	adminMux.HandleFunc("/api/admin/providers", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		if r.Method == http.MethodOptions {
			api.CORSHandler(w, r); return
		}
		switch r.Method {
		case http.MethodGet:
			api.HandleAdminGetProviders(db)(w, r)
		case http.MethodPost:
			api.HandleAdminUpsertProvider(db)(w, r)
		}
	})
	adminMux.HandleFunc("/api/admin/logs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		if r.Method == http.MethodOptions {
			api.CORSHandler(w, r); return
		}
		api.HandleAdminGetLogs(db)(w, r)
	})
	adminMux.HandleFunc("/api/admin/alerts", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		if r.Method == http.MethodOptions {
			api.CORSHandler(w, r); return
		}
		api.HandleAdminGetAlerts(db)(w, r)
	})
	http.Handle("/api/admin/", middleware.JWTMiddleware(middleware.AdminOnly(adminMux)))

	// Employee routes — JWT required
	employeeMux := http.NewServeMux()
	employeeMux.HandleFunc("/api/employee/dashboard", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		if r.Method == http.MethodOptions {
			api.CORSHandler(w, r); return
		}
		api.HandleEmployeeDashboard(db)(w, r)
	})
	employeeMux.HandleFunc("/api/employee/logs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		if r.Method == http.MethodOptions {
			api.CORSHandler(w, r); return
		}
		api.HandleEmployeeLogs(db)(w, r)
	})
	http.Handle("/api/employee/", middleware.JWTMiddleware(employeeMux))

	// Expose Prometheus metrics on a separate port
	go func() {
		metricsServeMux := http.NewServeMux()
		metricsServeMux.Handle("/metrics", promhttp.Handler())
		log.Println("Metrics server running on :9090")
		if err := http.ListenAndServe(":9090", metricsServeMux); err != nil {
			log.Printf("Metrics server error: %v", err)
		}
	}()

	log.Println("Proxy running on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

// AegisTransport implements http.RoundTripper and handles automatic failover/retries
type AegisTransport struct {
	db        *sql.DB
	apiKey    string
	geminiKey string
	transport http.RoundTripper
	breakers map[string]*provider.CircuitBreaker
}

func (t *AegisTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// 1. Keep track of the original request context
	originalCtx := req.Context()
	providerName, _ := originalCtx.Value(ProviderCtxKey).(string)
	cb := t.breakers[providerName]

	// Extract model name for metrics labels
	var modelName string
	if originalBody, ok := originalCtx.Value(OriginalBodyKey).([]byte); ok {
		var rm RequestModel
		if json.Unmarshal(originalBody, &rm) == nil {
			modelName = rm.Model
		}
	}

	// Start latency timer
	start := time.Now()

	// 2. Execute the primary request
	resp, err := t.transport.RoundTrip(req)

	// Record request duration regardless of outcome
	if providerName != "" {
		metrics.RequestDuration.WithLabelValues(providerName).Observe(time.Since(start).Seconds())
	}

	// Check for failures
	isFailure := (err != nil) || (resp != nil && (resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500))

	if cb != nil {
		if isFailure {
			cb.RecordFailure()
			log.Printf("[BREAKER] Recorded failure for provider '%s'. State: %s", providerName, cb.State())
			// Update circuit breaker gauge
			if cb.State() == "OPEN" {
				metrics.CircuitBreakerOpen.WithLabelValues(providerName).Set(1)
			}
		} else {
			cb.RecordSuccess()
			metrics.CircuitBreakerOpen.WithLabelValues(providerName).Set(0)
		}
	}

	// Record request outcome
	if providerName != "" && modelName != "" {
		if isFailure {
			metrics.RequestsTotal.WithLabelValues(providerName, modelName, "error").Inc()
		} else {
			metrics.RequestsTotal.WithLabelValues(providerName, modelName, "success").Inc()
		}
	}

	// Audit log — fire and forget (non-blocking goroutine inside LogRequest)
	virtualKey, _ := originalCtx.Value(middleware.VirtualKeyCtxKey).(string)
	latencyMS := int(time.Since(start).Milliseconds())
	status := "success"
	if isFailure {
		status = "error"
	}
	database.LogRequest(t.db, virtualKey, modelName, providerName, 0, 0, latencyMS, 0, status)

	if isFailure {
		// Retrieve the original body bytes we stored in the context
		originalBody, ok := originalCtx.Value(OriginalBodyKey).([]byte)
		if ok && len(originalBody) > 0 {
			var reqModel RequestModel
			if json.Unmarshal(originalBody, &reqModel) == nil && reqModel.Model != "" {
				// Query DB to see if the primary route defines a fallback model
				route, routeErr := database.GetModelRoute(t.db, reqModel.Model)
					if routeErr == nil && route.FallbackModel.Valid && route.FallbackModel.String != "" {
						fallbackModel := route.FallbackModel.String
						log.Printf("Primary request failed (err: %v). Attempting failover to fallback model: %s", err, fallbackModel)
						// Record failover event in metrics
						metrics.FailoverTotal.WithLabelValues(providerName, fallbackModel).Inc()
					// Look up the fallback route details
					fallbackRoute, fbErr := database.GetModelRoute(t.db, fallbackModel)
					if fbErr == nil {
						// Create a new request using the original body
						fallbackReq, reqErr := http.NewRequestWithContext(originalCtx, "POST", fallbackRoute.UpstreamURL, bytes.NewBuffer(originalBody))
						if reqErr == nil {
							// Copy original headers
							for k, vv := range req.Header {
								for _, v := range vv {
									fallbackReq.Header.Add(k, v)
								}
							}

							target, _ := url.Parse(fallbackRoute.UpstreamURL)
							fallbackReq.URL.Scheme = target.Scheme
							fallbackReq.URL.Host = target.Host
							fallbackReq.Host = target.Host

							// Route fallback payload
							if fallbackRoute.Provider == "ollama" {
								fallbackReq.Header.Del("Authorization")
							} else if fallbackRoute.Provider == "gemini" {
								fallbackReq.URL.Path = "/v1beta/models/" + fallbackRoute.ModelName + ":generateContent"
								q := fallbackReq.URL.Query()
								q.Set("key", t.geminiKey)
								fallbackReq.URL.RawQuery = q.Encode()
								fallbackReq.Header.Del("Authorization")

								geminiBytes, transErr := provider.TranslateOpenAIToGemini(originalBody)
								if transErr == nil {
									fallbackReq.Body = io.NopCloser(bytes.NewBuffer(geminiBytes))
									fallbackReq.ContentLength = int64(len(geminiBytes))
									fallbackReq.Header.Set("Content-Length", strconv.Itoa(len(geminiBytes)))
								}
							} else {
								// OpenAI
								fallbackReq.Header.Set("Authorization", "Bearer "+t.apiKey)
							}

							// Execute the fallback request
							fbResp, fbErr := t.transport.RoundTrip(fallbackReq)
							if fbErr == nil {
								if resp != nil {
									resp.Body.Close() // Close the original failed body stream
								}
								return fbResp, nil
							}
							log.Printf("Fallback to %s also failed: %v", fallbackModel, fbErr)
						}
					}
				}
			}
		}
	}

	return resp, err
}