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

)

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
func newOpenAIProxy(apiKey string) (*httputil.ReverseProxy, error) {

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

	return proxy, nil
}

func main() {
	if err := loadEnv(".env"); err != nil {
    	log.Fatalf("Error loading .env file: %v", err)
	}
	apiKey := os.Getenv("OPENAI_API_KEY")

	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY not found")
	}

	db, err := database.InitDB()
	if err != nil {
		log.Fatalf("Error initializing database: %v", err)
	}
	defer db.Close()


	// Create proxy
	proxy, err := newOpenAIProxy(apiKey)
	if err != nil {
		log.Fatal(err)
	}

	proxyHandler:= http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxy.ServeHTTP(w, r)
	})

	authedHandler := middleware.AuthMiddleware(db,proxyHandler)

	http.Handle("/v1/", authedHandler)

	log.Println("Proxy running on :8080")

	log.Fatal(http.ListenAndServe(":8080", nil))
}