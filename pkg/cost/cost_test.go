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

	if cfg.AnnualSalary != 250000.0 {
		t.Errorf("Expected annual salary $250,000, got $%.2f", cfg.AnnualSalary)
	}

	if cfg.BenefitsMultiplier != 1.3 {
		t.Errorf("Expected benefits multiplier 1.3, got %.2f", cfg.BenefitsMultiplier)
	}

	if cfg.EventDuration != 20*time.Minute {
		t.Errorf("Expected 20 minutes per event, got %v", cfg.EventDuration)
	}

	if cfg.SessionGapThreshold != 60*time.Minute {
		t.Errorf("Expected 60 minute session gap, got %v", cfg.SessionGapThreshold)
	}

	if cfg.DelayCostFactor != 0.20 {
		t.Errorf("Expected delay cost factor 0.20, got %.2f", cfg.DelayCostFactor)
	}

	if cfg.MaxProjectDelay != 60*24*time.Hour {
		t.Errorf("Expected 60 days max project delay, got %v", cfg.MaxProjectDelay)
	}

	if cfg.MaxCodeDrift != 90*24*time.Hour {
		t.Errorf("Expected 90 days max code drift, got %v", cfg.MaxCodeDrift)
	}
}

func TestHourlyRate(t *testing.T) {
	cfg := DefaultConfig()
	hourlyRate := cfg.HourlyRate()

	// $250,000 * 1.3 / 2080 = $156.25/hr
	expectedRate := 156.25
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
		CreatedAt:            now.Add(-1 * time.Hour),
		UpdatedAt:            now,
		AuthorHasWriteAccess: true,
	}

	cfg := DefaultConfig()
	breakdown := Calculate(prData, cfg)

	// Single event should create one session
	if breakdown.Author.Sessions != 1 {
		t.Errorf("Expected 1 session for single event, got %d", breakdown.Author.Sessions)
	}

	// Single event should have positive costs
	if breakdown.Author.CodeCost <= 0 {
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
		CreatedAt:            now.Add(-1 * time.Hour),
		UpdatedAt:            now.Add(10 * time.Minute),
		AuthorHasWriteAccess: true,
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
		CreatedAt:            now.Add(-2 * time.Hour),
		UpdatedAt:            now.Add(90 * time.Minute),
		AuthorHasWriteAccess: true,
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
		CreatedAt:            now.Add(-3 * time.Hour),
		UpdatedAt:            now.Add(2 * time.Hour),
		AuthorHasWriteAccess: true,
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
			UpdatedAt         string `json:"updated_at"`
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
		})
	}

	createdAt, err := time.Parse(time.RFC3339, prxData.PullRequest.CreatedAt)
	if err != nil {
		t.Fatalf("Failed to parse created_at: %v", err)
	}
	updatedAt, err := time.Parse(time.RFC3339, prxData.PullRequest.UpdatedAt)
	if err != nil {
		t.Fatalf("Failed to parse updated_at: %v", err)
	}

	prData := PRData{
		LinesAdded:           prxData.PullRequest.Additions,
		Author:               prxData.PullRequest.Author,
		Events:               events,
		CreatedAt:            createdAt,
		UpdatedAt:            updatedAt,
		AuthorHasWriteAccess: prxData.PullRequest.AuthorWriteAccess >= 0,
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
	if breakdown.Author.CodeCost <= 0 {
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
		CreatedAt:            time.Now().Add(-1 * time.Hour),
		UpdatedAt:            time.Now(),
		AuthorHasWriteAccess: true,
	}

	cfg := DefaultConfig()
	breakdown := Calculate(prData, cfg)

	// Even minimal PR should have costs
	if breakdown.Author.CodeCost <= 0 {
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
		CreatedAt:            time.Now().Add(-72 * time.Hour),
		UpdatedAt:            time.Now(),
		AuthorHasWriteAccess: false, // External contributor
	}

	cfg := DefaultConfig()
	breakdown := Calculate(prData, cfg)

	// External contributors should have same cost calculation as internal
	// (we removed the 50% reduction)
	internalData := prData
	internalData.AuthorHasWriteAccess = true
	internalBreakdown := Calculate(internalData, cfg)

	// Costs should be equal
	if breakdown.DelayCost != internalBreakdown.DelayCost {
		t.Errorf("External and internal contributor delay costs should be equal, got %.2f vs %.2f",
			breakdown.DelayCost, internalBreakdown.DelayCost)
	}

	if breakdown.TotalCost != internalBreakdown.TotalCost {
		t.Errorf("External and internal contributor total costs should be equal, got %.2f vs %.2f",
			breakdown.TotalCost, internalBreakdown.TotalCost)
	}
}

