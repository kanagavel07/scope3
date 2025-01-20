package main

import (
	"bytes"
	"container/heap"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	"github.com/rs/zerolog"
)

// Cache represents a thread-safe in-memory cache with a maximum size and eviction policy.
// It uses a priority queue to manage the items based on their priority and timestamp.
//
// Fields:
// - mu: A mutex to ensure thread-safe access to the cache.
// - items: A map to store cache items with their keys.
// - maxSize: The maximum number of items the cache can hold.
// - currSize: The current number of items in the cache.
// - pq: A priority queue to manage the cache items based on their priority and timestamp.
// - onEvict: A callback function that is called when an item is evicted from the cache.
type Cache struct {
	mu       sync.Mutex
	items    map[CacheKey]*CacheItem
	maxSize  int64
	currSize int64
	pq       PriorityQueue
	onEvict  func(key CacheKey, value CacheValue)
}

// CacheItem represents an item stored in the cache with an expiry time and priority.
type CacheItem struct {
	key       CacheKey
	value     CacheValue
	expiry    time.Time
	index     int
	timestamp time.Time
}

// PriorityQueue implements a priority queue for CacheItem based on their priority and timestamp.
type PriorityQueue []*CacheItem

// Len returns the number of items in the priority queue.
func (pq PriorityQueue) Len() int { return len(pq) }

// Less compares two items in the priority queue based on their priority and timestamp.
func (pq PriorityQueue) Less(i, j int) bool {
	if pq[i].value.Priority == pq[j].value.Priority {
		return pq[i].timestamp.Before(pq[j].timestamp)
	}
	return pq[i].value.Priority > pq[j].value.Priority
}

// Swap swaps two items in the priority queue.
func (pq PriorityQueue) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
	pq[i].index = i
	pq[j].index = j
}

// Push adds an item to the priority queue.
func (pq *PriorityQueue) Push(x interface{}) {
	n := len(*pq)
	item := x.(*CacheItem)
	item.index = n
	*pq = append(*pq, item)
}

// Pop removes and returns the highest priority item from the priority queue.
func (pq *PriorityQueue) Pop() interface{} {
	old := *pq
	n := len(old)
	item := old[n-1]
	old[n-1] = nil
	item.index = -1
	*pq = old[0 : n-1]
	return item
}

// Get retrieves a value from the cache by its key. It returns the value and a boolean indicating whether the key was found.
func (c *Cache) Get(key CacheKey) (CacheValue, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	item, found := c.items[key]
	if !found || item.expiry.Before(time.Now()) {
		return CacheValue{}, false
	}

	return item.value, true
}

// SetWithTTL adds a key-value pair to the cache with a specified time-to-live (TTL). It evicts items if the cache exceeds its maximum size.
func (c *Cache) SetWithTTL(key CacheKey, value CacheValue, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if item, found := c.items[key]; found {
		c.currSize -= 1
		heap.Remove(&c.pq, item.index)
	}

	expiry := time.Now().Add(ttl)
	item := &CacheItem{
		key:       key,
		value:     value,
		expiry:    expiry,
		timestamp: time.Now(),
	}
	c.items[key] = item
	heap.Push(&c.pq, item)
	c.currSize += 1

	go func(key CacheKey, ttl time.Duration) {
		time.Sleep(ttl)
		c.mu.Lock()
		defer c.mu.Unlock()
		if item, found := c.items[key]; found && item.expiry.Before(time.Now()) {
			heap.Remove(&c.pq, item.index)
			delete(c.items, key)
			c.currSize -= 1
			if c.onEvict != nil {
				c.onEvict(item.key, item.value)
			}
		}
	}(key, ttl)

	for c.currSize > c.maxSize {
		evicted := heap.Pop(&c.pq).(*CacheItem)
		delete(c.items, evicted.key)
		c.currSize -= 1
		if c.onEvict != nil {
			c.onEvict(evicted.key, evicted.value)
		}
	}
}

// Config represents the configuration for the server, including cache expiration duration.
type Config struct {
	CacheExpiration time.Duration
}

// Server represents the server that handles HTTP requests and manages the cache.
type Server struct {
	Cache      *Cache
	HTTPClient *http.Client
	APIKey     string
	Logger     zerolog.Logger
	Config     *Config
}

