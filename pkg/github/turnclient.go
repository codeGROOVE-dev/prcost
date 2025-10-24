package github

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/codeGROOVE-dev/prcost/pkg/cost"
	"github.com/codeGROOVE-dev/prx/pkg/prx"
	"github.com/codeGROOVE-dev/turnclient/pkg/turn"
)

// FetchPRDataViaTurnserver retrieves pull request information from the turnserver
// and converts it to the format needed for cost calculation.
//
// The turnserver aggregates PR data and analysis, and includes full event history
// when requested. This is more efficient than calling GitHub API directly for
// complete PR data.
//
// The updatedAt parameter enables effective caching on the turnserver side. Pass
// the PR's updatedAt timestamp from GraphQL queries, or time.Now() for fresh data.
//
// Parameters:
//   - ctx: Context for the API call
//   - prURL: Full GitHub PR URL (e.g., "https://github.com/owner/repo/pull/123")
//   - token: GitHub authentication token
//   - updatedAt: PR's last update timestamp (for caching) or time.Now() to bypass cache
//
// Returns:
//   - cost.PRData with all information needed for cost calculation
func FetchPRDataViaTurnserver(ctx context.Context, prURL string, token string, updatedAt time.Time) (cost.PRData, error) {
	slog.Debug("Creating turnserver client", "url", prURL, "updated_at", updatedAt.Format(time.RFC3339))

	// Create turnserver client using default endpoint
	client, err := turn.NewDefaultClient()
	if err != nil {
		slog.Error("Failed to create turnserver client", "error", err)
		return cost.PRData{}, fmt.Errorf("create turnserver client: %w", err)
	}

	// Set authentication token
	client.SetAuthToken(token)

	// Enable event data in response - critical for cost calculation
	client.IncludeEvents()

	slog.Debug("Calling turnserver API", "url", prURL, "updated_at", updatedAt.Format(time.RFC3339))

	// Fetch PR data from turnserver
	// We use a placeholder user since we're fetching all PR data, not checking for specific user actions
	// We pass updatedAt for effective caching (turnserver caches based on this timestamp)
	response, err := client.Check(ctx, prURL, "codeGROOVE-prcost", updatedAt)
	if err != nil {
		slog.Error("Turnserver API call failed", "url", prURL, "error", err)
		return cost.PRData{}, fmt.Errorf("turnserver API call failed: %w", err)
	}

	slog.Debug("Turnserver API call successful",
		"additions", response.PullRequest.Additions,
		"deletions", response.PullRequest.Deletions,
		"author", response.PullRequest.Author,
		"total_events", len(response.Events))

	// Convert turnserver response to prx.PullRequestData format
	// The turnserver embeds prx data structures, so we can reuse them
	prData := &prx.PullRequestData{
		PullRequest: response.PullRequest,
		Events:      response.Events,
	}

	// Convert to cost.PRData using existing conversion function
	result := PRDataFromPRX(prData)
	slog.Debug("Converted PR data", "human_events", len(result.Events))
	return result, nil
}
