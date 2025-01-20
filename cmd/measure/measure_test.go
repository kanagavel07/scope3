// Package main provides the main entry point for the application and includes
// the test functions for the emission handler.
package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// SetUpRouter initializes and returns a new Gin engine instance.
func SetUpRouter() *gin.Engine {
	router := gin.Default()
	return router
}

// TestEmissionHandler tests the EmissionHandler function to ensure it processes
// requests correctly and within an acceptable time frame.
func TestEmissionHandler(t *testing.T) {
	// Create Server instance
	server, err := CreateServer(1<<30, 24*time.Hour) // 1GB cache
	if err != nil {
		t.Errorf("failed to create server: %v", err)
	}

	r := SetUpRouter()
	r.POST("/", server.EmissionHandler)

	// Define the payload for the POST request.
	payload := []byte(`{
		"rows": [
			{
				"inventoryId": "nytimes.com",
				"utcDatetime": "2024-12-30",
				"priority": 1
			},
			{
				"inventoryId": "yahoo.com",
				"utcDatetime": "2024-12-30",
				"priority": 1
			}
		]
	}`)

	// Create a new HTTP POST request with the payload.
	req, _ := http.NewRequest("POST", "/", bytes.NewBuffer(payload))
	req.Header.Set("Content-Type", "application/json")

	// Create a new HTTP test recorder to capture the response.
	w := httptest.NewRecorder()

	// Measure the time taken to process the request.
	start := time.Now()
	r.ServeHTTP(w, req)
	duration := time.Since(start)

	// Check if the response status code is OK.
	if w.Code != http.StatusOK {
		t.Errorf("expected status OK, got %v", w.Code)
	}

	t.Logf("Time taken: %v ms", duration.Milliseconds())

	// Create a new HTTP POST request with the payload.
	req, _ = http.NewRequest("POST", "/", bytes.NewBuffer(payload))
	req.Header.Set("Content-Type", "application/json")

	// Create a new HTTP test recorder to capture the response.
	w = httptest.NewRecorder()

	// Measure the time taken to process the request.
	start = time.Now()
	r.ServeHTTP(w, req)
	duration = time.Since(start)

	// Check if the response status code is OK.
	if w.Code != http.StatusOK {
		t.Errorf("expected status OK, got %v", w.Code)
	}

	t.Logf("Time taken: %v ms", duration.Milliseconds())

	if duration.Milliseconds() > 50 {
		t.Errorf("expected duration less than 50ms, got %v ms", duration.Milliseconds())
	}
}

// TestEmissionHandlerEvictionExpiration tests the eviction policy of the cache based on expiration time
func TestEmissionHandlerEvictionExpiration(t *testing.T) {
	// Create Server instance with a large cache and expiration time of 10 seconds
	server, err := CreateServer(1<<30, 10*time.Second) // 1GB cache, 10 seconds expiration
	if err != nil {
		t.Errorf("failed to create server: %v", err)
	}

	r := SetUpRouter()
	r.POST("/", server.EmissionHandler)

	// Define the payload for the POST request.
	payload := []byte(`{
		"rows": [
			{
				"inventoryId": "nytimes.com",
				"utcDatetime": "2024-12-30",
				"priority": 1
			}
		]
	}`)

	// Send the request with one inventory.
	req, _ := http.NewRequest("POST", "/", bytes.NewBuffer(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected status OK, got %v", w.Code)
	}

	// Wait for 11 seconds to ensure the item expires.
	time.Sleep(11 * time.Second)

	// Check the cache to ensure the item is evicted after expiration time.
	if _, ok := server.Cache.Get(CacheKey{InventoryID: "nytimes.com", UtcDatetime: "2024-12-30"}); ok {
		t.Errorf("expected nytimes.com to be evicted from the cache after expiration time")
	}
}

// TestEmissionHandlerEviction tests the eviction policy of the cache based on priority
func TestEmissionHandlerEvictionPriority(t *testing.T) {
	// Create Server instance with maxSize as 2
	server, err := CreateServer(2, 24*time.Hour)
	if err != nil {
		t.Errorf("failed to create server: %v", err)
	}

	r := SetUpRouter()
	r.POST("/", server.EmissionHandler)

	// Define the payloads for the POST requests.
	payload1 := []byte(`{
		"rows": [
			{
				"inventoryId": "nytimes.com",
				"utcDatetime": "2024-12-30",
				"priority": 1
			},
			{
				"inventoryId": "yahoo.com",
				"utcDatetime": "2024-12-30",
				"priority": 2
			}
		]
	}`)

	payload2 := []byte(`{
		"rows": [
			{
				"inventoryId": "theguardian.com",
				"utcDatetime": "2024-12-30",
				"priority": 1
			}
		]
	}`)

	// Send the first request with two inventories of priority 1 and 2.
	req, _ := http.NewRequest("POST", "/", bytes.NewBuffer(payload1))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected status OK, got %v", w.Code)
	}

	// Send the third request with one inventory of priority 1.
	req, _ = http.NewRequest("POST", "/", bytes.NewBuffer(payload2))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected status OK, got %v", w.Code)
	}

	// Check the cache to ensure the item with priority 2 is evicted and items with priority 1 are still alive.
	if _, ok := server.Cache.Get(CacheKey{InventoryID: "nytimes.com", UtcDatetime: "2024-12-30"}); !ok {
		t.Errorf("expected nytimes.com to be in the cache")
	}

	if _, ok := server.Cache.Get(CacheKey{InventoryID: "yahoo.com", UtcDatetime: "2024-12-30"}); ok {
		t.Errorf("expected yahoo.com to be evicted from the cache")
	}

	if _, ok := server.Cache.Get(CacheKey{InventoryID: "theguardian.com", UtcDatetime: "2024-12-30"}); !ok {
		t.Errorf("expected theguardian.com to be in the cache")
	}
}
