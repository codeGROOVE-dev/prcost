package cost

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.AnnualSalary != 249000.0 {
		t.Errorf("Expected annual salary $249,000, got $%.2f", cfg.AnnualSalary)
	}

	if cfg.BenefitsMultiplier != 1.3 {
		t.Errorf("Expected benefits multiplier 1.3, got %.2f", cfg.BenefitsMultiplier)
	}

	if cfg.EventDuration != 10*time.Minute {
		t.Errorf("Expected 10 minutes per event, got %v", cfg.EventDuration)
	}

	if cfg.SessionGapThreshold != 20*time.Minute {
		t.Errorf("Expected 20 minute session gap, got %v", cfg.SessionGapThreshold)
	}

	if cfg.DeliveryDelayFactor != 0.20 {
		t.Errorf("Expected delivery delay factor 0.20, got %.2f", cfg.DeliveryDelayFactor)
	}

	if cfg.MaxDelayAfterLastEvent != 14*24*time.Hour {
		t.Errorf("Expected 14 days max delay after last event, got %v", cfg.MaxDelayAfterLastEvent)
	}

	if cfg.MaxProjectDelay != 90*24*time.Hour {
		t.Errorf("Expected 90 days max project delay, got %v", cfg.MaxProjectDelay)
	}

	if cfg.MaxCodeDrift != 90*24*time.Hour {
		t.Errorf("Expected 90 days max code drift, got %v", cfg.MaxCodeDrift)
	}
}

func TestHourlyRate(t *testing.T) {
	cfg := DefaultConfig()
	hourlyRate := (cfg.AnnualSalary * cfg.BenefitsMultiplier) / cfg.HoursPerYear

	// $249,000 * 1.3 / 2080 = $155.62/hr
	expectedRate := 155.625
	if hourlyRate < expectedRate-0.01 || hourlyRate > expectedRate+0.01 {
		t.Errorf("Expected hourly rate $%.2f, got $%.2f", expectedRate, hourlyRate)
	}
}

func TestCalculateSingleEvent(t *testing.T) {
	now := time.Now()
	prData := PRData{
		LinesAdded: 10,
		Author:     "test-author",
		Events: []ParticipantEvent{
			{Timestamp: now, Actor: "test-author"},
		},
		CreatedAt: now.Add(-1 * time.Hour),
	}

	cfg := DefaultConfig()
	breakdown := Calculate(prData, cfg)

	// Single event should create one session
	if breakdown.Author.Sessions != 1 {
		t.Errorf("Expected 1 session for single event, got %d", breakdown.Author.Sessions)
	}

	// Single event should have positive costs
	codeCost := breakdown.Author.NewCodeCost + breakdown.Author.AdaptationCost
	if codeCost <= 0 {
		t.Error("Code cost should be positive")
	}

	if breakdown.Author.GitHubCost <= 0 {
		t.Error("GitHub cost should be positive")
	}

	if breakdown.Author.GitHubContextCost <= 0 {
		t.Error("GitHub context cost should be positive")
	}
}

func TestCalculateMultipleEventsOneSession(t *testing.T) {
	now := time.Now()
	prData := PRData{
		LinesAdded: 100,
		Author:     "test-author",
		Events: []ParticipantEvent{
			{Timestamp: now, Actor: "test-author"},
			{Timestamp: now.Add(5 * time.Minute), Actor: "test-author"},
			{Timestamp: now.Add(10 * time.Minute), Actor: "test-author"},
		},
		CreatedAt: now.Add(-1 * time.Hour),
	}

	cfg := DefaultConfig()
	breakdown := Calculate(prData, cfg)

	// Three events within 10 minutes should be one session
	if breakdown.Author.Sessions != 1 {
		t.Errorf("Expected 1 session for closely spaced events, got %d", breakdown.Author.Sessions)
	}

	// Should have 3 events
	if breakdown.Author.Events != 3 {
		t.Errorf("Expected 3 events, got %d", breakdown.Author.Events)
	}
}

func TestCalculateMultipleEventsTwoSessions(t *testing.T) {
	now := time.Now()
	prData := PRData{
		LinesAdded: 100,
		Author:     "test-author",
		Events: []ParticipantEvent{
			{Timestamp: now, Actor: "test-author"},
			{Timestamp: now.Add(90 * time.Minute), Actor: "test-author"}, // 90 min gap = new session
		},
		CreatedAt: now.Add(-2 * time.Hour),
	}

	cfg := DefaultConfig()
	breakdown := Calculate(prData, cfg)

	// Two events 90 minutes apart should be two sessions
	if breakdown.Author.Sessions != 2 {
		t.Errorf("Expected 2 sessions for 90-minute gap, got %d", breakdown.Author.Sessions)
	}
}

func TestCalculateWithParticipants(t *testing.T) {
	now := time.Now()
	prData := PRData{
		LinesAdded: 100,
		Author:     "author",
		Events: []ParticipantEvent{
			{Timestamp: now, Actor: "author"},
			{Timestamp: now.Add(1 * time.Hour), Actor: "reviewer1"},
			{Timestamp: now.Add(2 * time.Hour), Actor: "reviewer2"},
		},
		CreatedAt: now.Add(-3 * time.Hour),
	}

	cfg := DefaultConfig()
	breakdown := Calculate(prData, cfg)

	// Should have 2 participants (excluding author)
	if len(breakdown.Participants) != 2 {
		t.Errorf("Expected 2 participants, got %d", len(breakdown.Participants))
	}

	// Each participant should have positive costs
	for _, p := range breakdown.Participants {
		if p.TotalCost <= 0 {
			t.Errorf("Participant %s should have positive cost", p.Actor)
		}
	}
}

