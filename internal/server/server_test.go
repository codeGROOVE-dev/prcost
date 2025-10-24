package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/codeGROOVE-dev/prcost/pkg/cost"
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
			method:     http.MethodGet,
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
	limiter := s.getLimiter(req1.Context(), "192.168.1.1")

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
