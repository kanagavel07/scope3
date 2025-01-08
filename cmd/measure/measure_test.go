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
	r := SetUpRouter()
	r.POST("/", EmissionHandler)

	// Define the payload for the POST request.
	payload := []byte(`{
		"rows": [
			{
				"inventoryId": "nytimes.com",
				"utcDatetime": "2024-12-30"
			},
			{
				"inventoryId": "yahoo.com",
				"utcDatetime": "2024-12-30"
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