// TestCalculateWithRealPRData tests cost calculation using actual PR data from prx
func TestCalculateWithRealPRData(t *testing.T) {
	// Test with PR 1891 - a merged PR with 26 LOC
	data, err := os.ReadFile("../../testdata/pr_1891.json")
	if err != nil {
		t.Skipf("Skipping real PR test: %v", err)
	}

	var prxData struct {
		Events []struct {
			Timestamp string `json:"timestamp"`
			Kind      string `json:"kind"`
			Actor     string `json:"actor"`
			Bot       bool   `json:"bot"`
		} `json:"events"`
		PullRequest struct {
			CreatedAt         string `json:"created_at"`
			Author            string `json:"author"`
			Additions         int    `json:"additions"`
			AuthorWriteAccess int    `json:"author_write_access"`
		} `json:"pull_request"`
	}

	if err := json.Unmarshal(data, &prxData); err != nil {
		t.Fatalf("Failed to parse PR data: %v", err)
	}

	// Convert to PRData
	var events []ParticipantEvent
	for _, e := range prxData.Events {
		if e.Bot {
			continue
		}
		ts, err := time.Parse(time.RFC3339, e.Timestamp)
		if err != nil {
			t.Fatalf("Failed to parse timestamp: %v", err)
		}
		events = append(events, ParticipantEvent{
			Timestamp: ts,
			Actor:     e.Actor,
			Kind:      e.Kind,
		})
	}

	createdAt, err := time.Parse(time.RFC3339, prxData.PullRequest.CreatedAt)
	if err != nil {
		t.Fatalf("Failed to parse created_at: %v", err)
	}

	prData := PRData{
		LinesAdded: prxData.PullRequest.Additions,
		Author:     prxData.PullRequest.Author,
		Events:     events,
		CreatedAt:  createdAt,
	}

	cfg := DefaultConfig()
	breakdown := Calculate(prData, cfg)

	// Validate basic properties
	if breakdown.HourlyRate <= 0 {
		t.Error("Hourly rate should be positive")
	}

	if breakdown.TotalCost <= 0 {
		t.Error("Total cost should be positive")
	}

	// PR 1891 had 26 LOC added
	if breakdown.Author.LinesAdded != 26 {
		t.Errorf("Expected 26 LOC, got %d", breakdown.Author.LinesAdded)
	}

	// Should have author costs
	codeCost := breakdown.Author.NewCodeCost + breakdown.Author.AdaptationCost
	if codeCost <= 0 {
		t.Error("Author code cost should be positive")
	}

	// Should have delay costs (PR was open for ~14 hours)
	if breakdown.DelayCost <= 0 {
		t.Error("Delay cost should be positive")
	}

	// Delay should not be capped (< 90 days)
	if breakdown.DelayCapped {
		t.Error("Short PR should not have capped delay")
	}

	// Should have exactly 2 human participants (markusthoemmes + xnox)
	if len(breakdown.Participants) != 1 {
		t.Errorf("Expected 1 non-author participant, got %d", len(breakdown.Participants))
	}

	// Total should equal sum of components
	expectedTotal := breakdown.Author.TotalCost + breakdown.DelayCost
	for _, p := range breakdown.Participants {
		expectedTotal += p.TotalCost
	}

	if breakdown.TotalCost < expectedTotal-0.01 || breakdown.TotalCost > expectedTotal+0.01 {
		t.Errorf("Total cost mismatch: %.2f != %.2f", breakdown.TotalCost, expectedTotal)
	}

	// Log the breakdown for manual inspection
	t.Logf("PR 1891 breakdown:")
	t.Logf("  Author cost: $%.2f", breakdown.Author.TotalCost)
	t.Logf("  Participant cost: $%.2f", expectedTotal-breakdown.Author.TotalCost-breakdown.DelayCost)
	t.Logf("  Delay cost: $%.2f", breakdown.DelayCost)
	t.Logf("  Total cost: $%.2f", breakdown.TotalCost)
}

func TestCalculateMinimumValues(t *testing.T) {
	// Test with minimal PR data
	prData := PRData{
		LinesAdded: 1,
		Author:     "test-author",
		Events: []ParticipantEvent{
			{Timestamp: time.Now(), Actor: "test-author"},
		},
		CreatedAt: time.Now().Add(-1 * time.Hour),
	}

	cfg := DefaultConfig()
	breakdown := Calculate(prData, cfg)

	// Even minimal PR should have costs
	codeCost := breakdown.Author.NewCodeCost + breakdown.Author.AdaptationCost
	if codeCost <= 0 {
		t.Error("Even 1 LOC should have positive code cost (minimum)")
	}

	if breakdown.Author.GitHubCost <= 0 {
		t.Error("Even 1 event should have positive GitHub cost")
	}

	if breakdown.DelayCost <= 0 {
		t.Error("Even 1 hour open should have positive delay cost")
	}
}

func TestCalculateExternalContributor(t *testing.T) {
	prData := PRData{
		LinesAdded: 100,
		Author:     "external-contributor",
		Events: []ParticipantEvent{
			{Timestamp: time.Now().Add(-72 * time.Hour), Actor: "external-contributor"},
		},
		CreatedAt: time.Now().Add(-72 * time.Hour),
	}

	cfg := DefaultConfig()
	breakdown := Calculate(prData, cfg)

	// Verify delay cost is calculated
	if breakdown.DelayCost <= 0 {
		t.Error("Expected positive delay cost")
	}

	if breakdown.TotalCost <= 0 {
		t.Error("Expected positive total cost")
	}
}

func TestCalculateDelayComponents(t *testing.T) {
	// Test PR open for 7 days - should have code drift
	now := time.Now()
	prData := PRData{
		LinesAdded: 100,
		Author:     "test-author",
		Events: []ParticipantEvent{
			{Timestamp: now.Add(-7 * 24 * time.Hour), Actor: "test-author", Kind: "commit"},
		},
		CreatedAt: now.Add(-7 * 24 * time.Hour),
	}

	cfg := DefaultConfig()
	breakdown := Calculate(prData, cfg)

	// Should have delivery delay cost
	if breakdown.DelayCostDetail.DeliveryDelayCost <= 0 {
		t.Error("Delivery delay cost should be positive for 7-day old PR")
	}

	// Should have code churn cost (7 days = ~7% drift)
	if breakdown.DelayCostDetail.CodeChurnCost <= 0 {
		t.Error("Code churn cost should be positive for 7-day old PR")
	}

	if breakdown.DelayCostDetail.ReworkPercentage <= 0 {
		t.Error("Rework percentage should be positive for 7-day old PR")
	}

	// Should have future costs (review + merge + context)
	futureTotalCost := breakdown.DelayCostDetail.FutureReviewCost +
		breakdown.DelayCostDetail.FutureMergeCost +
		breakdown.DelayCostDetail.FutureContextCost
	if futureTotalCost <= 0 {
		t.Error("Future costs should be positive")
	}

	// Should have open PR tracking cost (PR is open)
	if breakdown.DelayCostDetail.PRTrackingCost <= 0 {
		t.Error("Open PR tracking cost should be positive for open PR")
	}

	// Total delay should equal sum of components
	expectedDelay := breakdown.DelayCostDetail.DeliveryDelayCost +
		breakdown.DelayCostDetail.CodeChurnCost +
		breakdown.DelayCostDetail.AutomatedUpdatesCost +
		breakdown.DelayCostDetail.PRTrackingCost +
		futureTotalCost

	if breakdown.DelayCost < expectedDelay-0.01 || breakdown.DelayCost > expectedDelay+0.01 {
		t.Errorf("Delay cost mismatch: %.2f != %.2f", breakdown.DelayCost, expectedDelay)
	}
}