// CreateServer initializes a new server instance with the specified cache size and expiration duration.
func CreateServer(cacheMaxCost int64, cacheExpirationInMilliSeconds time.Duration) (*Server, error) {
	err := godotenv.Load()
	if err != nil {
		err = godotenv.Load("../../.env")
		if err != nil {
			return nil, fmt.Errorf("Error loading .env file: %s", err)
		}
	}

	apiKey := os.Getenv("SCOPE3_API_TOKEN")
	if apiKey == "" {
		return nil, fmt.Errorf("SCOPE3_API_TOKEN is not set in the environment variables")
	}

	logger := zerolog.New(
		zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339},
	).Level(zerolog.TraceLevel).With().Timestamp().Caller().Logger()

	if os.Getenv("ENV") == "production" {
		logger = zerolog.Nop()
	}

	return &Server{
		Config: &Config{
			CacheExpiration: cacheExpirationInMilliSeconds,
		},
		Cache: &Cache{
			items:   make(map[CacheKey]*CacheItem),
			maxSize: cacheMaxCost,
			pq:      make(PriorityQueue, 0),
			onEvict: func(key CacheKey, value CacheValue) {
				logger := zerolog.New(
					zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339},
				).Level(zerolog.TraceLevel).With().Timestamp().Caller().Logger()
				logger.Info().Msgf("Evicted key: %v, value: %v", key, value)
			},
		},
		HTTPClient: &http.Client{Timeout: 10 * time.Second},
		APIKey:     apiKey,
		Logger:     logger,
	}, nil
}

// Inventory represents the input data structure for inventory items.
type Inventory struct {
	InventoryID string `json:"inventoryId" binding:"required"`
	UtcDatetime string `json:"utcDatetime" binding:"required"`
	Priority    uint8  `json:"priority" binding:"required,min=1,max=10"`
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
	Priority  uint8
}

// getEmissionDataFromInternalAPI fetches emission data from an internal API.
func (s *Server) getEmissionDataFromInternalAPI(rows map[string]Inventory) ([]EmissionData, error) {
	s.Logger.Trace().Msg("Entry")
	defer s.Logger.Trace().Msg("Exit")

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
		s.Logger.Error().Msgf("failed to marshal request body: %s", err)
		return nil, err
	}

	s.Logger.Debug().Msgf("requestBody: %s", requestBody)

	// Create HTTP request
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(requestBody))
	if err != nil {
		s.Logger.Error().Msgf("failed to create request: %s", err)
		return nil, err
	}

	var bearer = "Bearer " + s.APIKey
	req.Header.Add("Authorization", bearer)
	req.Header.Add("Content-Type", "application/json")
	req.ContentLength = int64(len(requestBody))

	// Send HTTP request
	resp, err := s.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		s.Logger.Error().Msgf("failed to get emission data: %s", resp.Status)
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
		s.Logger.Error().Msgf("failed to decode response body: %s", err)
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

	s.Logger.Debug().Msgf("emissionData: %v", emissionData)

	return emissionData, nil
}

// EmissionHandler handles HTTP POST requests to fetch emission data.
func (s *Server) EmissionHandler(c *gin.Context) {
	s.Logger.Trace().Msg("Entry")
	defer s.Logger.Trace().Msg("Exit")

	var reqBody struct {
		Rows []Inventory `binding:"dive"`
	}

	s.Logger.Info().Msg("Request bind check")

	// Bind JSON request body to reqBody
	if err := c.BindJSON(&reqBody); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "inventoryId is required"})
		return
	}

	s.Logger.Info().Msg("Request bind check passed")

	var result []EmissionData
	var cacheMisses map[string]Inventory = make(map[string]Inventory)

	// Check cache for each row
	for _, row := range reqBody.Rows {
		key := CacheKey{InventoryID: row.InventoryID, UtcDatetime: row.UtcDatetime}
		if value, found := s.Cache.Get(key); found {
			s.Logger.Info().Msgf("Cache hit for key: %v", key)
			result = append(result, EmissionData{InventoryID: key.InventoryID, Emissions: value.Emissions})
		} else {
			s.Logger.Info().Msgf("Cache miss for key: %v", key)
			cacheMisses[key.InventoryID] = row
		}
	}

	if len(cacheMisses) > 0 {
		s.Logger.Info().Msg("Getting emission data from internal API")
		resBody, err := s.getEmissionDataFromInternalAPI(cacheMisses)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// Append the fetched emission data to the result slice
		result = append(result, resBody...)

		// Add the fetched data to the cache
		for _, data := range resBody {
			if v, ok := cacheMisses[data.InventoryID]; ok {
				key := CacheKey{InventoryID: data.InventoryID, UtcDatetime: v.UtcDatetime}
				s.Cache.SetWithTTL(key, CacheValue{Emissions: data.Emissions, Priority: v.Priority}, s.Config.CacheExpiration)
				s.Logger.Info().Msgf("Added to cache: %v", key)
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{"rows": result})
}

// main initializes the Gin router and starts the HTTP server.
func main() {
	// Create a new server instance.
	server, err := CreateServer(1<<30, 24*time.Hour) // 1GB cache
	if err != nil {
		fmt.Println(err)
		return
	}

	router := gin.Default()
	router.POST("/measure", server.EmissionHandler)

	router.Run(":8080")
}
