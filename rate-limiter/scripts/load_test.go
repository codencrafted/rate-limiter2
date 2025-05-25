package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// Response structure for the rate limiter API
type Response struct {
	Limited      bool    `json:"limited"`
	CurrentCount int64   `json:"current_count"`
	Limit        int     `json:"limit"`
	Remaining    int     `json:"remaining"`
	ResetAfter   float64 `json:"reset_after"`
	Window       string  `json:"window"`
}

// Statistics for the load test
type Stats struct {
	totalRequests     int64
	successfulRequests int64
	limitedRequests   int64
	errorRequests     int64
	totalLatency      int64 // in nanoseconds
}

func main() {
	// Command line arguments
	url := flag.String("url", "http://localhost:8080/api/v1/check", "URL to load test")
	concurrency := flag.Int("concurrency", 10, "Number of concurrent workers")
	requests := flag.Int("requests", 1000, "Number of requests per worker")
	rps := flag.Int("rps", 100, "Target requests per second")
	window := flag.String("window", "second", "Window to test (second, minute, hour, day)")
	key := flag.String("key", "load-test", "Key to use for rate limiting")
	flag.Parse()

	fmt.Printf("Starting load test with %d workers, %d requests per worker, target RPS: %d\n",
		*concurrency, *requests, *rps)
	fmt.Printf("URL: %s, window: %s, key: %s\n", *url, *window, *key)

	// Create wait group for workers
	var wg sync.WaitGroup
	
	// Create atomic stats
	var stats Stats
	
	// Calculate delay between requests to achieve target RPS
	delay := time.Second / time.Duration(*rps)
	
	// Start time
	startTime := time.Now()
	
	// Start workers
	for i := 0; i < *concurrency; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			
			client := &http.Client{
				Timeout: 5 * time.Second,
			}
			
			for j := 0; j < *requests; j++ {
				// Calculate request URL with parameters
				requestURL := fmt.Sprintf("%s?key=%s&identifier=worker-%d&window=%s",
					*url, *key, workerID, *window)
				
				// Record start time
				requestStart := time.Now()
				
				// Send request
				resp, err := client.Get(requestURL)
				
				// Record request latency
				latency := time.Since(requestStart)
				atomic.AddInt64(&stats.totalLatency, int64(latency))
				
				// Increment total requests
				atomic.AddInt64(&stats.totalRequests, 1)
				
				// Handle response
				if err != nil {
					atomic.AddInt64(&stats.errorRequests, 1)
					fmt.Printf("Error: %v\n", err)
				} else {
					// Read response body
					body, err := io.ReadAll(resp.Body)
					resp.Body.Close()
					
					if err != nil {
						atomic.AddInt64(&stats.errorRequests, 1)
						fmt.Printf("Error reading response: %v\n", err)
					} else if resp.StatusCode != http.StatusOK {
						atomic.AddInt64(&stats.errorRequests, 1)
						fmt.Printf("Error status code: %d\n", resp.StatusCode)
					} else {
						// Parse response
						var response Response
						if err := json.Unmarshal(body, &response); err != nil {
							atomic.AddInt64(&stats.errorRequests, 1)
							fmt.Printf("Error parsing response: %v\n", err)
						} else {
							atomic.AddInt64(&stats.successfulRequests, 1)
							
							if response.Limited {
								atomic.AddInt64(&stats.limitedRequests, 1)
							}
						}
					}
				}
				
				// Sleep to maintain target RPS
				time.Sleep(delay)
			}
		}(i)
	}
	
	// Wait for all workers to complete
	wg.Wait()
	
	// Calculate total duration
	duration := time.Since(startTime)
	
	// Print results
	fmt.Println("\nLoad Test Results:")
	fmt.Println("==================")
	fmt.Printf("Duration: %.2f seconds\n", duration.Seconds())
	fmt.Printf("Total Requests: %d\n", stats.totalRequests)
	fmt.Printf("Successful Requests: %d (%.2f%%)\n", 
		stats.successfulRequests, 
		float64(stats.successfulRequests)/float64(stats.totalRequests)*100)
	fmt.Printf("Limited Requests: %d (%.2f%%)\n", 
		stats.limitedRequests, 
		float64(stats.limitedRequests)/float64(stats.totalRequests)*100)
	fmt.Printf("Error Requests: %d (%.2f%%)\n", 
		stats.errorRequests, 
		float64(stats.errorRequests)/float64(stats.totalRequests)*100)
	
	// Calculate actual RPS
	actualRPS := float64(stats.totalRequests) / duration.Seconds()
	fmt.Printf("Actual RPS: %.2f\n", actualRPS)
	
	// Calculate average latency
	avgLatency := time.Duration(stats.totalLatency) / time.Duration(stats.totalRequests)
	fmt.Printf("Average Latency: %s\n", avgLatency)
} 