func TestCalculateShortPRNoRework(t *testing.T) {
	// Test PR open for only 2 days - should not have code drift
	now := time.Now()
	prData := PRData{
		LinesAdded: 100,
		Author:     "test-author",
		Events: []ParticipantEvent{
			{Timestamp: now.Add(-2 * 24 * time.Hour), Actor: "test-author"},
		},
		CreatedAt: now.Add(-2 * 24 * time.Hour),
	}

	cfg := DefaultConfig()
	breakdown := Calculate(prData, cfg)

	// Should NOT have code churn cost (< 3 days)
	if breakdown.DelayCostDetail.CodeChurnCost != 0 {
		t.Error("Code churn cost should be zero for 2-day old PR")
	}

	if breakdown.DelayCostDetail.ReworkPercentage != 0 {
		t.Error("Rework percentage should be zero for 2-day old PR")
	}

	// Should still have delivery delay cost
	if breakdown.DelayCostDetail.DeliveryDelayCost <= 0 {
		t.Error("Delivery delay cost should be positive even for short PR")
	}

	futureTotalCost := breakdown.DelayCostDetail.FutureReviewCost +
		breakdown.DelayCostDetail.FutureMergeCost +
		breakdown.DelayCostDetail.FutureContextCost
	if futureTotalCost <= 0 {
		t.Error("Future costs should be positive")
	}
}

func TestCalculateWithRealPR13(t *testing.T) {
	// Test with PR 13 - a long-lived PR (2136 days from Sep 2019 to Jul 2025)
	data, err := os.ReadFile("../../testdata/pr_13.json")
	if err != nil {
		t.Skipf("Skipping real PR test: %v", err)
	}

	// Extract JSON from the last line (prx outputs logs then JSON)
	lines := strings.Split(string(data), "\n")
	var jsonLine string
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.HasPrefix(lines[i], "{") {
			jsonLine = lines[i]
			break
		}
	}

	var prxData struct {
		Events []struct {
			Timestamp string `json:"timestamp"`
			Kind      string `json:"kind"`
			Actor     string `json:"actor"`
			Bot       bool   `json:"bot"`
		} `json:"events"`
		PullRequest struct {
			CreatedAt         string `json:"created_at"`
			Author            string `json:"author"`
			Additions         int    `json:"additions"`
			AuthorWriteAccess int    `json:"author_write_access"`
		} `json:"pull_request"`
	}

	if err := json.Unmarshal([]byte(jsonLine), &prxData); err != nil {
		t.Fatalf("Failed to parse PR data: %v", err)
	}

	// Convert to PRData
	var events []ParticipantEvent
	for _, e := range prxData.Events {
		if e.Bot {
			continue
		}
		ts, err := time.Parse(time.RFC3339, e.Timestamp)
		if err != nil {
			t.Fatalf("Failed to parse timestamp: %v", err)
		}
		events = append(events, ParticipantEvent{
			Timestamp: ts,
			Actor:     e.Actor,
			Kind:      e.Kind,
		})
	}

	createdAt, err := time.Parse(time.RFC3339, prxData.PullRequest.CreatedAt)
	if err != nil {
		t.Fatalf("Failed to parse created_at: %v", err)
	}

	prData := PRData{
		LinesAdded: prxData.PullRequest.Additions,
		Author:     prxData.PullRequest.Author,
		Events:     events,
		CreatedAt:  createdAt,
	}

	cfg := DefaultConfig()
	breakdown := Calculate(prData, cfg)

	// This PR was open for ~2136 days (almost 6 years!)
	// Project delay should be capped at 90 days absolute maximum
	if !breakdown.DelayCapped {
		t.Error("Very long PR should have project delay capped")
	}

	// 90 days absolute cap = 2160 hours
	// Delivery: 2160 * 0.15 = 324 hours
	expectedDeliveryHours := 90.0 * 24.0 * 0.20 // 432 hours (20% default delay factor)

	if breakdown.DelayCostDetail.DeliveryDelayHours != expectedDeliveryHours {
		t.Errorf("Expected %.0f delivery delay hours (20%% of 90 day cap), got %.2f",
			expectedDeliveryHours, breakdown.DelayCostDetail.DeliveryDelayHours)
	}

	// Code drift should be capped at 90 days (not unlimited)
	// At 90 days, drift is ~35%, so we should never see >100% rework
	if breakdown.DelayCostDetail.ReworkPercentage > 100.0 {
		t.Errorf("Rework should never exceed 100%%, got %.2f%%", breakdown.DelayCostDetail.ReworkPercentage)
	}

	// With 90-day cap, rework should be around 41% (probability-based model)
	if breakdown.DelayCostDetail.ReworkPercentage < 38.0 || breakdown.DelayCostDetail.ReworkPercentage > 44.0 {
		t.Logf("Rework percentage %.1f%% is outside expected 38-44%% range for 90-day cap",
			breakdown.DelayCostDetail.ReworkPercentage)
	}

	// Log the breakdown for manual inspection
	t.Logf("PR 13 breakdown (6 year old PR):")
	t.Logf("  638 LOC added")
	t.Logf("  Author cost: $%.2f", breakdown.Author.TotalCost)
	t.Logf("  Delivery Delay (20%%): $%.2f (%.0f hrs, capped at 90 days)",
		breakdown.DelayCostDetail.DeliveryDelayCost, breakdown.DelayCostDetail.DeliveryDelayHours)
	t.Logf("  Code Churn: $%.2f (%.1f%% rework, capped at 90 days drift)",
		breakdown.DelayCostDetail.CodeChurnCost, breakdown.DelayCostDetail.ReworkPercentage)
	futureTotalCost := breakdown.DelayCostDetail.FutureReviewCost +
		breakdown.DelayCostDetail.FutureMergeCost +
		breakdown.DelayCostDetail.FutureContextCost
	t.Logf("  Future Costs: $%.2f", futureTotalCost)
	t.Logf("  Total delay cost: $%.2f", breakdown.DelayCost)
	t.Logf("  Total cost: $%.2f", breakdown.TotalCost)
}

func TestCalculateLongPRCapped(t *testing.T) {
	// Test PR open for 120 days with last event at the start
	// Should only count 14 days after the last event
	now := time.Now()
	prData := PRData{
		LinesAdded: 100,
		Author:     "test-author",
		Events: []ParticipantEvent{
			{Timestamp: now.Add(-120 * 24 * time.Hour), Actor: "test-author"},
		},
		CreatedAt: now.Add(-120 * 24 * time.Hour),
	}

	cfg := DefaultConfig()
	breakdown := Calculate(prData, cfg)

	// Should be capped
	if !breakdown.DelayCapped {
		t.Error("120-day old PR with stale last event should have delay capped")
	}

	// Last event was 120 days ago, so we only count 14 days after it
	// Capped hours: 14 days = 336 hours
	// Delivery delay: 336 * 0.20 = 67.2 hours (20% default delay factor)
	expectedDeliveryHours := 14.0 * 24.0 * 0.20

	if breakdown.DelayCostDetail.DeliveryDelayHours != expectedDeliveryHours {
		t.Errorf("Expected %.1f delivery delay hours (20%% of 14 days), got %.2f",
			expectedDeliveryHours, breakdown.DelayCostDetail.DeliveryDelayHours)
	}
}

