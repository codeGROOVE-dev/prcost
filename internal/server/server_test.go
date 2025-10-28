package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/codeGROOVE-dev/prcost/pkg/cost"
	"github.com/codeGROOVE-dev/prcost/pkg/github"
)

func TestNew(t *testing.T) {
	s := New()
	if s == nil {
		t.Fatal("New() returned nil")
	}
	if s.logger == nil {
		t.Error("Server logger not initialized")
	}
	if s.httpClient == nil {
		t.Error("Server httpClient not initialized")
	}
	if s.ipLimiters == nil {
		t.Error("Server ipLimiters not initialized")
	}
}

func TestSetCommit(t *testing.T) {
	s := New()
	commit := "abc123def"
	s.SetCommit(commit)
	if s.serverCommit != commit {
		t.Errorf("SetCommit() failed: got %s, want %s", s.serverCommit, commit)
	}
}

func TestSetCORSConfig(t *testing.T) {
	tests := []struct {
		name         string
		origins      string
		allowAll     bool
		wantAllowAll bool
		wantOrigins  int
	}{
		{
			name:         "allow all",
			origins:      "",
			allowAll:     true,
			wantAllowAll: true,
			wantOrigins:  0,
		},
		{
			name:         "specific origins",
			origins:      "https://example.com,https://test.com",
			allowAll:     false,
			wantAllowAll: false,
			wantOrigins:  2,
		},
		{
			name:         "wildcard origin",
			origins:      "https://*.example.com",
			allowAll:     false,
			wantAllowAll: false,
			wantOrigins:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New()
			s.SetCORSConfig(tt.origins, tt.allowAll)
			if s.allowAllCors != tt.wantAllowAll {
				t.Errorf("allowAllCors = %v, want %v", s.allowAllCors, tt.wantAllowAll)
			}
			if len(s.allowedOrigins) != tt.wantOrigins {
				t.Errorf("len(allowedOrigins) = %d, want %d", len(s.allowedOrigins), tt.wantOrigins)
			}
		})
	}
}

func TestSetRateLimit(t *testing.T) {
	s := New()
	rps := 50
	burst := 75
	s.SetRateLimit(rps, burst)
	if s.rateLimit != rps {
		t.Errorf("rateLimit = %d, want %d", s.rateLimit, rps)
	}
	if s.rateBurst != burst {
		t.Errorf("rateBurst = %d, want %d", s.rateBurst, burst)
	}
}

