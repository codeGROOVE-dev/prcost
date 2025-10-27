package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"
)

// PRSummary holds minimal information about a PR for sampling and fetching.
//
//nolint:govet // fieldalignment: struct field order optimized for readability
type PRSummary struct {
	Owner     string    // Repository owner
	Repo      string    // Repository name
	Number    int       // PR number
	Author    string    // PR author login
	UpdatedAt time.Time // Last update time
}

// FetchPRsFromRepo queries GitHub GraphQL API for all PRs in a repository
// modified since the specified date.
//
// Parameters:
//   - ctx: Context for the API call
//   - owner: GitHub repository owner
//   - repo: GitHub repository name
//   - since: Only include PRs updated after this time
//   - token: GitHub authentication token
//
// Returns:
//   - Slice of PRSummary for all matching PRs
func FetchPRsFromRepo(ctx context.Context, owner, repo string, since time.Time, token string) ([]PRSummary, error) {
	const query = `
	query($owner: String!, $name: String!, $cursor: String) {
		repository(owner: $owner, name: $name) {
			pullRequests(first: 100, after: $cursor, orderBy: {field: UPDATED_AT, direction: DESC}) {
				totalCount
				pageInfo {
					hasNextPage
					endCursor
				}
				nodes {
					number
					updatedAt
					author {
						login
					}
				}
			}
		}
	}`

	var allPRs []PRSummary
	var cursor *string
	pageNum := 0

	for {
		pageNum++
		// Build request body
		variables := map[string]any{
			"owner": owner,
			"name":  repo,
		}
		if cursor != nil {
			variables["cursor"] = *cursor
		}

		requestBody := map[string]any{
			"query":     query,
			"variables": variables,
		}

		bodyBytes, err := json.Marshal(requestBody)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request: %w", err)
		}

		// Make GraphQL request
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.github.com/graphql", bytes.NewReader(bodyBytes))
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}

		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("failed to execute request: %w", err)
		}
		//nolint:revive,gocritic // defer-in-loop: proper HTTP response cleanup pattern
		defer func() {
			if err := resp.Body.Close(); err != nil {
				slog.Warn("Failed to close response body", "error", err)
			}
		}()

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("GraphQL request failed with status %d", resp.StatusCode)
		}

		// Parse response
		//nolint:govet // fieldalignment: anonymous GraphQL response struct
		var result struct {
			Data struct {
				Repository struct {
					PullRequests struct {
						TotalCount int
						PageInfo   struct {
							HasNextPage bool
							EndCursor   string
						}
						Nodes []struct {
							Number    int
							UpdatedAt time.Time
							Author    struct {
								Login string
							}
						}
					}
				}
			}
			Errors []struct {
				Message string
			}
		}

		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return nil, fmt.Errorf("failed to decode response: %w", err)
		}

		if len(result.Errors) > 0 {
			return nil, fmt.Errorf("GraphQL error: %s", result.Errors[0].Message)
		}

		totalCount := result.Data.Repository.PullRequests.TotalCount
		pageSize := len(result.Data.Repository.PullRequests.Nodes)
		hasNextPage := result.Data.Repository.PullRequests.PageInfo.HasNextPage

		slog.Info("GraphQL page fetched",
			"page", pageNum,
			"page_size", pageSize,
			"total_count", totalCount,
			"has_next_page", hasNextPage)

		// Filter and collect PRs modified since the date
		for _, node := range result.Data.Repository.PullRequests.Nodes {
			if node.UpdatedAt.Before(since) {
				// PRs are ordered by updatedAt DESC, so we can stop here
				slog.Info("Stopping pagination - encountered PR older than cutoff date",
					"collected_prs", len(allPRs),
					"pages_fetched", pageNum,
					"pr_number", node.Number,
					"pr_updated_at", node.UpdatedAt.Format(time.RFC3339))
				return allPRs, nil
			}
			allPRs = append(allPRs, PRSummary{
				Owner:     owner,
				Repo:      repo,
				Number:    node.Number,
				Author:    node.Author.Login,
				UpdatedAt: node.UpdatedAt,
			})
		}

		// Check if we need to fetch more pages
		if !result.Data.Repository.PullRequests.PageInfo.HasNextPage {
			break
		}
		cursor = &result.Data.Repository.PullRequests.PageInfo.EndCursor
	}

	return allPRs, nil
}