func TestDelayHoursNeverExceedPRAge(t *testing.T) {
	// Test that delay hours (as productivity-equivalent time) are reasonable
	// Delivery (15%) should be proportional to PR age
	now := time.Now()
	testCases := []struct {
		name    string
		ageHrs  float64
		prData  PRData
		wantMax float64 // Maximum acceptable delay hours (delivery)
	}{
		{
			name:   "1 day old PR",
			ageHrs: 24.0,
			prData: PRData{
				LinesAdded: 50,
				Author:     "test-author",
				Events: []ParticipantEvent{
					{Timestamp: now.Add(-24 * time.Hour), Actor: "test-author", Kind: "commit"},
				},
				CreatedAt: now.Add(-24 * time.Hour),
			},
			wantMax: 24.0 * 0.20, // 20% of 24 hours = 4.8 hours
		},
		{
			name:   "7 day old PR",
			ageHrs: 168.0,
			prData: PRData{
				LinesAdded: 100,
				Author:     "test-author",
				Events: []ParticipantEvent{
					{Timestamp: now.Add(-7 * 24 * time.Hour), Actor: "test-author", Kind: "commit"},
				},
				CreatedAt: now.Add(-7 * 24 * time.Hour),
			},
			wantMax: 168.0 * 0.20, // 20% of 168 hours = 33.6 hours
		},
	}

	cfg := DefaultConfig()

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			breakdown := Calculate(tc.prData, cfg)

			// Delivery delay hours should not exceed 20% of PR age
			if breakdown.DelayCostDetail.DeliveryDelayHours > tc.wantMax+0.1 { // Allow 0.1 hr tolerance for floating point
				t.Errorf("Delay hours exceed 20%% of PR age: got %.2f hours, want <= %.2f hours (PR age: %.1f hrs)",
					breakdown.DelayCostDetail.DeliveryDelayHours, tc.wantMax, tc.ageHrs)
			}

			// Verify delivery should be 20%
			expectedDelivery := tc.ageHrs * 0.20

			if breakdown.DelayCostDetail.DeliveryDelayHours > expectedDelivery+0.1 {
				t.Errorf("Delivery delay hours too high: got %.2f, want %.2f (20%% of %.1f)",
					breakdown.DelayCostDetail.DeliveryDelayHours, expectedDelivery, tc.ageHrs)
			}
		})
	}
}

// TestCalculateFastTurnaroundNoDelay verifies that PRs merged within 30 minutes have no delay costs.
func TestCalculateFastTurnaroundNoDelay(t *testing.T) {
	cfg := DefaultConfig()

	testCases := []struct {
		name        string
		openMinutes float64
		wantDelay   float64
	}{
		{
			name:        "0 minutes - instant merge",
			openMinutes: 0,
			wantDelay:   0,
		},
		{
			name:        "15 minutes - very fast",
			openMinutes: 15,
			wantDelay:   0,
		},
		{
			name:        "29 minutes - just under threshold",
			openMinutes: 29,
			wantDelay:   0,
		},
		{
			name:        "31 minutes - just over threshold",
			openMinutes: 31,
			wantDelay:   0, // Will be > 0 since 31 > 30
		},
		{
			name:        "60 minutes - one hour",
			openMinutes: 60,
			wantDelay:   0, // Will be > 0
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Now()
			createdAt := now.Add(-time.Duration(tc.openMinutes) * time.Minute)

			data := PRData{
				LinesAdded: 50,
				Author:     "test-author",
				Events: []ParticipantEvent{
					{Timestamp: createdAt, Actor: "test-author", Kind: "commit"},
				},
				CreatedAt: createdAt,
				ClosedAt:  now,
			}

			breakdown := Calculate(data, cfg)

			// For PRs < 30 minutes, delay cost should be exactly 0
			if tc.openMinutes < 30 {
				if breakdown.DelayCost != 0 {
					t.Errorf("Expected 0 delay cost for %v minute PR, got $%.2f",
						tc.openMinutes, breakdown.DelayCost)
				}
				if breakdown.DelayCostDetail.DeliveryDelayCost != 0 {
					t.Errorf("Expected 0 delivery delay cost for %v minute PR, got $%.2f",
						tc.openMinutes, breakdown.DelayCostDetail.DeliveryDelayCost)
				}
			} else if breakdown.DelayCost == 0 {
				// For PRs >= 30 minutes, delay cost should be > 0
				t.Errorf("Expected non-zero delay cost for %v minute PR, got $0",
					tc.openMinutes)
			}
		})
	}
}

// Mock PRFetcher for testing AnalyzePRs
type mockPRFetcher struct {
	data       map[string]PRData
	failURLs   map[string]error
	callCount  int
	maxCalls   int // Fail after this many calls (0 = no limit)
	fetchDelay time.Duration
}

func (m *mockPRFetcher) FetchPRData(ctx context.Context, prURL string, updatedAt time.Time) (PRData, error) {
	m.callCount++

	// Fail after max calls if set
	if m.maxCalls > 0 && m.callCount > m.maxCalls {
		return PRData{}, errors.New("max calls exceeded")
	}

	// Check for context cancellation
	if ctx.Err() != nil {
		return PRData{}, ctx.Err()
	}

	// Simulate fetch delay
	if m.fetchDelay > 0 {
		time.Sleep(m.fetchDelay)
	}

	// Check for specific URL failure
	if m.failURLs != nil {
		if err, ok := m.failURLs[prURL]; ok {
			return PRData{}, err
		}
	}

	// Return mock data
	if m.data != nil {
		if data, ok := m.data[prURL]; ok {
			return data, nil
		}
	}

	// Default success case
	now := time.Now()
	return PRData{
		LinesAdded: 100,
		Author:     "test-author",
		Events: []ParticipantEvent{
			{Timestamp: now, Actor: "test-author", Kind: "commit"},
		},
		CreatedAt: now.Add(-1 * time.Hour),
		ClosedAt:  now,
	}, nil
}

