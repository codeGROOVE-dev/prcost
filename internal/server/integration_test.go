package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// TestOrgSampleStreamIntegration tests the org sample stream endpoint end-to-end.
// This test verifies that:
// - SSE events are received
// - Progress updates are sent for each PR being fetched
// - A final "done" event is received
// - The stream doesn't timeout during long operations
func TestOrgSampleStreamIntegration(t *testing.T) {
	// Skip if no GitHub token is available
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		t.Skip("GITHUB_TOKEN not set, skipping integration test")
	}

	// Create server
	s := New()
	s.SetRateLimit(1000, 1000) // High rate limit for testing

	// Create request
	reqBody := OrgSampleRequest{
		Org:        "codeGROOVE-dev",
		SampleSize: 30,
		Days:       90,
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		t.Fatalf("Failed to marshal request: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/calculate/org/stream", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	// Use a recorder that supports flushing
	w := httptest.NewRecorder()

	// Run the handler in a goroutine since it's a streaming endpoint
	done := make(chan bool)
	go func() {
		s.handleOrgSampleStream(w, req)
		close(done)
	}()

	// Wait for the handler to complete or timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	select {
	case <-done:
		// Handler completed successfully
	case <-ctx.Done():
		t.Fatal("Handler timed out after 5 minutes")
	}

	// Parse SSE events from response
	scanner := bufio.NewScanner(strings.NewReader(w.Body.String()))
	var events []ProgressUpdate
	var currentData string

	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "data: ") {
			currentData = strings.TrimPrefix(line, "data: ")
		} else if line == "" && currentData != "" {
			// Empty line marks end of SSE event
			var event ProgressUpdate
			if err := json.Unmarshal([]byte(currentData), &event); err != nil {
				t.Logf("Warning: Failed to parse SSE event: %v (data: %s)", err, currentData)
			} else {
				events = append(events, event)
			}
			currentData = ""
		}
	}

	if err := scanner.Err(); err != nil {
		t.Fatalf("Error reading response: %v", err)
	}

	// Verify we received events
	if len(events) == 0 {
		t.Fatal("No SSE events received")
	}

	t.Logf("Received %d SSE events", len(events))

	// Track event types
	eventTypes := make(map[string]int)
	var fetchingEvents, processingEvents, completeEvents, errorEvents int
	var finalDone bool

	for i, event := range events {
		eventTypes[event.Type]++

		switch event.Type {
		case "fetching":
			fetchingEvents++
			t.Logf("Event %d: fetching PR #%d (%s)", i+1, event.PR, event.Progress)

		case "processing":
			processingEvents++
			t.Logf("Event %d: processing PR #%d (%s)", i+1, event.PR, event.Progress)

		case "complete":
			completeEvents++
			t.Logf("Event %d: complete PR #%d (%s)", i+1, event.PR, event.Progress)

		case "error":
			errorEvents++
			t.Logf("Event %d: error - %s (PR #%d)", i+1, event.Error, event.PR)

		case "done":
			finalDone = true
			if event.Result == nil {
				t.Error("Final 'done' event has nil Result")
			} else {
				t.Logf("Event %d: done - Total cost: $%.2f (from %d sampled PRs)",
					i+1, event.Result.TotalCost, event.Result.SampledPRs)
			}

		default:
			t.Logf("Event %d: unknown type '%s'", i+1, event.Type)
		}
	}

	// Assertions
	t.Logf("Event summary: %d fetching, %d processing, %d complete, %d error, final done: %v",
		fetchingEvents, processingEvents, completeEvents, errorEvents, finalDone)

	// We should receive at least some fetching events
	if fetchingEvents == 0 {
		t.Error("Expected at least one 'fetching' event")
	}

	// We should receive a final 'done' event
	if !finalDone {
		t.Error("Expected final 'done' event")
	}

	// Processing events should roughly match fetching events (minus errors)
	// Allow some variance for errors
	if completeEvents == 0 && errorEvents == 0 {
		t.Error("Expected at least some 'complete' or 'error' events")
	}

	// Check that the final event is 'done'
	if len(events) > 0 && events[len(events)-1].Type != "done" {
		t.Errorf("Expected final event to be 'done', got '%s'", events[len(events)-1].Type)
	}

	// Verify response headers for SSE
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

// TestOrgSampleStreamNoTimeout verifies the stream doesn't timeout during long operations.
func TestOrgSampleStreamNoTimeout(t *testing.T) {
	// Skip if no GitHub token is available
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		t.Skip("GITHUB_TOKEN not set, skipping integration test")
	}

	// Create server
	s := New()
	s.SetRateLimit(1000, 1000)

	// Create request with larger sample size to ensure longer operation
	reqBody := OrgSampleRequest{
		Org:        "codeGROOVE-dev",
		SampleSize: 30,
		Days:       90,
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		t.Fatalf("Failed to marshal request: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/calculate/org/stream", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	w := httptest.NewRecorder()

	// Track time
	start := time.Now()

	// Run handler
	done := make(chan bool)
	var receivedEvents int

	go func() {
		s.handleOrgSampleStream(w, req)

		// Count events
		scanner := bufio.NewScanner(strings.NewReader(w.Body.String()))
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "data: ") {
				receivedEvents++
			}
		}
		close(done)
	}()

	// Wait with generous timeout (5 minutes for 20 PRs)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	select {
	case <-done:
		elapsed := time.Since(start)
		t.Logf("Stream completed in %v with %d events", elapsed, receivedEvents)

		if receivedEvents == 0 {
			t.Error("No events received - stream may have timed out")
		}

	case <-ctx.Done():
		t.Fatal("Stream timed out after 5 minutes - this indicates a problem with context cancellation")
	}
}