func TestValidateGitHubPRURL(t *testing.T) {
	s := New()
	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{
			name:    "valid PR URL",
			url:     "https://github.com/owner/repo/pull/123",
			wantErr: false,
		},
		{
			name:    "valid PR URL with trailing slash",
			url:     "https://github.com/owner/repo/pull/123/",
			wantErr: false,
		},
		{
			name:    "invalid - not github.com",
			url:     "https://gitlab.com/owner/repo/pull/123",
			wantErr: true,
		},
		{
			name:    "invalid - http instead of https",
			url:     "http://github.com/owner/repo/pull/123",
			wantErr: true,
		},
		{
			name:    "invalid - has query params",
			url:     "https://github.com/owner/repo/pull/123?foo=bar",
			wantErr: true,
		},
		{
			name:    "invalid - has fragment",
			url:     "https://github.com/owner/repo/pull/123#section",
			wantErr: true,
		},
		{
			name:    "invalid - missing pull number",
			url:     "https://github.com/owner/repo/pull/",
			wantErr: true,
		},
		{
			name:    "invalid - wrong path format",
			url:     "https://github.com/owner/repo/issues/123",
			wantErr: true,
		},
		{
			name:    "invalid - too long",
			url:     "https://github.com/" + strings.Repeat("a", 200) + "/repo/pull/123",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := s.validateGitHubPRURL(tt.url)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateGitHubPRURL() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestExtractToken(t *testing.T) {
	s := New()
	tests := []struct {
		name   string
		header string
		want   string
	}{
		{
			name:   "Bearer token",
			header: "Bearer ghp_abc123",
			want:   "ghp_abc123",
		},
		{
			name:   "token prefix",
			header: "token ghp_abc123",
			want:   "ghp_abc123",
		},
		{
			name:   "plain token",
			header: "ghp_abc123",
			want:   "ghp_abc123",
		},
		{
			name:   "empty",
			header: "",
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/v1/calculate", http.NoBody)
			if tt.header != "" {
				req.Header.Set("Authorization", tt.header)
			}
			got := s.extractToken(req)
			if got != tt.want {
				t.Errorf("extractToken() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsOriginAllowed(t *testing.T) {
	s := New()
	s.SetCORSConfig("https://example.com,https://*.test.com,*.dev.com", false)

	tests := []struct {
		name   string
		origin string
		want   bool
	}{
		{
			name:   "exact match",
			origin: "https://example.com",
			want:   true,
		},
		{
			name:   "wildcard subdomain match",
			origin: "https://sub.test.com",
			want:   true,
		},
		{
			name:   "wildcard deep subdomain match",
			origin: "https://deep.sub.test.com",
			want:   true,
		},
		{
			name:   "wildcard without protocol",
			origin: "https://sub.dev.com",
			want:   true,
		},
		{
			name:   "no match",
			origin: "https://evil.com",
			want:   false,
		},
		{
			name:   "partial match not allowed",
			origin: "https://notexample.com",
			want:   false,
		},
		{
			name:   "protocol mismatch",
			origin: "http://sub.test.com",
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := s.isOriginAllowed(tt.origin)
			if got != tt.want {
				t.Errorf("isOriginAllowed(%q) = %v, want %v", tt.origin, got, tt.want)
			}
		})
	}
}

func TestHandleHealth(t *testing.T) {
	s := New()
	req := httptest.NewRequest(http.MethodGet, "/health", http.NoBody)
	w := httptest.NewRecorder()

	s.handleHealth(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("handleHealth() status = %d, want %d", w.Code, http.StatusOK)
	}

	var response map[string]string
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response["status"] != "healthy" {
		t.Errorf("handleHealth() status = %s, want healthy", response["status"])
	}
}

func TestServeHTTPSecurityHeaders(t *testing.T) {
	s := New()
	req := httptest.NewRequest(http.MethodGet, "/health", http.NoBody)
	w := httptest.NewRecorder()

	s.ServeHTTP(w, req)

	headers := map[string]string{
		"X-Content-Type-Options":       "nosniff",
		"X-Frame-Options":              "DENY",
		"X-XSS-Protection":             "1; mode=block",
		"Referrer-Policy":              "no-referrer",
		"Cross-Origin-Resource-Policy": "cross-origin",
	}

	for name, want := range headers {
		got := w.Header().Get(name)
		if got != want {
			t.Errorf("Security header %s = %s, want %s", name, got, want)
		}
	}
}

func TestServeHTTPCORS(t *testing.T) {
	tests := []struct {
		name           string
		origin         string
		allowAllCors   bool
		configOrigins  string
		wantCORSHeader bool
	}{
		{
			name:           "allow all - valid origin",
			origin:         "https://example.com",
			allowAllCors:   true,
			configOrigins:  "",
			wantCORSHeader: true,
		},
		{
			name:           "specific origin - allowed",
			origin:         "https://example.com",
			allowAllCors:   false,
			configOrigins:  "https://example.com",
			wantCORSHeader: true,
		},
		{
			name:           "specific origin - not allowed",
			origin:         "https://evil.com",
			allowAllCors:   false,
			configOrigins:  "https://example.com",
			wantCORSHeader: false,
		},
		{
			name:           "no origin header",
			origin:         "",
			allowAllCors:   false,
			configOrigins:  "https://example.com",
			wantCORSHeader: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New()
			s.SetCORSConfig(tt.configOrigins, tt.allowAllCors)

			req := httptest.NewRequest(http.MethodOptions, "/v1/calculate", http.NoBody)
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}
			w := httptest.NewRecorder()

			s.ServeHTTP(w, req)

			corsHeader := w.Header().Get("Access-Control-Allow-Origin")
			hasCORS := corsHeader != ""

			if hasCORS != tt.wantCORSHeader {
				t.Errorf("CORS header present = %v, want %v (header value: %s)", hasCORS, tt.wantCORSHeader, corsHeader)
			}

			if w.Code != http.StatusNoContent {
				t.Errorf("OPTIONS request status = %d, want %d", w.Code, http.StatusNoContent)
			}
		})
	}
}

func TestServeHTTPRouting(t *testing.T) {
	s := New()

	tests := []struct {
		name       string
		method     string
		path       string
		wantStatus int
	}{
		{
			name:       "health endpoint",
			method:     http.MethodGet,
			path:       "/health",
			wantStatus: http.StatusOK,
		},
		{
			name:       "calculate endpoint - wrong method",
			method:     http.MethodDelete,
			path:       "/v1/calculate",
			wantStatus: http.StatusMethodNotAllowed,
		},
		{
			name:       "not found",
			method:     http.MethodGet,
			path:       "/nonexistent",
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, http.NoBody)
			w := httptest.NewRecorder()

			s.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("ServeHTTP() status = %d, want %d", w.Code, tt.wantStatus)
			}
		})
	}
}