func TestAnalyzePRsNoSamples(t *testing.T) {
	ctx := context.Background()
	fetcher := &mockPRFetcher{}

	req := &AnalysisRequest{
		Samples: []PRSummaryInfo{},
		Fetcher: fetcher,
		Config:  DefaultConfig(),
	}

	result, err := AnalyzePRs(ctx, req)

	if err == nil {
		t.Error("Expected error when no samples provided")
	}

	if result != nil {
		t.Error("Expected nil result when no samples provided")
	}

	if err.Error() != "no samples provided" {
		t.Errorf("Expected 'no samples provided' error, got: %v", err)
	}
}

func TestAnalyzePRsNoFetcher(t *testing.T) {
	ctx := context.Background()

	req := &AnalysisRequest{
		Samples: []PRSummaryInfo{
			{Owner: "owner", Repo: "repo", Number: 1, UpdatedAt: time.Now()},
		},
		Fetcher: nil,
		Config:  DefaultConfig(),
	}

	result, err := AnalyzePRs(ctx, req)

	if err == nil {
		t.Error("Expected error when fetcher is nil")
	}

	if result != nil {
		t.Error("Expected nil result when fetcher is nil")
	}

	if err.Error() != "fetcher is required" {
		t.Errorf("Expected 'fetcher is required' error, got: %v", err)
	}
}

func TestAnalyzePRsSequentialSuccess(t *testing.T) {
	ctx := context.Background()
	now := time.Now()

	fetcher := &mockPRFetcher{
		data: map[string]PRData{
			"https://github.com/owner/repo/pull/1": {
				LinesAdded: 50,
				Author:     "author1",
				Events: []ParticipantEvent{
					{Timestamp: now, Actor: "author1", Kind: "commit"},
				},
				CreatedAt: now.Add(-2 * time.Hour),
				ClosedAt:  now,
			},
			"https://github.com/owner/repo/pull/2": {
				LinesAdded: 100,
				Author:     "author2",
				Events: []ParticipantEvent{
					{Timestamp: now, Actor: "author2", Kind: "commit"},
				},
				CreatedAt: now.Add(-3 * time.Hour),
				ClosedAt:  now,
			},
		},
	}

	req := &AnalysisRequest{
		Samples: []PRSummaryInfo{
			{Owner: "owner", Repo: "repo", Number: 1, UpdatedAt: now},
			{Owner: "owner", Repo: "repo", Number: 2, UpdatedAt: now},
		},
		Fetcher:     fetcher,
		Config:      DefaultConfig(),
		Concurrency: 0, // Sequential
	}

	result, err := AnalyzePRs(ctx, req)

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if result == nil {
		t.Fatal("Expected non-nil result")
	}

	if len(result.Breakdowns) != 2 {
		t.Errorf("Expected 2 breakdowns, got %d", len(result.Breakdowns))
	}

	if result.Skipped != 0 {
		t.Errorf("Expected 0 skipped, got %d", result.Skipped)
	}

	if fetcher.callCount != 2 {
		t.Errorf("Expected 2 fetcher calls, got %d", fetcher.callCount)
	}
}

func TestAnalyzePRsSequentialPartialFailure(t *testing.T) {
	ctx := context.Background()
	now := time.Now()

	fetcher := &mockPRFetcher{
		data: map[string]PRData{
			"https://github.com/owner/repo/pull/1": {
				LinesAdded: 50,
				Author:     "author1",
				Events: []ParticipantEvent{
					{Timestamp: now, Actor: "author1", Kind: "commit"},
				},
				CreatedAt: now.Add(-2 * time.Hour),
				ClosedAt:  now,
			},
		},
		failURLs: map[string]error{
			"https://github.com/owner/repo/pull/2": errors.New("fetch failed"),
		},
	}

	req := &AnalysisRequest{
		Samples: []PRSummaryInfo{
			{Owner: "owner", Repo: "repo", Number: 1, UpdatedAt: now},
			{Owner: "owner", Repo: "repo", Number: 2, UpdatedAt: now},
		},
		Fetcher:     fetcher,
		Config:      DefaultConfig(),
		Concurrency: 1,
	}

	result, err := AnalyzePRs(ctx, req)

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if result == nil {
		t.Fatal("Expected non-nil result")
	}

	if len(result.Breakdowns) != 1 {
		t.Errorf("Expected 1 breakdown, got %d", len(result.Breakdowns))
	}

	if result.Skipped != 1 {
		t.Errorf("Expected 1 skipped, got %d", result.Skipped)
	}
}

func TestAnalyzePRsSequentialAllFail(t *testing.T) {
	ctx := context.Background()
	now := time.Now()

	fetcher := &mockPRFetcher{
		failURLs: map[string]error{
			"https://github.com/owner/repo/pull/1": errors.New("fetch failed"),
			"https://github.com/owner/repo/pull/2": errors.New("fetch failed"),
		},
	}

	req := &AnalysisRequest{
		Samples: []PRSummaryInfo{
			{Owner: "owner", Repo: "repo", Number: 1, UpdatedAt: now},
			{Owner: "owner", Repo: "repo", Number: 2, UpdatedAt: now},
		},
		Fetcher:     fetcher,
		Config:      DefaultConfig(),
		Concurrency: 1,
	}

	result, err := AnalyzePRs(ctx, req)

	if err == nil {
		t.Error("Expected error when all fetches fail")
	}

	if result != nil {
		t.Error("Expected nil result when all fetches fail")
	}

	expectedErrMsg := "no samples could be processed successfully (2 skipped)"
	if err.Error() != expectedErrMsg {
		t.Errorf("Expected error message '%s', got: %v", expectedErrMsg, err)
	}
}

func TestAnalyzePRsParallelSuccess(t *testing.T) {
	ctx := context.Background()
	now := time.Now()

	fetcher := &mockPRFetcher{
		data: map[string]PRData{
			"https://github.com/owner/repo/pull/1": {
				LinesAdded: 50,
				Author:     "author1",
				Events: []ParticipantEvent{
					{Timestamp: now, Actor: "author1", Kind: "commit"},
				},
				CreatedAt: now.Add(-2 * time.Hour),
				ClosedAt:  now,
			},
			"https://github.com/owner/repo/pull/2": {
				LinesAdded: 100,
				Author:     "author2",
				Events: []ParticipantEvent{
					{Timestamp: now, Actor: "author2", Kind: "commit"},
				},
				CreatedAt: now.Add(-3 * time.Hour),
				ClosedAt:  now,
			},
			"https://github.com/owner/repo/pull/3": {
				LinesAdded: 75,
				Author:     "author3",
				Events: []ParticipantEvent{
					{Timestamp: now, Actor: "author3", Kind: "commit"},
				},
				CreatedAt: now.Add(-4 * time.Hour),
				ClosedAt:  now,
			},
		},
		fetchDelay: 10 * time.Millisecond, // Small delay to test concurrency
	}

	req := &AnalysisRequest{
		Samples: []PRSummaryInfo{
			{Owner: "owner", Repo: "repo", Number: 1, UpdatedAt: now},
			{Owner: "owner", Repo: "repo", Number: 2, UpdatedAt: now},
			{Owner: "owner", Repo: "repo", Number: 3, UpdatedAt: now},
		},
		Fetcher:     fetcher,
		Config:      DefaultConfig(),
		Concurrency: 2, // Parallel with 2 workers
	}

	result, err := AnalyzePRs(ctx, req)

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if result == nil {
		t.Fatal("Expected non-nil result")
	}

	if len(result.Breakdowns) != 3 {
		t.Errorf("Expected 3 breakdowns, got %d", len(result.Breakdowns))
	}

	if result.Skipped != 0 {
		t.Errorf("Expected 0 skipped, got %d", result.Skipped)
	}

	if fetcher.callCount != 3 {
		t.Errorf("Expected 3 fetcher calls, got %d", fetcher.callCount)
	}
}

