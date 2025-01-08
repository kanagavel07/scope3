package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/cespare/xxhash"

	"github.com/dgraph-io/ristretto"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	"github.com/rs/zerolog"
)

// Initialize a global logger with Zerolog.
var logger = zerolog.New(
	zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339},
).Level(zerolog.TraceLevel).With().Timestamp().Caller().Logger()

var apiKey string
var httpClient = &http.Client{Timeout: 10 * time.Second}
var cache *ristretto.Cache
var cacheExpiration = 24 * time.Hour

// Load environment variables once during initialization.
func init() {
	err := godotenv.Load()

	if err != nil {
		err = godotenv.Load("../../.env")
		if err != nil {
			logger.Fatal().Msgf("Error loading .env file: %s", err)
		}
	}

	apiKey = os.Getenv("SCOPE3_API_TOKEN")
	if apiKey == "" {
		logger.Fatal().Msg("SCOPE3_API_TOKEN is not set in the environment variables")
	}

	if os.Getenv("ENV") == "production" {
		logger = zerolog.Nop()
	}

	// Initialize a global cache with Ristretto.
	cache, err = ristretto.NewCache(&ristretto.Config{
		NumCounters: 1e7,     // number of keys to track frequency of (10M).
		MaxCost:     1 << 30, // maximum cost of cache (1GB).
		BufferItems: 64,      // number of keys per Get buffer.
		KeyToHash: func(key interface{}) (uint64, uint64) {
			k := key.(CacheKey).InventoryID + ":" + key.(CacheKey).UtcDatetime
			return xxhash.Sum64String(k), xxhash.Sum64String(k)
		},
	})

	if err != nil {
		logger.Fatal().Msgf("Error creating cache: %s", err)
	}
}

// Inventory represents the input data structure for inventory items.
type Inventory struct {
	InventoryID string `json:"inventoryId" binding:"required"`
	UtcDatetime string `json:"utcDatetime" binding:"required"`
}

// EmissionData represents the output data structure for emission data.
type EmissionData struct {
	InventoryID string  `json:"inventoryId"`
	Emissions   float64 `json:"totalEmissions"`
}

// CacheKey represents the key structure for the cache.
type CacheKey struct {
	InventoryID string
	UtcDatetime string
}

// CacheValue represents the value structure for the cache.
type CacheValue struct {
	Emissions float64
}

// getEmissionDataFromInternalAPI fetches emission data from an internal API.
func getEmissionDataFromInternalAPI(rows map[string]Inventory) ([]EmissionData, error) {
	logger.Trace().Msg("Entry")
	defer logger.Trace().Msg("Exit")

	url := "https://api.scope3.com/v2/measure?includeRows=true&latest=true&fields=emissionsBreakdown"

	// Prepare request body
	var requestBodyRows []map[string]interface{}
	for _, row := range rows {
		requestBodyRows = append(requestBodyRows, map[string]interface{}{
			"country":       "US",  // hard-coded for now
			"channel":       "web", // hard-coded for now
			"impressions":   1000,  // hard-coded for now
			"inventoryId":   row.InventoryID,
			"utcDatetime":   row.UtcDatetime,
			"rowIdentifier": row.InventoryID,
		})
	}

	requestBody, err := json.Marshal(map[string]interface{}{
		"rows": requestBodyRows,
	})
	if err != nil {
		logger.Error().Msgf("failed to marshal request body: %s", err)
		return nil, err
	}

	logger.Debug().Msgf("requestBody: %s", requestBody)

	// Create HTTP request
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(requestBody))
	if err != nil {
		logger.Error().Msgf("failed to create request: %s", err)
		return nil, err
	}

	var bearer = "Bearer " + apiKey
	req.Header.Add("Authorization", bearer)
	req.Header.Add("Content-Type", "application/json")
	req.ContentLength = int64(len(requestBody))

	// Send HTTP request
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logger.Error().Msgf("failed to get emission data: %s", resp.Status)
		return nil, fmt.Errorf("failed to get emission data: %s", resp.Status)
	}

	// Decode response body
	var result struct {
		Rows []struct {
			RowIdentifier  string  `json:"rowIdentifier"`
			TotalEmissions float64 `json:"totalEmissions"`
		} `json:"rows"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		logger.Error().Msgf("failed to decode response body: %s", err)
		return nil, err
	}

	// Prepare emission data
	var emissionData []EmissionData
	for _, row := range result.Rows {
		emissionData = append(emissionData, EmissionData{
			InventoryID: row.RowIdentifier,
			Emissions:   row.TotalEmissions,
		})
	}

	logger.Debug().Msgf("emissionData: %v", emissionData)

	return emissionData, nil
}

// EmissionHandler handles HTTP POST requests to fetch emission data.
func EmissionHandler(c *gin.Context) {
	logger.Trace().Msg("Entry")
	defer logger.Trace().Msg("Exit")

	var reqBody struct {
		Rows []Inventory `binding:"dive"`
	}

	logger.Info().Msg("Request bind check")

	// Bind JSON request body to reqBody
	if err := c.BindJSON(&reqBody); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "inventoryId is required"})
		return
	}

	logger.Info().Msg("Request bind check passed")

	var result []EmissionData
	var cacheMisses map[string]Inventory = make(map[string]Inventory)

	// Check cache for each row
	for _, row := range reqBody.Rows {
		key := CacheKey{InventoryID: row.InventoryID, UtcDatetime: row.UtcDatetime}
		if value, found := cache.Get(key); found {
			logger.Info().Msgf("Cache hit for key: %v", key)
			result = append(result, EmissionData{InventoryID: key.InventoryID, Emissions: value.(CacheValue).Emissions})
		} else {
			logger.Info().Msgf("Cache miss for key: %v", key)
			cacheMisses[key.InventoryID] = row
		}
	}

	if len(cacheMisses) > 0 {
		logger.Info().Msg("Getting emission data from internal API")
		resBody, err := getEmissionDataFromInternalAPI(cacheMisses)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// Append the fetched emission data to the result slice
		result = append(result, resBody...)

		// Add the fetched data to the cache
		for _, data := range resBody {
			if _, ok := cacheMisses[data.InventoryID]; ok {
				key := CacheKey{InventoryID: data.InventoryID, UtcDatetime: cacheMisses[data.InventoryID].UtcDatetime}
				cache.SetWithTTL(key, CacheValue{Emissions: data.Emissions}, 1, cacheExpiration)
				logger.Info().Msgf("Added to cache: %v", key)
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{"rows": result})
}

// main initializes the Gin router and starts the HTTP server.
func main() {

	router := gin.Default()
	router.POST("/measure", EmissionHandler)

	router.Run(":8080")
}