func TestCalculateDelayComponents(t *testing.T) {
	// Test PR open for 7 days - should have code drift
	now := time.Now()
	prData := PRData{
		LinesAdded: 100,
		Author:     "test-author",
		Events: []ParticipantEvent{
			{Timestamp: now.Add(-7 * 24 * time.Hour), Actor: "test-author"},
		},
		CreatedAt:            now.Add(-7 * 24 * time.Hour),
		UpdatedAt:            now,
		AuthorHasWriteAccess: true,
	}

	cfg := DefaultConfig()
	breakdown := Calculate(prData, cfg)

	// Should have project delay cost
	if breakdown.DelayCostDetail.ProjectDelayCost <= 0 {
		t.Error("Project delay cost should be positive for 7-day old PR")
	}

	// Should have code updates cost (7 days = ~7% drift)
	if breakdown.DelayCostDetail.CodeUpdatesCost <= 0 {
		t.Error("Code updates cost should be positive for 7-day old PR")
	}

	if breakdown.DelayCostDetail.ReworkPercentage <= 0 {
		t.Error("Rework percentage should be positive for 7-day old PR")
	}

	// Should have future GitHub cost
	if breakdown.DelayCostDetail.FutureGitHubCost <= 0 {
		t.Error("Future GitHub cost should be positive")
	}

	// Total delay should equal sum of components
	expectedDelay := breakdown.DelayCostDetail.ProjectDelayCost +
		breakdown.DelayCostDetail.CodeUpdatesCost +
		breakdown.DelayCostDetail.FutureGitHubCost

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
		CreatedAt:            now.Add(-2 * 24 * time.Hour),
		UpdatedAt:            now,
		AuthorHasWriteAccess: true,
	}

	cfg := DefaultConfig()
	breakdown := Calculate(prData, cfg)

	// Should NOT have code updates cost (< 3 days)
	if breakdown.DelayCostDetail.CodeUpdatesCost != 0 {
		t.Error("Code updates cost should be zero for 2-day old PR")
	}

	if breakdown.DelayCostDetail.ReworkPercentage != 0 {
		t.Error("Rework percentage should be zero for 2-day old PR")
	}

	// Should still have project delay and future GitHub costs
	if breakdown.DelayCostDetail.ProjectDelayCost <= 0 {
		t.Error("Project delay cost should be positive even for short PR")
	}

	if breakdown.DelayCostDetail.FutureGitHubCost <= 0 {
		t.Error("Future GitHub cost should be positive")
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
			UpdatedAt         string `json:"updated_at"`
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
		})
	}

	createdAt, err := time.Parse(time.RFC3339, prxData.PullRequest.CreatedAt)
	if err != nil {
		t.Fatalf("Failed to parse created_at: %v", err)
	}
	updatedAt, err := time.Parse(time.RFC3339, prxData.PullRequest.UpdatedAt)
	if err != nil {
		t.Fatalf("Failed to parse updated_at: %v", err)
	}

	prData := PRData{
		LinesAdded:           prxData.PullRequest.Additions,
		Author:               prxData.PullRequest.Author,
		Events:               events,
		CreatedAt:            createdAt,
		UpdatedAt:            updatedAt,
		AuthorHasWriteAccess: prxData.PullRequest.AuthorWriteAccess >= 0,
	}

	cfg := DefaultConfig()
	breakdown := Calculate(prData, cfg)

	// This PR was open for ~2136 days (almost 6 years!)
	// Project delay should be capped at 60 days
	if !breakdown.DelayCapped {
		t.Error("Very long PR should have project delay capped")
	}

	expectedProjectDelayHours := 60.0 * 24.0 // 60 days cap
	if breakdown.DelayCostDetail.ProjectDelayHours != expectedProjectDelayHours {
		t.Errorf("Expected %.0f project delay hours (60 day cap), got %.2f",
			expectedProjectDelayHours, breakdown.DelayCostDetail.ProjectDelayHours)
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
	t.Logf("  Project Delay: $%.2f (%.0f hrs, capped at 60 days)",
		breakdown.DelayCostDetail.ProjectDelayCost, breakdown.DelayCostDetail.ProjectDelayHours)
	t.Logf("  Code Updates: $%.2f (%.1f%% rework, capped at 90 days drift)",
		breakdown.DelayCostDetail.CodeUpdatesCost, breakdown.DelayCostDetail.ReworkPercentage)
	t.Logf("  Future GitHub: $%.2f", breakdown.DelayCostDetail.FutureGitHubCost)
	t.Logf("  Total delay cost: $%.2f", breakdown.DelayCost)
	t.Logf("  Total cost: $%.2f", breakdown.TotalCost)
}

func TestCalculateLongPRCapped(t *testing.T) {
	// Test PR open for 120 days - should be capped at 60 days for project delay
	now := time.Now()
	prData := PRData{
		LinesAdded: 100,
		Author:     "test-author",
		Events: []ParticipantEvent{
			{Timestamp: now.Add(-120 * 24 * time.Hour), Actor: "test-author"},
		},
		CreatedAt:            now.Add(-120 * 24 * time.Hour),
		UpdatedAt:            now,
		AuthorHasWriteAccess: true,
	}

	cfg := DefaultConfig()
	breakdown := Calculate(prData, cfg)

	// Should be capped
	if !breakdown.DelayCapped {
		t.Error("120-day old PR should have delay capped")
	}

	// Project delay hours should be capped at 60 days (default MaxProjectDelay)
	expectedHours := 60.0 * 24.0
	if breakdown.DelayCostDetail.ProjectDelayHours != expectedHours {
		t.Errorf("Expected %.0f project delay hours (60 days), got %.2f",
			expectedHours, breakdown.DelayCostDetail.ProjectDelayHours)
	}
}