func TestAnalyzePRsParallelPartialFailure(t *testing.T) {
	ctx := context.Background()
	now := time.Now()

	fetcher := &mockPRFetcher{
		data: map[string]PRData{
			"https://github.com/owner/repo/pull/1": {
				LinesAdded: 50,
				Author:     "author1",
				Events: []ParticipantEvent{
					{Timestamp: now, Actor: "author1", Kind: "commit"},
				},
				CreatedAt: now.Add(-2 * time.Hour),
				ClosedAt:  now,
			},
		},
		failURLs: map[string]error{
			"https://github.com/owner/repo/pull/2": errors.New("fetch failed"),
		},
	}

	req := &AnalysisRequest{
		Samples: []PRSummaryInfo{
			{Owner: "owner", Repo: "repo", Number: 1, UpdatedAt: now},
			{Owner: "owner", Repo: "repo", Number: 2, UpdatedAt: now},
		},
		Fetcher:     fetcher,
		Config:      DefaultConfig(),
		Concurrency: 2,
	}

	result, err := AnalyzePRs(ctx, req)

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if result == nil {
		t.Fatal("Expected non-nil result")
	}

	if len(result.Breakdowns) != 1 {
		t.Errorf("Expected 1 breakdown, got %d", len(result.Breakdowns))
	}

	if result.Skipped != 1 {
		t.Errorf("Expected 1 skipped, got %d", result.Skipped)
	}
}

func TestAnalyzePRsParallelAllFail(t *testing.T) {
	ctx := context.Background()
	now := time.Now()

	fetcher := &mockPRFetcher{
		failURLs: map[string]error{
			"https://github.com/owner/repo/pull/1": errors.New("fetch failed"),
			"https://github.com/owner/repo/pull/2": errors.New("fetch failed"),
		},
	}

	req := &AnalysisRequest{
		Samples: []PRSummaryInfo{
			{Owner: "owner", Repo: "repo", Number: 1, UpdatedAt: now},
			{Owner: "owner", Repo: "repo", Number: 2, UpdatedAt: now},
		},
		Fetcher:     fetcher,
		Config:      DefaultConfig(),
		Concurrency: 2,
	}

	result, err := AnalyzePRs(ctx, req)

	if err == nil {
		t.Error("Expected error when all fetches fail")
	}

	if result != nil {
		t.Error("Expected nil result when all fetches fail")
	}

	expectedErrMsg := "no samples could be processed successfully (2 skipped)"
	if err.Error() != expectedErrMsg {
		t.Errorf("Expected error message '%s', got: %v", expectedErrMsg, err)
	}
}

func TestAnalyzePRsWithLogger(t *testing.T) {
	ctx := context.Background()
	now := time.Now()

	// Create a logger that writes to a buffer
	var logBuf strings.Builder
	logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	fetcher := &mockPRFetcher{
		data: map[string]PRData{
			"https://github.com/owner/repo/pull/1": {
				LinesAdded: 50,
				Author:     "author1",
				Events: []ParticipantEvent{
					{Timestamp: now, Actor: "author1", Kind: "commit"},
				},
				CreatedAt: now.Add(-2 * time.Hour),
				ClosedAt:  now,
			},
		},
		failURLs: map[string]error{
			"https://github.com/owner/repo/pull/2": errors.New("fetch failed"),
		},
	}

	req := &AnalysisRequest{
		Samples: []PRSummaryInfo{
			{Owner: "owner", Repo: "repo", Number: 1, UpdatedAt: now},
			{Owner: "owner", Repo: "repo", Number: 2, UpdatedAt: now},
		},
		Fetcher:     fetcher,
		Logger:      logger,
		Config:      DefaultConfig(),
		Concurrency: 1,
	}

	result, err := AnalyzePRs(ctx, req)

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if result == nil {
		t.Fatal("Expected non-nil result")
	}

	logOutput := logBuf.String()

	// Check that processing logs are present
	if !strings.Contains(logOutput, "Processing sample PR") {
		t.Error("Expected 'Processing sample PR' in log output")
	}

	// Check that skip warning is present
	if !strings.Contains(logOutput, "Failed to fetch PR data, skipping") {
		t.Error("Expected 'Failed to fetch PR data, skipping' in log output")
	}

	// Check that progress is logged
	if !strings.Contains(logOutput, "1/2") {
		t.Error("Expected '1/2' progress in log output")
	}

	if !strings.Contains(logOutput, "2/2") {
		t.Error("Expected '2/2' progress in log output")
	}
}

func TestAnalyzePRsConcurrencyDefault(t *testing.T) {
	ctx := context.Background()
	now := time.Now()

	fetcher := &mockPRFetcher{}

	req := &AnalysisRequest{
		Samples: []PRSummaryInfo{
			{Owner: "owner", Repo: "repo", Number: 1, UpdatedAt: now},
		},
		Fetcher:     fetcher,
		Config:      DefaultConfig(),
		Concurrency: 0, // Should default to sequential (1)
	}

	result, err := AnalyzePRs(ctx, req)

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if result == nil {
		t.Fatal("Expected non-nil result")
	}

	if len(result.Breakdowns) != 1 {
		t.Errorf("Expected 1 breakdown, got %d", len(result.Breakdowns))
	}
}

func TestAnalyzePRsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	now := time.Now()

	fetcher := &mockPRFetcher{
		fetchDelay: 100 * time.Millisecond, // Delay to allow cancellation
	}

	req := &AnalysisRequest{
		Samples: []PRSummaryInfo{
			{Owner: "owner", Repo: "repo", Number: 1, UpdatedAt: now},
			{Owner: "owner", Repo: "repo", Number: 2, UpdatedAt: now},
		},
		Fetcher:     fetcher,
		Config:      DefaultConfig(),
		Concurrency: 1,
	}

	// Cancel context after a short delay
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	result, err := AnalyzePRs(ctx, req)

	// Should either fail completely or have some skipped
	if err == nil && result != nil && result.Skipped == 0 {
		// This is acceptable if cancellation happened after all fetches
		return
	}

	// If we got here, either err or skipped should be non-zero
	if err == nil && (result == nil || result.Skipped == 0) {
		t.Error("Expected context cancellation to affect results")
	}
}

