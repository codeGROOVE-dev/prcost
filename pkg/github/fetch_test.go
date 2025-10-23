package github

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/codeGROOVE-dev/prx/pkg/prx"
)

func TestParsePRURL(t *testing.T) {
	tests := []struct {
		name       string
		url        string
		wantOwner  string
		wantRepo   string
		wantNumber int
		wantErr    bool
	}{
		{
			name:       "valid HTTPS URL",
			url:        "https://github.com/owner/repo/pull/123",
			wantOwner:  "owner",
			wantRepo:   "repo",
			wantNumber: 123,
			wantErr:    false,
		},
		{
			name:       "valid HTTP URL",
			url:        "http://github.com/owner/repo/pull/456",
			wantOwner:  "owner",
			wantRepo:   "repo",
			wantNumber: 456,
			wantErr:    false,
		},
		{
			name:       "URL without protocol",
			url:        "github.com/owner/repo/pull/789",
			wantOwner:  "owner",
			wantRepo:   "repo",
			wantNumber: 789,
			wantErr:    false,
		},
		{
			name:    "invalid - not github.com",
			url:     "https://gitlab.com/owner/repo/pull/123",
			wantErr: true,
		},
		{
			name:    "invalid - missing pull keyword",
			url:     "https://github.com/owner/repo/123",
			wantErr: true,
		},
		{
			name:    "invalid - non-numeric PR number",
			url:     "https://github.com/owner/repo/pull/abc",
			wantErr: true,
		},
		{
			name:    "invalid - incomplete URL",
			url:     "https://github.com/owner/repo",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner, repo, number, err := parsePRURL(tt.url)

			if (err != nil) != tt.wantErr {
				t.Errorf("parsePRURL() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if owner != tt.wantOwner {
					t.Errorf("parsePRURL() owner = %v, want %v", owner, tt.wantOwner)
				}
				if repo != tt.wantRepo {
					t.Errorf("parsePRURL() repo = %v, want %v", repo, tt.wantRepo)
				}
				if number != tt.wantNumber {
					t.Errorf("parsePRURL() number = %v, want %v", number, tt.wantNumber)
				}
			}
		})
	}
}

func TestExtractParticipantEvents(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name           string
		events         []prx.Event
		expectedCount  int
		expectedActors []string
	}{
		{
			name: "all human events",
			events: []prx.Event{
				{Timestamp: now, Actor: "alice", Bot: false},
				{Timestamp: now.Add(1 * time.Hour), Actor: "bob", Bot: false},
				{Timestamp: now.Add(2 * time.Hour), Actor: "charlie", Bot: false},
			},
			expectedCount:  3,
			expectedActors: []string{"alice", "bob", "charlie"},
		},
		{
			name: "mixed human and bot events",
			events: []prx.Event{
				{Timestamp: now, Actor: "alice", Bot: false},
				{Timestamp: now.Add(1 * time.Hour), Actor: "github-actions", Bot: true},
				{Timestamp: now.Add(2 * time.Hour), Actor: "bob", Bot: false},
				{Timestamp: now.Add(3 * time.Hour), Actor: "dependabot", Bot: true},
			},
			expectedCount:  2,
			expectedActors: []string{"alice", "bob"},
		},
		{
			name: "filter out github automation",
			events: []prx.Event{
				{Timestamp: now, Actor: "alice", Bot: false},
				{Timestamp: now.Add(1 * time.Hour), Actor: "github", Bot: false},
				{Timestamp: now.Add(2 * time.Hour), Actor: "bob", Bot: false},
			},
			expectedCount:  2,
			expectedActors: []string{"alice", "bob"},
		},
		{
			name: "all bot events",
			events: []prx.Event{
				{Timestamp: now, Actor: "github-actions", Bot: true},
				{Timestamp: now.Add(1 * time.Hour), Actor: "dependabot", Bot: true},
			},
			expectedCount:  0,
			expectedActors: []string{},
		},
		{
			name:           "empty events",
			events:         []prx.Event{},
			expectedCount:  0,
			expectedActors: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractParticipantEvents(tt.events)

			if len(result) != tt.expectedCount {
				t.Errorf("Expected %d events, got %d", tt.expectedCount, len(result))
			}

			for i, expected := range tt.expectedActors {
				if i >= len(result) {
					t.Errorf("Missing expected actor: %s", expected)
					continue
				}
				if result[i].Actor != expected {
					t.Errorf("Expected actor %s at position %d, got %s", expected, i, result[i].Actor)
				}
			}
		})
	}
}