// FetchPRsFromOrg queries GitHub GraphQL Search API for all PRs across
// an organization modified since the specified date.
//
// Parameters:
//   - ctx: Context for the API call
//   - org: GitHub organization name
//   - since: Only include PRs updated after this time
//   - token: GitHub authentication token
//
// Returns:
//   - Slice of PRSummary for all matching PRs
func FetchPRsFromOrg(ctx context.Context, org string, since time.Time, token string) ([]PRSummary, error) {
	// Use GitHub search API to search across organization
	// Query: org:myorg is:pr updated:>2025-07-25 sort:updated-desc
	sinceStr := since.Format("2006-01-02")
	searchQuery := fmt.Sprintf("org:%s is:pr updated:>%s sort:updated-desc", org, sinceStr)

	const query = `
	query($searchQuery: String!, $cursor: String) {
		search(query: $searchQuery, type: ISSUE, first: 100, after: $cursor) {
			issueCount
			pageInfo {
				hasNextPage
				endCursor
			}
			nodes {
				... on PullRequest {
					number
					updatedAt
					author {
						login
					}
					repository {
						owner {
							login
						}
						name
					}
				}
			}
		}
	}`

	var allPRs []PRSummary
	var cursor *string
	pageNum := 0

	for {
		pageNum++
		// Build request body
		variables := map[string]any{
			"searchQuery": searchQuery,
		}
		if cursor != nil {
			variables["cursor"] = *cursor
		}

		requestBody := map[string]any{
			"query":     query,
			"variables": variables,
		}

		bodyBytes, err := json.Marshal(requestBody)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request: %w", err)
		}

		// Make GraphQL request
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.github.com/graphql", bytes.NewReader(bodyBytes))
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}

		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("failed to execute request: %w", err)
		}
		//nolint:revive,gocritic // defer-in-loop: proper HTTP response cleanup pattern
		defer func() {
			if err := resp.Body.Close(); err != nil {
				slog.Warn("Failed to close response body", "error", err)
			}
		}()

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("GraphQL request failed with status %d", resp.StatusCode)
		}

		// Parse response
		//nolint:govet // fieldalignment: anonymous GraphQL response struct
		var result struct {
			Data struct {
				Search struct {
					IssueCount int
					PageInfo   struct {
						HasNextPage bool
						EndCursor   string
					}
					Nodes []struct {
						Number    int
						UpdatedAt time.Time
						Author    struct {
							Login string
						}
						Repository struct {
							Owner struct {
								Login string
							}
							Name string
						}
					}
				}
			}
			Errors []struct {
				Message string
			}
		}

		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return nil, fmt.Errorf("failed to decode response: %w", err)
		}

		if len(result.Errors) > 0 {
			return nil, fmt.Errorf("GraphQL error: %s", result.Errors[0].Message)
		}

		totalCount := result.Data.Search.IssueCount
		pageSize := len(result.Data.Search.Nodes)
		hasNextPage := result.Data.Search.PageInfo.HasNextPage

		slog.Info("GraphQL search page fetched",
			"page", pageNum,
			"page_size", pageSize,
			"total_count", totalCount,
			"has_next_page", hasNextPage)

		// Collect PRs from this page
		for _, node := range result.Data.Search.Nodes {
			allPRs = append(allPRs, PRSummary{
				Owner:     node.Repository.Owner.Login,
				Repo:      node.Repository.Name,
				Number:    node.Number,
				Author:    node.Author.Login,
				UpdatedAt: node.UpdatedAt,
			})
		}

		// Check if we need to fetch more pages
		if !result.Data.Search.PageInfo.HasNextPage {
			break
		}
		cursor = &result.Data.Search.PageInfo.EndCursor
	}

	return allPRs, nil
}

// isBot returns true if the author name indicates a bot account.
func isBot(author string) bool {
	// Check for common bot name patterns
	if strings.HasSuffix(author, "[bot]") || strings.Contains(author, "-bot-") {
		return true
	}

	// Check for specific known bot usernames (case-insensitive)
	lowerAuthor := strings.ToLower(author)
	knownBots := []string{
		"renovate",
		"dependabot",
		"github-actions",
		"codecov",
		"snyk",
		"greenkeeper",
		"imgbot",
		"renovate-bot",
		"dependabot-preview",
	}

	for _, botName := range knownBots {
		if lowerAuthor == botName {
			return true
		}
	}

	return false
}