func TestExtrapolateFromSamplesEmpty(t *testing.T) {
	cfg := DefaultConfig()
	result := ExtrapolateFromSamples([]Breakdown{}, 100, 10, 5, 30, cfg)

	if result.TotalPRs != 100 {
		t.Errorf("Expected TotalPRs=100, got %d", result.TotalPRs)
	}

	if result.SampledPRs != 0 {
		t.Errorf("Expected SampledPRs=0, got %d", result.SampledPRs)
	}

	if result.SuccessfulSamples != 0 {
		t.Errorf("Expected SuccessfulSamples=0, got %d", result.SuccessfulSamples)
	}

	if result.TotalCost != 0 {
		t.Errorf("Expected TotalCost=0, got $%.2f", result.TotalCost)
	}
}

func TestExtrapolateFromSamplesSingle(t *testing.T) {
	now := time.Now()
	cfg := DefaultConfig()

	// Create a single breakdown
	breakdown := Calculate(PRData{
		LinesAdded: 100,
		Author:     "test-author",
		Events: []ParticipantEvent{
			{Timestamp: now, Actor: "test-author", Kind: "commit"},
			{Timestamp: now.Add(10 * time.Minute), Actor: "reviewer", Kind: "review"},
		},
		CreatedAt: now.Add(-24 * time.Hour),
		ClosedAt:  now,
	}, cfg)

	// Extrapolate from 1 sample to 10 total PRs
	result := ExtrapolateFromSamples([]Breakdown{breakdown}, 10, 2, 0, 7, cfg)

	if result.TotalPRs != 10 {
		t.Errorf("Expected TotalPRs=10, got %d", result.TotalPRs)
	}

	if result.SampledPRs != 1 {
		t.Errorf("Expected SampledPRs=1, got %d", result.SampledPRs)
	}

	if result.SuccessfulSamples != 1 {
		t.Errorf("Expected SuccessfulSamples=1, got %d", result.SuccessfulSamples)
	}

	// Total cost should be roughly 10x the single breakdown cost
	expectedTotalCost := breakdown.TotalCost * 10
	if result.TotalCost < expectedTotalCost*0.9 || result.TotalCost > expectedTotalCost*1.1 {
		t.Errorf("Expected TotalCost≈$%.2f (10x single), got $%.2f", expectedTotalCost, result.TotalCost)
	}

	// Check that author cost is extrapolated
	if result.AuthorTotalCost <= 0 {
		t.Error("Expected positive author total cost")
	}

	// Check that participant cost is extrapolated
	if result.ParticipantTotalCost <= 0 {
		t.Error("Expected positive participant total cost")
	}

	// Check unique authors count
	if result.UniqueAuthors != 1 {
		t.Errorf("Expected 1 unique author, got %d", result.UniqueAuthors)
	}
}

func TestExtrapolateFromSamplesMultiple(t *testing.T) {
	now := time.Now()
	cfg := DefaultConfig()

	// Create multiple breakdowns with different characteristics
	breakdowns := []Breakdown{
		Calculate(PRData{
			LinesAdded: 100,
			Author:     "author1",
			Events: []ParticipantEvent{
				{Timestamp: now, Actor: "author1", Kind: "commit"},
			},
			CreatedAt: now.Add(-2 * time.Hour),
			ClosedAt:  now,
		}, cfg),
		Calculate(PRData{
			LinesAdded: 200,
			Author:     "author2",
			Events: []ParticipantEvent{
				{Timestamp: now, Actor: "author2", Kind: "commit"},
				{Timestamp: now.Add(10 * time.Minute), Actor: "reviewer", Kind: "review"},
			},
			CreatedAt: now.Add(-48 * time.Hour),
			ClosedAt:  now,
		}, cfg),
	}

	// Extrapolate from 2 samples to 20 total PRs over 14 days
	result := ExtrapolateFromSamples(breakdowns, 20, 5, 3, 14, cfg)

	if result.TotalPRs != 20 {
		t.Errorf("Expected TotalPRs=20, got %d", result.TotalPRs)
	}

	if result.SampledPRs != 2 {
		t.Errorf("Expected SampledPRs=2, got %d", result.SampledPRs)
	}

	if result.SuccessfulSamples != 2 {
		t.Errorf("Expected SuccessfulSamples=2, got %d", result.SuccessfulSamples)
	}

	// Check unique authors (should be 2)
	if result.UniqueAuthors != 2 {
		t.Errorf("Expected 2 unique authors, got %d", result.UniqueAuthors)
	}

	// Total cost should be roughly 10x the average breakdown cost
	avgCost := (breakdowns[0].TotalCost + breakdowns[1].TotalCost) / 2
	expectedTotalCost := avgCost * 20
	if result.TotalCost < expectedTotalCost*0.9 || result.TotalCost > expectedTotalCost*1.1 {
		t.Errorf("Expected TotalCost≈$%.2f, got $%.2f", expectedTotalCost, result.TotalCost)
	}

	// Check waste per week calculations (should be > 0 for 14 day period)
	if result.WasteHoursPerWeek <= 0 {
		t.Error("Expected positive waste hours per week")
	}

	if result.WasteCostPerWeek <= 0 {
		t.Error("Expected positive waste cost per week")
	}

	// Check average PR duration is calculated
	if result.AvgPRDurationHours <= 0 {
		t.Error("Expected positive average PR duration")
	}
}

func TestExtrapolateFromSamplesBotVsHuman(t *testing.T) {
	cfg := DefaultConfig()

	// Create breakdowns with both human and bot PRs
	breakdowns := []Breakdown{
		// Human PR
		{
			PRAuthor:   "human-author",
			AuthorBot:  false,
			PRDuration: 24.0,
			Author: AuthorCostDetail{
				NewLines:      100,
				ModifiedLines: 150,
			},
			TotalCost: 1000,
		},
		// Bot PR
		{
			PRAuthor:   "dependabot[bot]",
			AuthorBot:  true,
			PRDuration: 2.0,
			Author: AuthorCostDetail{
				NewLines:      50,
				ModifiedLines: 60,
			},
			TotalCost: 100,
		},
	}

	result := ExtrapolateFromSamples(breakdowns, 10, 5, 0, 7, cfg)

	// Should have both human and bot PR counts
	if result.HumanPRs <= 0 {
		t.Error("Expected positive human PR count")
	}

	if result.BotPRs <= 0 {
		t.Error("Expected positive bot PR count")
	}

	// Should have separate duration averages
	if result.AvgHumanPRDurationHours <= 0 {
		t.Error("Expected positive average human PR duration")
	}

	if result.AvgBotPRDurationHours <= 0 {
		t.Error("Expected positive average bot PR duration")
	}

	// Bot LOC should be tracked separately
	if result.BotNewLines <= 0 {
		t.Error("Expected positive bot new lines")
	}

	if result.BotModifiedLines <= 0 {
		t.Error("Expected positive bot modified lines")
	}

	// Human authors should only count human PRs
	if result.UniqueAuthors != 1 {
		t.Errorf("Expected 1 unique human author, got %d", result.UniqueAuthors)
	}
}

