import http from 'k6/http';
import { check, sleep } from 'k6';
import { Counter, Rate, Trend } from 'k6/metrics';
import { randomString } from 'https://jslib.k6.io/k6-utils/1.2.0/index.js';

// Custom metrics
const limitedRequests = new Counter('limited_requests');
const requestsPerSecond = new Rate('requests_per_second');
const limitLatency = new Trend('limit_check_latency');

// Test configuration
export let options = {
  // Staged load test - increase load, maintain, then decrease
  stages: [
    { duration: '30s', target: 1000 },   // Ramp up to 1000 users over 30 seconds
    { duration: '1m', target: 1000 },    // Stay at 1000 users for 1 minute
    { duration: '2m', target: 5000 },    // Ramp up to 5000 users over 2 minutes
    { duration: '5m', target: 5000 },    // Stay at 5000 users for 5 minutes
    { duration: '1m', target: 10000 },   // Ramp up to 10000 users over 1 minute
    { duration: '3m', target: 10000 },   // Stay at 10000 users for 3 minutes
    { duration: '1m', target: 0 },       // Ramp down to 0 users over 1 minute
  ],
  // Limit the RPS to simulate the expected production load
  rps: 10000,
  // Test distribution across multiple load generators
  distribution: {
    'amazon:us-east-1': { loadZone: 'amazon:us-east-1', percent: 25 },
    'amazon:us-west-1': { loadZone: 'amazon:us-west-1', percent: 25 },
    'amazon:eu-west-1': { loadZone: 'amazon:eu-west-1', percent: 25 },
    'amazon:ap-southeast-1': { loadZone: 'amazon:ap-southeast-1', percent: 25 },
  },
  // Set thresholds for test success/failure
  thresholds: {
    http_req_duration: ['p(95)<15', 'p(99)<30'],     // 95% of requests must complete within 15ms, 99% within 30ms
    http_req_failed: ['rate<0.01'],                  // Error rate must be less than 1%
    limit_check_latency: ['p(95)<10', 'p(99)<20'],   // 95% of limit checks must complete within 10ms, 99% within 20ms
    limited_requests: ['count<1000'],                // Less than 1000 requests should be rate limited
  },
};

// Shared constants
const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';
const API_KEY = __ENV.API_KEY || 'test-api-key';

// Prepare test identifiers
const testUserIds = [];
const testApiKeys = [];
const testIps = [];

// Initialize test data
export function setup() {
  // Generate test identifiers
  for (let i = 0; i < 100; i++) {
    testUserIds.push(`user-${i}-${randomString(8)}`);
    testApiKeys.push(`api-${i}-${randomString(8)}`);
    testIps.push(`192.168.${Math.floor(i / 255)}.${i % 255}`);
  }

  // Warm up the system with some initial requests
  const warmupRequests = 100;
  for (let i = 0; i < warmupRequests; i++) {
    http.get(`${BASE_URL}/v1/ratelimit/check?identifier=${testUserIds[i % testUserIds.length]}&window=second`, {
      headers: { 'X-API-Key': API_KEY },
    });
    sleep(0.01);
  }

  return { userIds: testUserIds, apiKeys: testApiKeys, ips: testIps };
}

// Main test function
export default function(data) {
  const { userIds, apiKeys, ips } = data;

  // Randomly select test scenario
  const scenario = Math.floor(Math.random() * 4);

  let response;
  let startTime;
  
  switch (scenario) {
    case 0: // Check rate limit for user
      const userId = userIds[Math.floor(Math.random() * userIds.length)];
      startTime = new Date();
      response = http.get(`${BASE_URL}/v1/ratelimit/check?identifier=${userId}&window=second`, {
        headers: { 'X-API-Key': API_KEY },
        tags: { name: 'check_user_rate_limit' },
      });
      limitLatency.add(new Date() - startTime);
      break;
      
    case 1: // Check rate limit for API key
      const apiKey = apiKeys[Math.floor(Math.random() * apiKeys.length)];
      startTime = new Date();
      response = http.get(`${BASE_URL}/v1/ratelimit/check?identifier=${apiKey}&window=minute`, {
        headers: { 'X-API-Key': API_KEY },
        tags: { name: 'check_api_key_rate_limit' },
      });
      limitLatency.add(new Date() - startTime);
      break;
      
    case 2: // Check rate limit for IP
      const ip = ips[Math.floor(Math.random() * ips.length)];
      startTime = new Date();
      response = http.get(`${BASE_URL}/v1/ratelimit/check?identifier=${ip}&window=hour`, {
        headers: { 'X-API-Key': API_KEY },
        tags: { name: 'check_ip_rate_limit' },
      });
      limitLatency.add(new Date() - startTime);
      break;
      
    case 3: // Batch check rate limits
      const batchSize = 5;
      const batchIds = [];
      for (let i = 0; i < batchSize; i++) {
        batchIds.push(userIds[Math.floor(Math.random() * userIds.length)]);
      }
      
      startTime = new Date();
      response = http.post(`${BASE_URL}/v1/ratelimit/batch`, JSON.stringify({
        identifiers: batchIds,
        window: 'minute'
      }), {
        headers: { 
          'Content-Type': 'application/json',
          'X-API-Key': API_KEY
        },
        tags: { name: 'batch_check_rate_limit' },
      });
      limitLatency.add(new Date() - startTime);
      break;
  }

  // Record metrics
  requestsPerSecond.add(1);
  
  // Check if request was rate limited
  if (response.status === 429) {
    limitedRequests.add(1);
  }
  
  // Validate response
  check(response, {
    'status is 200 or 429': (r) => r.status === 200 || r.status === 429,
    'has valid headers': (r) => r.headers['X-RateLimit-Limit'] !== undefined && 
                               r.headers['X-RateLimit-Remaining'] !== undefined,
    'response is valid JSON': (r) => r.json() !== null,
  });
  
  // Add small random sleep to make the test more realistic
  sleep(Math.random() * 0.2);
}

// Cleanup function
export function teardown(data) {
  // Nothing to clean up
} 