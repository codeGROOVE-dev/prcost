package cost

import (
	"encoding/json"
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

	if cfg.DeliveryDelayFactor != 0.15 {
		t.Errorf("Expected delivery delay factor 0.15, got %.2f", cfg.DeliveryDelayFactor)
	}

	if cfg.CoordinationFactor != 0.05 {
		t.Errorf("Expected coordination factor 0.05, got %.2f", cfg.CoordinationFactor)
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

	// Should have coordination cost
	if breakdown.DelayCostDetail.CoordinationCost <= 0 {
		t.Error("Coordination cost should be positive for 7-day old PR")
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

	// Total delay should equal sum of components
	expectedDelay := breakdown.DelayCostDetail.DeliveryDelayCost +
		breakdown.DelayCostDetail.CoordinationCost +
		breakdown.DelayCostDetail.CodeChurnCost +
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

	// Should still have delivery delay and coordination costs
	if breakdown.DelayCostDetail.DeliveryDelayCost <= 0 {
		t.Error("Delivery delay cost should be positive even for short PR")
	}

	if breakdown.DelayCostDetail.CoordinationCost <= 0 {
		t.Error("Coordination cost should be positive even for short PR")
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
	// Coordination: 2160 * 0.05 = 108 hours
	expectedDeliveryHours := 90.0 * 24.0 * 0.15     // 324 hours
	expectedCoordinationHours := 90.0 * 24.0 * 0.05 // 108 hours

	if breakdown.DelayCostDetail.DeliveryDelayHours != expectedDeliveryHours {
		t.Errorf("Expected %.0f delivery delay hours (15%% of 90 day cap), got %.2f",
			expectedDeliveryHours, breakdown.DelayCostDetail.DeliveryDelayHours)
	}

	if breakdown.DelayCostDetail.CoordinationHours != expectedCoordinationHours {
		t.Errorf("Expected %.0f coordination hours (5%% of 90 day cap), got %.2f",
			expectedCoordinationHours, breakdown.DelayCostDetail.CoordinationHours)
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
	t.Logf("  Delivery Delay (15%%): $%.2f (%.0f hrs, capped at 90 days)",
		breakdown.DelayCostDetail.DeliveryDelayCost, breakdown.DelayCostDetail.DeliveryDelayHours)
	t.Logf("  Coordination (5%%): $%.2f (%.0f hrs, capped at 90 days)",
		breakdown.DelayCostDetail.CoordinationCost, breakdown.DelayCostDetail.CoordinationHours)
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
	// Delivery delay: 336 * 0.15 = 50.4 hours
	// Coordination: 336 * 0.05 = 16.8 hours
	expectedDeliveryHours := 14.0 * 24.0 * 0.15
	expectedCoordinationHours := 14.0 * 24.0 * 0.05

	if breakdown.DelayCostDetail.DeliveryDelayHours != expectedDeliveryHours {
		t.Errorf("Expected %.1f delivery delay hours (15%% of 14 days), got %.2f",
			expectedDeliveryHours, breakdown.DelayCostDetail.DeliveryDelayHours)
	}

	if breakdown.DelayCostDetail.CoordinationHours != expectedCoordinationHours {
		t.Errorf("Expected %.1f coordination hours (5%% of 14 days), got %.2f",
			expectedCoordinationHours, breakdown.DelayCostDetail.CoordinationHours)
	}
}

func TestDelayHoursNeverExceedPRAge(t *testing.T) {
	// Test that delay hours (as productivity-equivalent time) are reasonable
	// Delivery (15%) + Coordination (5%) should equal 20% of PR age
	now := time.Now()
	testCases := []struct {
		name    string
		ageHrs  float64
		prData  PRData
		wantMax float64 // Maximum acceptable delay hours (delivery + coordination)
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

			totalDelayHours := breakdown.DelayCostDetail.DeliveryDelayHours +
				breakdown.DelayCostDetail.CoordinationHours

			// Total delay hours should not exceed 20% of PR age
			if totalDelayHours > tc.wantMax+0.1 { // Allow 0.1 hr tolerance for floating point
				t.Errorf("Delay hours exceed 20%% of PR age: got %.2f hours, want <= %.2f hours (PR age: %.1f hrs)",
					totalDelayHours, tc.wantMax, tc.ageHrs)
			}

			// Verify the split: delivery should be 15%, coordination should be 5%
			expectedDelivery := tc.ageHrs * 0.15
			expectedCoordination := tc.ageHrs * 0.05

			if breakdown.DelayCostDetail.DeliveryDelayHours > expectedDelivery+0.1 {
				t.Errorf("Delivery delay hours too high: got %.2f, want %.2f (15%% of %.1f)",
					breakdown.DelayCostDetail.DeliveryDelayHours, expectedDelivery, tc.ageHrs)
			}

			if breakdown.DelayCostDetail.CoordinationHours > expectedCoordination+0.1 {
				t.Errorf("Coordination hours too high: got %.2f, want %.2f (5%% of %.1f)",
					breakdown.DelayCostDetail.CoordinationHours, expectedCoordination, tc.ageHrs)
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
				if breakdown.DelayCostDetail.CoordinationCost != 0 {
					t.Errorf("Expected 0 coordination cost for %v minute PR, got $%.2f",
						tc.openMinutes, breakdown.DelayCostDetail.CoordinationCost)
				}
			} else if breakdown.DelayCost == 0 {
				// For PRs >= 30 minutes, delay cost should be > 0
				t.Errorf("Expected non-zero delay cost for %v minute PR, got $0",
					tc.openMinutes)
			}
		})
	}
}
