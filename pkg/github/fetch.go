// Package github fetches pull request data from GitHub using prx or turnserver.
package github

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/codeGROOVE-dev/prcost/pkg/cost"
	"github.com/codeGROOVE-dev/prx/pkg/prx"
)

// PRDataFromPRX converts prx.PullRequestData to cost.PRData.
// This allows you to use prcost with pre-fetched PR data.
//
// Parameters:
//   - prData: PullRequestData from prx package
//
// Returns:
//   - cost.PRData with all information needed for cost calculation
func PRDataFromPRX(prData *prx.PullRequestData) cost.PRData {
	pr := prData.PullRequest

	// Extract all human events with timestamps (exclude bots)
	events := extractParticipantEvents(prData.Events)

	// Handle ClosedAt pointer - use zero time if nil
	var closedAt time.Time
	if pr.ClosedAt != nil {
		closedAt = *pr.ClosedAt
	}

	return cost.PRData{
		LinesAdded:   pr.Additions,
		LinesDeleted: pr.Deletions,
		Author:       pr.Author,
		AuthorBot:    pr.AuthorBot,
		Events:       events,
		CreatedAt:    pr.CreatedAt,
		ClosedAt:     closedAt,
	}
}

// FetchPRData retrieves pull request information from GitHub and converts it
// to the format needed for cost calculation.
//
// Uses prx's CacheClient for disk-based caching with automatic cleanup.
//
// The updatedAt parameter enables effective caching. Pass the PR's updatedAt
// timestamp from GraphQL queries, or time.Now() for fresh data.
//
// Parameters:
//   - ctx: Context for the API call
//   - prURL: Full GitHub PR URL (e.g., "https://github.com/owner/repo/pull/123")
//   - token: GitHub authentication token
//   - updatedAt: PR's last update timestamp (for caching) or time.Now() to bypass cache
//
// Returns:
//   - cost.PRData with all information needed for cost calculation
func FetchPRData(ctx context.Context, prURL string, token string, updatedAt time.Time) (cost.PRData, error) {
	// Parse the PR URL to extract owner, repo, and PR number
	owner, repo, number, err := parsePRURL(prURL)
	if err != nil {
		slog.Error("Failed to parse PR URL", "url", prURL, "error", err)
		return cost.PRData{}, fmt.Errorf("invalid PR URL: %w", err)
	}

	slog.Debug("Parsed PR URL", "owner", owner, "repo", repo, "number", number)

	// Get cache directory from user's cache directory
	cacheDir, err := getCacheDir()
	if err != nil {
		slog.Warn("Failed to get cache directory, using non-cached client", "error", err)
		// Fallback to non-cached client
		client := prx.NewClient(token)
		prData, err := client.PullRequest(ctx, owner, repo, number)
		if err != nil {
			slog.Error("GitHub API call failed", "owner", owner, "repo", repo, "pr", number, "error", err)
			return cost.PRData{}, fmt.Errorf("failed to fetch PR data: %w", err)
		}
		result := PRDataFromPRX(prData)
		return result, nil
	}

	// Create prx cache client for disk-based caching
	client, err := prx.NewCacheClient(token, cacheDir)
	if err != nil {
		slog.Error("Failed to create cache client", "error", err)
		return cost.PRData{}, fmt.Errorf("failed to create cache client: %w", err)
	}

	// Fetch PR data using prx (prx has built-in retry logic and caching)
	// Pass updatedAt for effective cache validation
	slog.Debug("Calling GitHub API via prx cache client", "owner", owner, "repo", repo, "pr", number, "updated_at", updatedAt.Format(time.RFC3339))
	prData, err := client.PullRequest(ctx, owner, repo, number, updatedAt)
	if err != nil {
		slog.Error("GitHub API call failed", "owner", owner, "repo", repo, "pr", number, "error", err)
		return cost.PRData{}, fmt.Errorf("failed to fetch PR data: %w", err)
	}

	slog.Debug("GitHub API call successful",
		"additions", prData.PullRequest.Additions,
		"deletions", prData.PullRequest.Deletions,
		"author", prData.PullRequest.Author,
		"total_events", len(prData.Events))

	// Convert to cost.PRData
	result := PRDataFromPRX(prData)
	slog.Debug("Converted PR data", "human_events", len(result.Events))
	return result, nil
}

// parsePRURL extracts owner, repo, and PR number from a GitHub PR URL.
// Expected format: https://github.com/owner/repo/pull/123
//
//nolint:revive // Four return values is simpler than creating a struct wrapper
func parsePRURL(prURL string) (owner, repo string, number int, err error) {
	// Remove protocol prefix
	prURL = strings.TrimPrefix(prURL, "https://")
	prURL = strings.TrimPrefix(prURL, "http://")

	// Remove github.com prefix
	if !strings.HasPrefix(prURL, "github.com/") {
		return "", "", 0, errors.New("URL must be from github.com")
	}
	prURL = strings.TrimPrefix(prURL, "github.com/")

	// Split by /
	parts := strings.Split(prURL, "/")
	if len(parts) < 4 || parts[2] != "pull" {
		return "", "", 0, errors.New("expected format: https://github.com/owner/repo/pull/123")
	}

	number, err = strconv.Atoi(parts[3])
	if err != nil {
		return "", "", 0, fmt.Errorf("invalid PR number: %w", err)
	}

	return parts[0], parts[1], number, nil
}

// extractParticipantEvents extracts all human events with their timestamps and actors.
// Bot events are excluded - bots have zero cost for now.
//
// All human events are included:
// - Reviews
// - Comments
// - Commits
// - Force pushes
// - etc.
func extractParticipantEvents(events []prx.Event) []cost.ParticipantEvent {
	var participantEvents []cost.ParticipantEvent

	for i := range events {
		event := &events[i]
		// Skip bots, GitHub's own automation, and events with no actor
		if event.Bot || event.Actor == "github" || event.Actor == "" {
			continue
		}

		// Only include human events
		participantEvents = append(participantEvents, cost.ParticipantEvent{
			Timestamp: event.Timestamp,
			Actor:     event.Actor,
			Kind:      event.Kind,
		})
	}

	return participantEvents
}

// getCacheDir returns the cache directory for prx client.
// Uses OS-specific user cache directory with prcost subdirectory.
func getCacheDir() (string, error) {
	userCacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("get user cache dir: %w", err)
	}

	cacheDir := filepath.Join(userCacheDir, "prcost")

	// Ensure cache directory exists
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		return "", fmt.Errorf("create cache dir: %w", err)
	}

	return cacheDir, nil
}