// SamplePRs uses a time-bucket strategy to evenly sample PRs across the time range.
// This ensures samples are distributed throughout the period rather than clustered.
// Bot-authored PRs are excluded from sampling.
//
// Parameters:
//   - prs: List of PRs to sample from
//   - sampleSize: Desired number of samples
//
// Returns:
//   - Slice of sampled PRs (may be smaller than sampleSize if insufficient PRs)
//
// Strategy:
//   - Filters out bot-authored PRs
//   - Divides time range into buckets equal to sampleSize
//   - Selects most recent PR from each bucket
//   - If buckets are empty, fills with nearest unused PRs
func SamplePRs(prs []PRSummary, sampleSize int) []PRSummary {
	if len(prs) == 0 {
		return nil
	}

	// Filter out bot-authored PRs
	var humanPRs []PRSummary
	for _, pr := range prs {
		if !isBot(pr.Author) {
			humanPRs = append(humanPRs, pr)
		}
	}

	if len(humanPRs) == 0 {
		return nil
	}

	// If we have fewer PRs than samples, return all
	if len(humanPRs) <= sampleSize {
		return humanPRs
	}

	prs = humanPRs

	// Sort PRs by updatedAt (newest first)
	sorted := make([]PRSummary, len(prs))
	copy(sorted, prs)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].UpdatedAt.After(sorted[j].UpdatedAt)
	})

	// Calculate time range
	newest := sorted[0].UpdatedAt
	oldest := sorted[len(sorted)-1].UpdatedAt
	totalDuration := newest.Sub(oldest)

	// Calculate bucket size
	bucketDuration := totalDuration / time.Duration(sampleSize)

	slog.Info("Time bucket sampling",
		"newest", newest.Format(time.RFC3339),
		"oldest", oldest.Format(time.RFC3339),
		"bucket_duration", bucketDuration,
		"num_buckets", sampleSize)

	// Create time buckets and assign PRs
	type bucket struct {
		startTime time.Time
		endTime   time.Time
		prs       []PRSummary
	}

	buckets := make([]bucket, sampleSize)
	for i := range sampleSize {
		buckets[i].startTime = newest.Add(-time.Duration(i+1) * bucketDuration)
		buckets[i].endTime = newest.Add(-time.Duration(i) * bucketDuration)
	}

	// Assign PRs to buckets
	for _, pr := range sorted {
		for i := range buckets {
			if (pr.UpdatedAt.After(buckets[i].startTime) || pr.UpdatedAt.Equal(buckets[i].startTime)) &&
				(pr.UpdatedAt.Before(buckets[i].endTime) || pr.UpdatedAt.Equal(buckets[i].endTime)) {
				buckets[i].prs = append(buckets[i].prs, pr)
				break
			}
		}
	}

	// Select one PR from each bucket (most recent in bucket)
	var samples []PRSummary
	used := make(map[int]bool)

	for _, b := range buckets {
		if len(b.prs) > 0 {
			// Pick most recent PR in bucket
			samples = append(samples, b.prs[0])
			used[b.prs[0].Number] = true
		}
	}

	// If some buckets were empty, fill with nearest unused PRs
	if len(samples) < sampleSize {
		for _, pr := range sorted {
			if len(samples) >= sampleSize {
				break
			}
			if !used[pr.Number] {
				samples = append(samples, pr)
				used[pr.Number] = true
			}
		}
	}

	return samples
}

// CountUniqueAuthors counts the number of unique authors in a slice of PRSummary.
// Bot authors are excluded from the count.
func CountUniqueAuthors(prs []PRSummary) int {
	uniqueAuthors := make(map[string]bool)
	for _, pr := range prs {
		if !isBot(pr.Author) {
			uniqueAuthors[pr.Author] = true
		}
	}
	return len(uniqueAuthors)
}

// CalculateActualTimeWindow determines the actual time period covered by the PRs.
// When we hit the 1000 PR API limit, we may not have data for the full requested period.
// This function calculates the actual time window based on the oldest PR's updatedAt time.
//
// Parameters:
//   - prs: List of PRs fetched
//   - requestedDays: Number of days originally requested
//
// Returns:
//   - actualDays: The actual number of days covered by the PR data
//   - hitLimit: Whether we likely hit the API limit (exactly 1000 PRs)
func CalculateActualTimeWindow(prs []PRSummary, requestedDays int) (actualDays int, hitLimit bool) {
	// If no PRs, return requested days
	if len(prs) == 0 {
		return requestedDays, false
	}

	// GitHub Search API has a hard limit of 1000 results
	// If we hit exactly 1000, we likely hit the limit and are missing data
	const apiLimit = 1000
	hitLimit = len(prs) >= apiLimit

	// If we didn't hit the limit, use the requested days
	if !hitLimit {
		return requestedDays, false
	}

	// PRs are sorted by updatedAt DESC (newest first), so the last PR is the oldest
	newestTime := prs[0].UpdatedAt
	oldestTime := prs[len(prs)-1].UpdatedAt

	// Calculate actual days from oldest PR to now
	actualDuration := time.Since(oldestTime)
	actualDays = int(actualDuration.Hours() / 24.0)

	// Round up to avoid showing 0 days
	if actualDays == 0 {
		actualDays = 1
	}

	// Create distribution by day to verify sort order and show clustering
	dayDistribution := make(map[string]int)
	for _, pr := range prs {
		dayKey := pr.UpdatedAt.Format("2006-01-02")
		dayDistribution[dayKey]++
	}

	// Log first 10 and last 10 PRs to verify sort order
	slog.Info("PR sort order verification",
		"first_3_prs", []string{
			prs[0].UpdatedAt.Format(time.RFC3339),
			prs[1].UpdatedAt.Format(time.RFC3339),
			prs[2].UpdatedAt.Format(time.RFC3339),
		},
		"last_3_prs", []string{
			prs[len(prs)-3].UpdatedAt.Format(time.RFC3339),
			prs[len(prs)-2].UpdatedAt.Format(time.RFC3339),
			prs[len(prs)-1].UpdatedAt.Format(time.RFC3339),
		})

	slog.Info("PR distribution by day", "distribution", dayDistribution)

	slog.Info("Detected API limit - adjusted time window",
		"requested_days", requestedDays,
		"actual_days", actualDays,
		"total_prs", len(prs),
		"newest_pr_updated_at", newestTime.Format(time.RFC3339),
		"oldest_pr_updated_at", oldestTime.Format(time.RFC3339),
		"prs_per_day", float64(len(prs))/float64(actualDays))

	return actualDays, true
}
