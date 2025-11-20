package github

import (
	"testing"
	"time"
)

func TestIsBot(t *testing.T) {
	tests := []struct {
		name     string
		prAuthor string
		want     bool
	}{
		{
			name:     "dependabot",
			prAuthor: "dependabot[bot]",
			want:     true,
		},
		{
			name:     "renovate",
			prAuthor: "renovate[bot]",
			want:     true,
		},
		{
			name:     "github-actions",
			prAuthor: "github-actions[bot]",
			want:     true,
		},
		{
			name:     "human user",
			prAuthor: "testuser",
			want:     false,
		},
		{
			name:     "user with bot in name",
			prAuthor: "robot-person",
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsBot("", tt.prAuthor)
			if got != tt.want {
				t.Errorf("IsBot(%q) = %v, want %v", tt.prAuthor, got, tt.want)
			}
		})
	}
}

func TestCountBotPRs(t *testing.T) {
	prs := []PRSummary{
		{Author: "dependabot[bot]"},
		{Author: "renovate[bot]"},
		{Author: "testuser"},
		{Author: "anotheruser"},
		{Author: "github-actions[bot]"},
	}

	botCount := CountBotPRs(prs)
	if botCount != 3 {
		t.Errorf("CountBotPRs() = %d, want 3", botCount)
	}
}

func TestSamplePRs(t *testing.T) {
	// Create sample PRs
	prs := make([]PRSummary, 100)
	for i := range prs {
		prs[i] = PRSummary{
			Number: i + 1,
			Owner:  "testowner",
			Repo:   "testrepo",
		}
	}

	tests := []struct {
		name       string
		totalPRs   int
		targetSize int
		wantSize   int
	}{
		{
			name:       "sample 10 from 100",
			totalPRs:   100,
			targetSize: 10,
			wantSize:   10,
		},
		{
			name:       "sample more than available",
			totalPRs:   100,
			targetSize: 150,
			wantSize:   100,
		},
		{
			name:       "sample with small set",
			totalPRs:   5,
			targetSize: 10,
			wantSize:   5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testPRs := prs[:tt.totalPRs]
			result := SamplePRs(testPRs, tt.targetSize)
			if len(result) != tt.wantSize {
				t.Errorf("SamplePRs() returned %d PRs, want %d", len(result), tt.wantSize)
			}
		})
	}
}

func TestCountUniqueAuthors(t *testing.T) {
	prs := []PRSummary{
		{Author: "user1"},
		{Author: "user2"},
		{Author: "user1"}, // duplicate
		{Author: "user3"},
		{Author: "user2"}, // duplicate
		{Author: "dependabot[bot]"},
	}

	count := CountUniqueAuthors(prs)
	if count != 3 {
		t.Errorf("CountUniqueAuthors() = %d, want 3", count)
	}
}

func TestCalculateActualTimeWindow(t *testing.T) {
	now := time.Now()
	prs := []PRSummary{
		{UpdatedAt: now.Add(-10 * 24 * time.Hour)},
		{UpdatedAt: now.Add(-5 * 24 * time.Hour)},
		{UpdatedAt: now.Add(-1 * 24 * time.Hour)},
	}

	// When PRs don't cover full requested period, function returns requested days
	days, hitLimit := CalculateActualTimeWindow(prs, 30)
	if days != 30 {
		t.Errorf("CalculateActualTimeWindow() = %d days, want 30 days (requested)", days)
	}
	if hitLimit {
		t.Error("CalculateActualTimeWindow() hitLimit = true, want false")
	}

	// Test with empty PRs
	days2, hitLimit2 := CalculateActualTimeWindow([]PRSummary{}, 30)
	if days2 != 30 {
		t.Errorf("CalculateActualTimeWindow(empty) = %d days, want 30", days2)
	}
	if hitLimit2 {
		t.Error("CalculateActualTimeWindow(empty) hitLimit = true, want false")
	}
}

func TestDeduplicatePRs(t *testing.T) {
	now := time.Now()
	earlier := now.Add(-1 * time.Hour)
	later := now.Add(1 * time.Hour)

	prs := []PRSummary{
		{Number: 1, Owner: "owner", Repo: "repo", UpdatedAt: earlier},
		{Number: 2, Owner: "owner", Repo: "repo", UpdatedAt: now},
		{Number: 1, Owner: "owner", Repo: "repo", UpdatedAt: later}, // duplicate - first occurrence kept
	}

	result := deduplicatePRs(prs)
	if len(result) != 2 {
		t.Errorf("deduplicatePRs() returned %d PRs, want 2", len(result))
	}

	// Verify we have both unique PRs
	numbers := make(map[int]bool)
	for _, pr := range result {
		numbers[pr.Number] = true
	}
	if !numbers[1] || !numbers[2] {
		t.Error("deduplicatePRs() did not include all unique PRs")
	}
}

func TestDeduplicatePRsByOwnerRepoNumber(t *testing.T) {
	prs := []PRSummary{
		{Number: 1, Owner: "owner1", Repo: "repo1", UpdatedAt: time.Now()},
		{Number: 1, Owner: "owner2", Repo: "repo1", UpdatedAt: time.Now()},                    // different owner
		{Number: 1, Owner: "owner1", Repo: "repo1", UpdatedAt: time.Now().Add(1 * time.Hour)}, // duplicate
	}

	result := deduplicatePRsByOwnerRepoNumber(prs)
	if len(result) != 2 {
		t.Errorf("deduplicatePRsByOwnerRepoNumber() returned %d PRs, want 2", len(result))
	}
}

func TestIsBotEdgeCases(t *testing.T) {
	tests := []struct {
		name   string
		author string
		want   bool
	}{
		{"empty string", "", false},
		{"just brackets", "[bot]", true},
		{"app suffix", "myapp[bot]", true},
		{"greenkeeper", "greenkeeper[bot]", true},
		{"snyk", "snyk[bot]", true},
		{"imgbot", "imgbot[bot]", true},
		{"allcontributors", "allcontributors[bot]", true},
		{"stale", "stale[bot]", true},
		{"codecov", "codecov[bot]", true},
		{"whitesource", "whitesource[bot]", true},
		{"normal user no brackets", "username", false},
		{"bot in middle of name", "robot-user", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsBot("", tt.author)
			if got != tt.want {
				t.Errorf("IsBot(%q) = %v, want %v", tt.author, got, tt.want)
			}
		})
	}
}