func TestPRDataFromPRX(t *testing.T) {
	now := time.Now()
	created := now.Add(-24 * time.Hour)

	prxData := prx.PullRequestData{
		PullRequest: prx.PullRequest{
			Author:            "test-author",
			Additions:         100,
			Deletions:         50,
			CreatedAt:         created,
			AuthorWriteAccess: 1, // Has write access
		},
		Events: []prx.Event{
			{Timestamp: created, Actor: "test-author", Kind: "commit", Bot: false},
			{Timestamp: created.Add(1 * time.Hour), Actor: "reviewer", Kind: "review", Bot: false},
			{Timestamp: created.Add(2 * time.Hour), Actor: "github-actions", Kind: "check_run", Bot: true},
		},
	}

	costData := PRDataFromPRX(&prxData)

	// Validate basic fields
	if costData.Author != "test-author" {
		t.Errorf("Expected author 'test-author', got '%s'", costData.Author)
	}

	if costData.LinesAdded != 100 {
		t.Errorf("Expected 100 lines added, got %d", costData.LinesAdded)
	}

	if !costData.CreatedAt.Equal(created) {
		t.Errorf("Expected created at %v, got %v", created, costData.CreatedAt)
	}

	// Should have 2 events (bot event filtered out)
	if len(costData.Events) != 2 {
		t.Errorf("Expected 2 human events, got %d", len(costData.Events))
	}

	// Validate event actors
	expectedActors := []string{"test-author", "reviewer"}
	for i, expected := range expectedActors {
		if costData.Events[i].Actor != expected {
			t.Errorf("Expected event %d actor '%s', got '%s'", i, expected, costData.Events[i].Actor)
		}
	}
}

func TestPRDataFromPRXExternalContributor(t *testing.T) {
	now := time.Now()

	prxData := prx.PullRequestData{
		PullRequest: prx.PullRequest{
			Author:            "external-contributor",
			Additions:         50,
			CreatedAt:         now,
			AuthorWriteAccess: -1, // No write access
		},
		Events: []prx.Event{
			{Timestamp: now, Actor: "external-contributor", Kind: "commit", Bot: false},
		},
	}

	costData := PRDataFromPRX(&prxData)

	// Verify basic conversion
	if costData.Author != "external-contributor" {
		t.Errorf("Expected author 'external-contributor', got '%s'", costData.Author)
	}
}

func TestPRDataFromPRXWithRealData(t *testing.T) {
	// Test with real PR 1891 data
	data, err := os.ReadFile("../../testdata/pr_1891.json")
	if err != nil {
		t.Skipf("Skipping real data test: %v", err)
	}

	var prxData prx.PullRequestData
	if err := json.Unmarshal(data, &prxData); err != nil {
		t.Fatalf("Failed to parse PR data: %v", err)
	}

	costData := PRDataFromPRX(&prxData)

	// PR 1891 specific validations
	if costData.Author != "markusthoemmes" {
		t.Errorf("Expected author 'markusthoemmes', got '%s'", costData.Author)
	}

	if costData.LinesAdded != 26 {
		t.Errorf("Expected 26 lines added, got %d", costData.LinesAdded)
	}

	// Should have filtered out all bot events
	for _, event := range costData.Events {
		if event.Actor == "github" {
			t.Error("Should have filtered out github automation events")
		}
	}

	// PR 1891 had 25 events total, but 20 were bot events
	// So we should have 5 human events (markusthoemmes commit/open + xnox review/merge/close)
	if len(costData.Events) < 2 {
		t.Errorf("Expected at least 2 human events, got %d", len(costData.Events))
	}

	t.Logf("PR 1891: %d human events out of %d total events", len(costData.Events), len(prxData.Events))
}
