// Package github fetches pull request data from GitHub using prx.
package github

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"

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

	// Determine if author has write access
	// author_write_access > 0 means they have write access
	// author_write_access <= 0 means external contributor
	authorHasWriteAccess := pr.AuthorWriteAccess > 0

	return cost.PRData{
		LinesAdded:           pr.Additions,
		Author:               pr.Author,
		Events:               events,
		CreatedAt:            pr.CreatedAt,
		UpdatedAt:            pr.UpdatedAt,
		AuthorHasWriteAccess: authorHasWriteAccess,
	}
}

// FetchPRData retrieves pull request information from GitHub and converts it
// to the format needed for cost calculation.
//
// Parameters:
//   - ctx: Context for the API call
//   - prURL: Full GitHub PR URL (e.g., "https://github.com/owner/repo/pull/123")
//   - token: GitHub authentication token
//
// Returns:
//   - cost.PRData with all information needed for cost calculation
func FetchPRData(ctx context.Context, prURL string, token string) (cost.PRData, error) {
	// Parse the PR URL to extract owner, repo, and PR number
	owner, repo, number, err := parsePRURL(prURL)
	if err != nil {
		slog.Error("Failed to parse PR URL", "url", prURL, "error", err)
		return cost.PRData{}, fmt.Errorf("invalid PR URL: %w", err)
	}

	slog.Debug("Parsed PR URL", "owner", owner, "repo", repo, "number", number)

	// Create prx client
	client := prx.NewClient(token)

	// Fetch PR data using prx (prx has built-in retry logic)
	slog.Debug("Calling GitHub API via prx", "owner", owner, "repo", repo, "pr", number)
	prData, err := client.PullRequest(ctx, owner, repo, number)
	if err != nil {
		slog.Error("GitHub API call failed", "owner", owner, "repo", repo, "pr", number, "error", err)
		return cost.PRData{}, fmt.Errorf("failed to fetch PR data: %w", err)
	}

	slog.Debug("GitHub API call successful",
		"additions", prData.PullRequest.Additions,
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

// FetchPRDataWithDefaults is a convenience function that uses environment variables
// for authentication.
//
// Parameters:
//   - ctx: Context for the API call
//   - prURL: Full GitHub PR URL (e.g., "https://github.com/owner/repo/pull/123")
//
// Returns:
//   - cost.PRData with all information needed for cost calculation
func FetchPRDataWithDefaults(ctx context.Context, prURL string) (cost.PRData, error) {
	// Get GitHub token from environment
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		return cost.PRData{}, errors.New("GITHUB_TOKEN environment variable not set")
	}

	return FetchPRData(ctx, prURL, token)
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
		// Skip bots and GitHub's own automation
		if event.Bot || event.Actor == "github" {
			continue
		}

		// Only include human events
		participantEvents = append(participantEvents, cost.ParticipantEvent{
			Timestamp: event.Timestamp,
			Actor:     event.Actor,
		})
	}

	return participantEvents
}