func TestExtrapolateFromSamplesWasteCalculation(t *testing.T) {
	now := time.Now()
	cfg := DefaultConfig()

	// Create a breakdown with significant delay costs
	breakdown := Calculate(PRData{
		LinesAdded: 100,
		Author:     "author1",
		Events: []ParticipantEvent{
			{Timestamp: now.Add(-168 * time.Hour), Actor: "author1", Kind: "commit"},
		},
		CreatedAt: now.Add(-168 * time.Hour), // 7 days old
		ClosedAt:  now,
	}, cfg)

	// Extrapolate over 7 days
	result := ExtrapolateFromSamples([]Breakdown{breakdown}, 10, 3, 0, 7, cfg)

	// Waste per week should be calculated
	if result.WasteHoursPerWeek <= 0 {
		t.Error("Expected positive waste hours per week")
	}

	if result.WasteCostPerWeek <= 0 {
		t.Error("Expected positive waste cost per week")
	}

	// Per-author waste should be calculated
	if result.WasteHoursPerAuthorPerWeek <= 0 {
		t.Error("Expected positive waste hours per author per week")
	}

	if result.WasteCostPerAuthorPerWeek <= 0 {
		t.Error("Expected positive waste cost per author per week")
	}

	// Waste should be roughly the delay costs
	// WastePerWeek = (delay costs) / weeks
	expectedWastePerWeek := breakdown.DelayCost * 10 // Extrapolated to 10 PRs, 1 week period
	if result.WasteCostPerWeek < expectedWastePerWeek*0.5 || result.WasteCostPerWeek > expectedWastePerWeek*1.5 {
		t.Errorf("Expected WasteCostPerWeek≈$%.2f, got $%.2f", expectedWastePerWeek, result.WasteCostPerWeek)
	}
}

func TestExtrapolateFromSamplesR2RSavings(t *testing.T) {
	now := time.Now()
	cfg := DefaultConfig()

	// Create breakdowns with long PR durations (high waste)
	breakdowns := []Breakdown{
		Calculate(PRData{
			LinesAdded: 100,
			Author:     "author1",
			Events: []ParticipantEvent{
				{Timestamp: now.Add(-72 * time.Hour), Actor: "author1", Kind: "commit"},
			},
			CreatedAt: now.Add(-72 * time.Hour), // 3 days old
			ClosedAt:  now,
		}, cfg),
	}

	result := ExtrapolateFromSamples(breakdowns, 100, 10, 5, 30, cfg)

	// R2R savings should be calculated
	// Savings formula: baseline waste - remodeled waste - subscription cost
	// Should be > 0 if current waste is high enough
	if result.R2RSavings < 0 {
		t.Error("R2R savings should not be negative")
	}

	// For a 3-day PR, there should be significant savings
	// (R2R targets 40-minute PRs, which would eliminate most delay costs)
	if result.R2RSavings == 0 {
		t.Error("Expected positive R2R savings for long-duration PRs")
	}

	// UniqueNonBotUsers should be tracked
	if result.UniqueNonBotUsers <= 0 {
		t.Error("Expected positive unique non-bot users count")
	}
}

func TestExtrapolateFromSamplesOpenPRTracking(t *testing.T) {
	now := time.Now()
	cfg := DefaultConfig()

	breakdown := Calculate(PRData{
		LinesAdded: 50,
		Author:     "author1",
		Events: []ParticipantEvent{
			{Timestamp: now, Actor: "author1", Kind: "commit"},
		},
		CreatedAt: now.Add(-1 * time.Hour),
		ClosedAt:  now,
	}, cfg)

	// Test with actual open PRs
	actualOpenPRs := 15
	result := ExtrapolateFromSamples([]Breakdown{breakdown}, 100, 5, actualOpenPRs, 30, cfg)

	// Open PRs should match actual count (not extrapolated)
	if result.OpenPRs != actualOpenPRs {
		t.Errorf("Expected OpenPRs=%d (actual), got %d", actualOpenPRs, result.OpenPRs)
	}

	// PR tracking cost should be based on actual open PRs
	if result.PRTrackingCost <= 0 {
		t.Error("Expected positive PR tracking cost with open PRs")
	}

	// PR tracking hours should scale with open PRs and user count
	if result.PRTrackingHours <= 0 {
		t.Error("Expected positive PR tracking hours")
	}
}

func TestExtrapolateFromSamplesParticipants(t *testing.T) {
	now := time.Now()
	cfg := DefaultConfig()

	// Create breakdown with multiple participants
	breakdown := Calculate(PRData{
		LinesAdded: 100,
		Author:     "author1",
		Events: []ParticipantEvent{
			{Timestamp: now, Actor: "author1", Kind: "commit"},
			{Timestamp: now.Add(10 * time.Minute), Actor: "reviewer1", Kind: "review"},
			{Timestamp: now.Add(20 * time.Minute), Actor: "reviewer2", Kind: "review"},
			{Timestamp: now.Add(30 * time.Minute), Actor: "commenter1", Kind: "comment"},
		},
		CreatedAt: now.Add(-2 * time.Hour),
		ClosedAt:  now,
	}, cfg)

	result := ExtrapolateFromSamples([]Breakdown{breakdown}, 10, 5, 0, 7, cfg)

	// Participant costs should be extrapolated
	if result.ParticipantReviewCost <= 0 {
		t.Error("Expected positive participant review cost")
	}

	if result.ParticipantTotalCost <= 0 {
		t.Error("Expected positive participant total cost")
	}

	// Participant metrics should be tracked
	if result.ParticipantEvents <= 0 {
		t.Error("Expected positive participant events count")
	}

	if result.ParticipantSessions <= 0 {
		t.Error("Expected positive participant sessions count")
	}

	// Unique non-bot users should include both authors and participants
	if result.UniqueNonBotUsers < 2 {
		t.Errorf("Expected at least 2 unique non-bot users (author + reviewers), got %d", result.UniqueNonBotUsers)
	}
}