func TestParseRequest(t *testing.T) {
	s := New()

	tests := []struct {
		name    string
		body    string
		wantErr bool
	}{
		{
			name:    "valid request",
			body:    `{"url":"https://github.com/owner/repo/pull/123"}`,
			wantErr: false,
		},
		{
			name:    "valid request with config",
			body:    `{"url":"https://github.com/owner/repo/pull/123","config":{"annual_salary":300000}}`,
			wantErr: false,
		},
		{
			name:    "missing url",
			body:    `{}`,
			wantErr: true,
		},
		{
			name:    "invalid json",
			body:    `{invalid`,
			wantErr: true,
		},
		{
			name:    "invalid url format",
			body:    `{"url":"https://gitlab.com/owner/repo/pull/123"}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/v1/calculate", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")

			_, err := s.parseRequest(req.Context(), req)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseRequest() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestHandleCalculateNoToken(t *testing.T) {
	// Clear environment variables that could provide a fallback token
	// t.Setenv automatically restores the original value after the test
	t.Setenv("GITHUB_TOKEN", "")
	// Clear PATH to prevent gh CLI lookup
	t.Setenv("PATH", "")

	s := New()

	reqBody := CalculateRequest{
		URL: "https://github.com/owner/repo/pull/123",
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		t.Fatalf("Failed to marshal request: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/calculate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	// No Authorization header

	w := httptest.NewRecorder()
	s.handleCalculate(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("handleCalculate() without token status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestRateLimiting(t *testing.T) {
	s := New()
	s.SetRateLimit(1, 1) // Very low rate limit for testing

	// Test rate limiter directly to avoid actual GitHub API calls
	req1 := httptest.NewRequest(http.MethodPost, "/v1/calculate", http.NoBody)
	req1.RemoteAddr = "192.168.1.1:12345"

	// Get rate limiter for this IP
	limiter := s.limiter(req1.Context(), "192.168.1.1")

	// First request - allowed
	if !limiter.Allow() {
		t.Error("First request should not be rate limited")
	}

	// Second request from same IP should be rate limited
	if limiter.Allow() {
		t.Error("Second request should be rate limited")
	}
}

func TestSanitizeError(t *testing.T) {
	tests := []struct {
		name  string
		input error
		want  string
	}{
		{
			name:  "contains Bearer token",
			input: errors.New("error with Bearer ghp_1234567890abcdef1234567890abcdef123456"),
			want:  "error with [REDACTED_TOKEN]",
		},
		{
			name:  "contains token prefix",
			input: errors.New("error with token ghp_1234567890abcdef1234567890abcdef123456"),
			want:  "error with [REDACTED_TOKEN]",
		},
		{
			name:  "contains github_pat token",
			input: errors.New("error with github_pat_" + strings.Repeat("a", 82)),
			want:  "error with [REDACTED_TOKEN]",
		},
		{
			name:  "no token",
			input: errors.New("normal error message"),
			want:  "normal error message",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeError(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeError() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestConfigMerging(t *testing.T) {
	s := New()

	// Create a request with custom config
	reqBody := CalculateRequest{
		URL: "https://github.com/owner/repo/pull/123",
		Config: &cost.Config{
			AnnualSalary:       300000,
			BenefitsMultiplier: 1.4,
			EventDuration:      15 * time.Minute,
		},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		t.Fatalf("Failed to marshal request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/calculate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer fake_token")

	// Parse the request
	parsedReq, err := s.parseRequest(req.Context(), req)
	if err != nil {
		t.Fatalf("parseRequest() error = %v", err)
	}

	// Verify config values are present
	if parsedReq.Config.AnnualSalary != 300000 {
		t.Errorf("Config.AnnualSalary = %f, want 300000", parsedReq.Config.AnnualSalary)
	}
	if parsedReq.Config.BenefitsMultiplier != 1.4 {
		t.Errorf("Config.BenefitsMultiplier = %f, want 1.4", parsedReq.Config.BenefitsMultiplier)
	}
	if parsedReq.Config.EventDuration != 15*time.Minute {
		t.Errorf("Config.EventDuration = %v, want 15m", parsedReq.Config.EventDuration)
	}
}

// Test cache functions
func TestCachePRDataMemory(t *testing.T) {
	s := New()
	ctx := testContext()

	prData := cost.PRData{
		LinesAdded:   100,
		LinesDeleted: 50,
		Author:       "testuser",
		CreatedAt:    time.Now(),
	}

	key := "pr:https://github.com/owner/repo/pull/123"

	// Initially should not be cached
	_, cached := s.cachedPRData(ctx, key)
	if cached {
		t.Error("PR data should not be cached initially")
	}

	// Cache the data
	s.cachePRData(ctx, key, prData)

	// Should now be cached
	cachedData, cached := s.cachedPRData(ctx, key)
	if !cached {
		t.Error("PR data should be cached after caching")
	}

	if cachedData.LinesAdded != prData.LinesAdded {
		t.Errorf("Cached LinesAdded = %d, want %d", cachedData.LinesAdded, prData.LinesAdded)
	}
	if cachedData.Author != prData.Author {
		t.Errorf("Cached Author = %s, want %s", cachedData.Author, prData.Author)
	}
}

func TestCachePRQueryMemory(t *testing.T) {
	s := New()
	ctx := testContext()

	prs := []github.PRSummary{
		{Number: 123, Owner: "owner", Repo: "repo", Author: "testuser", UpdatedAt: time.Now()},
		{Number: 456, Owner: "owner", Repo: "repo", Author: "testuser2", UpdatedAt: time.Now()},
	}

	key := "repo:owner/repo:days=30"

	// Initially should not be cached
	_, cached := s.cachedPRQuery(ctx, key)
	if cached {
		t.Error("PR query should not be cached initially")
	}

	// Cache the query results
	s.cachePRQuery(ctx, key, prs)

	// Should now be cached
	cachedPRs, cached := s.cachedPRQuery(ctx, key)
	if !cached {
		t.Error("PR query should be cached after caching")
	}

	if len(cachedPRs) != len(prs) {
		t.Errorf("Cached PR count = %d, want %d", len(cachedPRs), len(prs))
	}
	if cachedPRs[0].Number != prs[0].Number {
		t.Errorf("Cached PR number = %d, want %d", cachedPRs[0].Number, prs[0].Number)
	}
}

func TestCacheKeyPrefixes(t *testing.T) {
	s := New()
	ctx := testContext()

	// Test different key prefixes
	tests := []struct {
		name string
		key  string
	}{
		{"PR key", "pr:https://github.com/owner/repo/pull/123"},
		{"Repo key", "repo:owner/repo:days=30"},
		{"Org key", "org:myorg:days=90"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prs := []github.PRSummary{{Number: 1}}
			s.cachePRQuery(ctx, tt.key, prs)

			cached, ok := s.cachedPRQuery(ctx, tt.key)
			if !ok {
				t.Errorf("Key %s should be cached", tt.key)
			}
			if len(cached) != 1 {
				t.Errorf("Expected 1 PR, got %d", len(cached))
			}
		})
	}
}

func TestHandleCalculateInvalidJSON(t *testing.T) {
	s := New()

	req := httptest.NewRequest(http.MethodPost, "/v1/calculate", strings.NewReader("{invalid json"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer ghp_test")

	w := httptest.NewRecorder()
	s.handleCalculate(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("handleCalculate() with invalid JSON status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleCalculateMissingURL(t *testing.T) {
	s := New()

	reqBody := CalculateRequest{} // No URL
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/v1/calculate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer ghp_test")

	w := httptest.NewRecorder()
	s.handleCalculate(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("handleCalculate() with missing URL status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleRepoSampleInvalidJSON(t *testing.T) {
	s := New()

	req := httptest.NewRequest(http.MethodPost, "/v1/repo-sample", strings.NewReader("{invalid json"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer ghp_test")

	w := httptest.NewRecorder()
	s.handleRepoSample(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("handleRepoSample() with invalid JSON status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleRepoSampleMissingFields(t *testing.T) {
	s := New()

	tests := []struct {
		name string
		body RepoSampleRequest
	}{
		{
			name: "missing owner",
			body: RepoSampleRequest{Repo: "repo", Days: 30},
		},
		{
			name: "missing repo",
			body: RepoSampleRequest{Owner: "owner", Days: 30},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.body)
			req := httptest.NewRequest(http.MethodPost, "/v1/repo-sample", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer ghp_test")

			w := httptest.NewRecorder()
			s.handleRepoSample(w, req)

			if w.Code != http.StatusBadRequest {
				t.Errorf("handleRepoSample() %s status = %d, want %d", tt.name, w.Code, http.StatusBadRequest)
			}
		})
	}
}

func TestHandleOrgSampleMissingOrg(t *testing.T) {
	s := New()

	reqBody := OrgSampleRequest{Days: 30} // Missing Org
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/v1/org-sample", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer ghp_test")

	w := httptest.NewRecorder()
	s.handleOrgSample(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("handleOrgSample() with missing org status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleRepoSampleStreamHeaders(t *testing.T) {
	s := New()

	reqBody := RepoSampleRequest{
		Owner: "testowner",
		Repo:  "testrepo",
		Days:  30,
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/v1/repo-sample-stream", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer ghp_test")

	w := httptest.NewRecorder()
	// Note: This will fail with no token error or GitHub API error, but we're testing headers
	s.handleRepoSampleStream(w, req)

	// Check SSE headers were set
	contentType := w.Header().Get("Content-Type")
	if contentType != "text/event-stream" {
		t.Errorf("Content-Type = %s, want text/event-stream", contentType)
	}

	cacheControl := w.Header().Get("Cache-Control")
	if cacheControl != "no-cache" {
		t.Errorf("Cache-Control = %s, want no-cache", cacheControl)
	}

	connection := w.Header().Get("Connection")
	if connection != "keep-alive" {
		t.Errorf("Connection = %s, want keep-alive", connection)
	}
}

func TestHandleOrgSampleStreamHeaders(t *testing.T) {
	s := New()

	reqBody := OrgSampleRequest{
		Org:  "testorg",
		Days: 30,
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/v1/org-sample-stream", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer ghp_test")

	w := httptest.NewRecorder()
	s.handleOrgSampleStream(w, req)

	// Check SSE headers were set
	contentType := w.Header().Get("Content-Type")
	if contentType != "text/event-stream" {
		t.Errorf("Content-Type = %s, want text/event-stream", contentType)
	}
}

func TestMergeConfig(t *testing.T) {
	s := New()

	baseConfig := cost.Config{
		AnnualSalary: 250000,
	}
	customConfig := &cost.Config{
		AnnualSalary: 300000,
	}

	merged := s.mergeConfig(baseConfig, customConfig)

	if merged.AnnualSalary != 300000 {
		t.Errorf("mergeConfig() AnnualSalary = %f, want 300000", merged.AnnualSalary)
	}
}

func TestHandleNotFound(t *testing.T) {
	s := New()

	req := httptest.NewRequest(http.MethodGet, "/invalid/path", http.NoBody)
	w := httptest.NewRecorder()

	s.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Invalid path status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestHandleMethodNotAllowed(t *testing.T) {
	s := New()

	// PATCH is not allowed on /v1/calculate
	req := httptest.NewRequest(http.MethodPatch, "/v1/calculate", http.NoBody)
	w := httptest.NewRecorder()

	s.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Wrong method status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestSetTokenValidationErrors(t *testing.T) {
	s := New()

	// Test with invalid app ID (empty)
	err := s.SetTokenValidation("", "nonexistent.pem")
	if err == nil {
		t.Error("SetTokenValidation() with empty app ID should return error")
	}

	// Test with nonexistent key file
	err = s.SetTokenValidation("12345", "/nonexistent/path/key.pem")
	if err == nil {
		t.Error("SetTokenValidation() with nonexistent key file should return error")
	}
}

func TestSetDataSource(t *testing.T) {
	s := New()

	tests := []struct {
		name       string
		source     string
		wantSource string
	}{
		{"prx source", "prx", "prx"},
		{"turnserver source", "turnserver", "turnserver"},
		{"invalid source falls back to prx", "custom", "prx"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s.SetDataSource(tt.source)
			if s.dataSource != tt.wantSource {
				t.Errorf("SetDataSource(%s) = %s, want %s", tt.source, s.dataSource, tt.wantSource)
			}
		})
	}
}

func TestLimiterConcurrency(t *testing.T) {
	s := New()
	s.SetRateLimit(10, 10)
	ctx := testContext()

	// Test that same IP gets same limiter (concurrency safe)
	limiter1 := s.limiter(ctx, "192.168.1.1")
	limiter2 := s.limiter(ctx, "192.168.1.1")

	if limiter1 != limiter2 {
		t.Error("Same IP should return same limiter instance")
	}

	// Test that different IPs get different limiters
	limiter3 := s.limiter(ctx, "192.168.1.2")
	if limiter1 == limiter3 {
		t.Error("Different IPs should return different limiters")
	}
}

func TestSanitizeErrorWithMultipleTokens(t *testing.T) {
	input := errors.New("error with Bearer ghp_token1 and token ghp_token2")
	result := sanitizeError(input)

	if strings.Contains(result, "ghp_") {
		t.Errorf("sanitizeError() still contains token: %s", result)
	}
	if !strings.Contains(result, "[REDACTED_TOKEN]") {
		t.Error("sanitizeError() should contain redaction marker")
	}
}

func TestAllowAllCorsFlag(t *testing.T) {
	s := New()
	s.SetCORSConfig("", true) // Allow all

	// Verify the allowAllCors flag is set
	if !s.allowAllCors {
		t.Error("SetCORSConfig with allowAll=true should set allowAllCors flag")
	}

	// When allowAll is false, flag should be false
	s.SetCORSConfig("https://example.com", false)
	if s.allowAllCors {
		t.Error("SetCORSConfig with allowAll=false should clear allowAllCors flag")
	}
}

func TestIsOriginAllowedEdgeCases(t *testing.T) {
	s := New()
	s.SetCORSConfig("https://example.com,https://*.test.com", false)

	tests := []struct {
		name   string
		origin string
		want   bool
	}{
		{"empty origin", "", false},
		{"case sensitive exact match", "https://Example.com", false},
		// Note: The wildcard matcher appears to match the base domain too
		{"wildcard matches base domain", "https://test.com", true},
		{"wildcard matches subdomain", "https://sub.test.com", true},
		{"wildcard ignores path", "https://sub.test.com/path", true}, // Path is stripped before matching
		{"unmatched domain", "https://other.com", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := s.isOriginAllowed(tt.origin)
			if got != tt.want {
				t.Errorf("isOriginAllowed(%q) = %v, want %v", tt.origin, got, tt.want)
			}
		})
	}
}

func TestRateLimiterBehavior(t *testing.T) {
	s := New()
	s.SetRateLimit(1, 2) // 1 per second, burst of 2
	ctx := testContext()

	limiter := s.limiter(ctx, "192.168.1.100")

	// First two requests should be allowed (burst)
	if !limiter.Allow() {
		t.Error("First request should be allowed (within burst)")
	}
	if !limiter.Allow() {
		t.Error("Second request should be allowed (within burst)")
	}

	// Third request should be rate limited
	if limiter.Allow() {
		t.Error("Third request should be rate limited (burst exhausted)")
	}
}

func TestValidateGitHubPRURLEdgeCases(t *testing.T) {
	s := New()

	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{"PR number zero", "https://github.com/owner/repo/pull/0", false},
		{"Large PR number", "https://github.com/owner/repo/pull/999999", false},
		{"Dashes in owner", "https://github.com/owner-name/repo/pull/123", false},
		{"Dashes in repo", "https://github.com/owner/repo-name/pull/123", false},
		{"Underscores rejected", "https://github.com/owner_name/repo_name/pull/123", true},
		{"Numbers in names", "https://github.com/owner123/repo456/pull/123", false},
		{"Dots in repo", "https://github.com/owner/repo.name/pull/123", false},
		{"Single char owner", "https://github.com/a/repo/pull/123", false},
		{"Single char repo", "https://github.com/owner/r/pull/123", false},
		{"Non-numeric PR number", "https://github.com/owner/repo/pull/abc", true},
		{"Negative PR number", "https://github.com/owner/repo/pull/-1", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := s.validateGitHubPRURL(tt.url)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateGitHubPRURL(%q) error = %v, wantErr %v", tt.url, err, tt.wantErr)
			}
		})
	}
}

func TestParseRequestEdgeCases(t *testing.T) {
	s := New()

	tests := []struct {
		name        string
		contentType string
		body        string
		wantErr     bool
	}{
		{
			name:        "empty body",
			contentType: "application/json",
			body:        "",
			wantErr:     true,
		},
		{
			name:        "whitespace only",
			contentType: "application/json",
			body:        "   ",
			wantErr:     true,
		},
		{
			name:        "null json",
			contentType: "application/json",
			body:        "null",
			wantErr:     true,
		},
		{
			name:        "array instead of object",
			contentType: "application/json",
			body:        "[]",
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/v1/calculate", strings.NewReader(tt.body))
			if tt.contentType != "" {
				req.Header.Set("Content-Type", tt.contentType)
			}

			_, err := s.parseRequest(req.Context(), req)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseRequest() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestCacheConcurrency(t *testing.T) {
	s := New()
	ctx := testContext()

	prData := cost.PRData{
		LinesAdded: 100,
		Author:     "testuser",
	}

	key := "pr:https://github.com/owner/repo/pull/123"

	// Test concurrent writes
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			s.cachePRData(ctx, key, prData)
			done <- true
		}()
	}

	// Wait for all writes
	for i := 0; i < 10; i++ {
		<-done
	}

	// Test concurrent reads
	for i := 0; i < 10; i++ {
		go func() {
			_, _ = s.cachedPRData(ctx, key)
			done <- true
		}()
	}

	// Wait for all reads
	for i := 0; i < 10; i++ {
		<-done
	}

	// Verify data is still correct
	cached, ok := s.cachedPRData(ctx, key)
	if !ok {
		t.Error("Data should still be cached after concurrent access")
	}
	if cached.LinesAdded != 100 {
		t.Errorf("Cached data corrupted: LinesAdded = %d, want 100", cached.LinesAdded)
	}
}

func TestExtractTokenVariations(t *testing.T) {
	s := New()

	tests := []struct {
		name        string
		authHeader  string
		wantToken   string
		description string
	}{
		{
			name:        "Bearer with single space",
			authHeader:  "Bearer ghp_token123",
			wantToken:   "ghp_token123",
			description: "Standard Bearer format",
		},
		{
			name:        "token prefix",
			authHeader:  "token ghp_token123",
			wantToken:   "ghp_token123",
			description: "Lowercase token prefix",
		},
		{
			name:        "plain token",
			authHeader:  "ghp_token123",
			wantToken:   "ghp_token123",
			description: "No prefix",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/v1/calculate", http.NoBody)
			req.Header.Set("Authorization", tt.authHeader)

			got := s.extractToken(req)
			if got != tt.wantToken {
				t.Errorf("extractToken() = %q, want %q (%s)", got, tt.wantToken, tt.description)
			}
		})
	}
}

// Helper function to create a test context
func testContext() context.Context {
	return context.Background()
}
