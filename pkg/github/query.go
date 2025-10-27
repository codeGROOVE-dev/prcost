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
// Uses an adaptive multi-query strategy for comprehensive time coverage:
//  1. Query recent activity (updated DESC) - get up to 1000 PRs
//  2. If hit limit, query old activity (updated ASC) - get ~500 more
//  3. Check gap between oldest "recent" and newest "old"
//  4. If gap > 1 week, query early period (created ASC) - get ~250 more
//
// Parameters:
//   - ctx: Context for the API call
//   - owner: GitHub repository owner
//   - repo: GitHub repository name
//   - since: Only include PRs updated after this time
//   - token: GitHub authentication token
//
// Returns:
//   - Slice of PRSummary for all matching PRs (deduplicated)
func FetchPRsFromRepo(ctx context.Context, owner, repo string, since time.Time, token string) ([]PRSummary, error) {
	// Query 1: Recent activity (updated DESC) - get up to 1000 PRs
	recent, hitLimit, err := fetchPRsFromRepoWithSort(ctx, owner, repo, since, token, "UPDATED_AT", "DESC", 1000)
	if err != nil {
		return nil, err
	}

	slog.Info("Fetched recent PRs",
		"count", len(recent),
		"hit_limit", hitLimit)

	// If we didn't hit the limit, we got all PRs within the period - done!
	if !hitLimit {
		return recent, nil
	}

	// Hit limit - need more coverage for earlier periods
	// Query 2: Old activity (updated ASC) - get ~500 more
	old, _, err := fetchPRsFromRepoWithSort(ctx, owner, repo, since, token, "UPDATED_AT", "ASC", 500)
	if err != nil {
		slog.Warn("Failed to fetch old PRs, falling back to recent only", "error", err)
		return recent, nil
	}

	slog.Info("Fetched old PRs",
		"count", len(old))

	// Check gap between oldest "recent" and newest "old"
	if len(recent) > 0 && len(old) > 0 {
		oldestRecent := recent[len(recent)-1].UpdatedAt
		newestOld := old[0].UpdatedAt
		gap := oldestRecent.Sub(newestOld)

		slog.Info("Checking time coverage gap",
			"oldest_recent", oldestRecent.Format(time.RFC3339),
			"newest_old", newestOld.Format(time.RFC3339),
			"gap_hours", gap.Hours())

		// If gap > 1 week, we have a coverage hole - fill it
		const oneWeek = 7 * 24 * time.Hour
		if gap > oneWeek {
			slog.Info("Gap > 1 week detected, fetching early period PRs to fill coverage hole")

			// Query 3: Early period (created ASC) - get ~250 more
			early, _, err := fetchPRsFromRepoWithSort(ctx, owner, repo, since, token, "CREATED_AT", "ASC", 250)
			if err != nil {
				slog.Warn("Failed to fetch early PRs, proceeding with recent+old", "error", err)
				return deduplicatePRs(append(recent, old...)), nil
			}

			slog.Info("Fetched early PRs",
				"count", len(early))

			return deduplicatePRs(append(append(recent, old...), early...)), nil
		}
	}

	// Gap <= 1 week or no gap to check - merge recent + old
	return deduplicatePRs(append(recent, old...)), nil
}

// fetchPRsFromRepoWithSort queries GitHub GraphQL API with configurable sort order.
// Returns PRs and a boolean indicating if the API limit (1000) was hit.
func fetchPRsFromRepoWithSort(
	ctx context.Context, owner, repo string, since time.Time,
	token, field, direction string, maxPRs int,
) ([]PRSummary, bool, error) {
	query := fmt.Sprintf(`
	query($owner: String!, $name: String!, $cursor: String) {
		repository(owner: $owner, name: $name) {
			pullRequests(first: 100, after: $cursor, orderBy: {field: %s, direction: %s}) {
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
	}`, field, direction)

	var allPRs []PRSummary
	var cursor *string
	pageNum := 0
	hitLimit := false

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
			return nil, false, fmt.Errorf("failed to marshal request: %w", err)
		}

		// Make GraphQL request
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.github.com/graphql", bytes.NewReader(bodyBytes))
		if err != nil {
			return nil, false, fmt.Errorf("failed to create request: %w", err)
		}

		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, false, fmt.Errorf("failed to execute request: %w", err)
		}
		//nolint:revive,gocritic // defer-in-loop: proper HTTP response cleanup pattern
		defer func() {
			if err := resp.Body.Close(); err != nil {
				slog.Warn("Failed to close response body", "error", err)
			}
		}()

		if resp.StatusCode != http.StatusOK {
			return nil, false, fmt.Errorf("GraphQL request failed with status %d", resp.StatusCode)
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
			return nil, false, fmt.Errorf("failed to decode response: %w", err)
		}

		if len(result.Errors) > 0 {
			return nil, false, fmt.Errorf("GraphQL error: %s", result.Errors[0].Message)
		}

		totalCount := result.Data.Repository.PullRequests.TotalCount
		pageSize := len(result.Data.Repository.PullRequests.Nodes)
		hasNextPage := result.Data.Repository.PullRequests.PageInfo.HasNextPage

		slog.Info("GraphQL page fetched",
			"field", field,
			"direction", direction,
			"page", pageNum,
			"page_size", pageSize,
			"total_count", totalCount,
			"has_next_page", hasNextPage)

		// Filter and collect PRs modified since the date
		for _, node := range result.Data.Repository.PullRequests.Nodes {
			if node.UpdatedAt.Before(since) {
				// For DESC queries, we can stop early
				if direction == "DESC" {
					slog.Info("Stopping pagination - encountered PR older than cutoff",
						"collected_prs", len(allPRs),
						"pages_fetched", pageNum,
						"field", field,
						"direction", direction)
					return allPRs, hitLimit, nil
				}
				// For ASC queries, skip and continue (older PRs come first)
				continue
			}
			allPRs = append(allPRs, PRSummary{
				Owner:     owner,
				Repo:      repo,
				Number:    node.Number,
				Author:    node.Author.Login,
				UpdatedAt: node.UpdatedAt,
			})

			// Check if we've hit the maxPRs limit
			if len(allPRs) >= maxPRs {
				hitLimit = true
				slog.Info("Reached max PRs limit",
					"max_prs", maxPRs,
					"field", field,
					"direction", direction)
				return allPRs, hitLimit, nil
			}
		}

		// Check if we need to fetch more pages
		if !result.Data.Repository.PullRequests.PageInfo.HasNextPage {
			break
		}
		cursor = &result.Data.Repository.PullRequests.PageInfo.EndCursor
	}

	return allPRs, hitLimit, nil
}

// deduplicatePRs removes duplicate PRs from a slice, keeping the first occurrence.
func deduplicatePRs(prs []PRSummary) []PRSummary {
	seen := make(map[int]bool)
	var unique []PRSummary

	for _, pr := range prs {
		if !seen[pr.Number] {
			seen[pr.Number] = true
			unique = append(unique, pr)
		}
	}

	slog.Info("Deduplicated PRs",
		"total", len(prs),
		"unique", len(unique),
		"duplicates", len(prs)-len(unique))

	return unique
}

// FetchPRsFromOrg queries GitHub GraphQL Search API for all PRs across
// an organization modified since the specified date.
//
// Uses an adaptive multi-query strategy for comprehensive time coverage:
//  1. Query recent activity (updated desc) - get up to 1000 PRs
//  2. If hit limit, query old activity (updated asc) - get ~500 more
//  3. Check gap between oldest "recent" and newest "old"
//  4. If gap > 1 week, query early period (created asc) - get ~250 more
//
// Parameters:
//   - ctx: Context for the API call
//   - org: GitHub organization name
//   - since: Only include PRs updated after this time
//   - token: GitHub authentication token
//
// Returns:
//   - Slice of PRSummary for all matching PRs (deduplicated)
func FetchPRsFromOrg(ctx context.Context, org string, since time.Time, token string) ([]PRSummary, error) {
	sinceStr := since.Format("2006-01-02")

	// Query 1: Recent activity (updated desc) - get up to 1000 PRs
	recent, hitLimit, err := fetchPRsFromOrgWithSort(ctx, org, sinceStr, token, "updated", "desc", 1000)
	if err != nil {
		return nil, err
	}

	slog.Info("Fetched recent PRs from org",
		"count", len(recent),
		"hit_limit", hitLimit)

	// If we didn't hit the limit, we got all PRs within the period - done!
	if !hitLimit {
		return recent, nil
	}

	// Hit limit - need more coverage for earlier periods
	// Query 2: Old activity (updated asc) - get ~500 more
	old, _, err := fetchPRsFromOrgWithSort(ctx, org, sinceStr, token, "updated", "asc", 500)
	if err != nil {
		slog.Warn("Failed to fetch old PRs from org, falling back to recent only", "error", err)
		return recent, nil
	}

	slog.Info("Fetched old PRs from org",
		"count", len(old))

	// Check gap between oldest "recent" and newest "old"
	if len(recent) > 0 && len(old) > 0 {
		oldestRecent := recent[len(recent)-1].UpdatedAt
		newestOld := old[0].UpdatedAt
		gap := oldestRecent.Sub(newestOld)

		slog.Info("Checking time coverage gap (org)",
			"oldest_recent", oldestRecent.Format(time.RFC3339),
			"newest_old", newestOld.Format(time.RFC3339),
			"gap_hours", gap.Hours())

		// If gap > 1 week, we have a coverage hole - fill it
		const oneWeek = 7 * 24 * time.Hour
		if gap > oneWeek {
			slog.Info("Gap > 1 week detected, fetching early period PRs to fill coverage hole (org)")

			// Query 3: Early period (created asc) - get ~250 more
			early, _, err := fetchPRsFromOrgWithSort(ctx, org, sinceStr, token, "created", "asc", 250)
			if err != nil {
				slog.Warn("Failed to fetch early PRs from org, proceeding with recent+old", "error", err)
				return deduplicatePRsByOwnerRepoNumber(append(recent, old...)), nil
			}

			slog.Info("Fetched early PRs from org",
				"count", len(early))

			return deduplicatePRsByOwnerRepoNumber(append(append(recent, old...), early...)), nil
		}
	}

	// Gap <= 1 week or no gap to check - merge recent + old
	return deduplicatePRsByOwnerRepoNumber(append(recent, old...)), nil
}

// fetchPRsFromOrgWithSort queries GitHub Search API with configurable sort order.
// Returns PRs and a boolean indicating if the API limit (1000) was hit.
func fetchPRsFromOrgWithSort(
	ctx context.Context, org, sinceStr, token, field, direction string, maxPRs int,
) ([]PRSummary, bool, error) {
	// Build search query with sort
	// Query format: org:myorg is:pr updated:>2025-07-25 sort:updated-desc
	searchQuery := fmt.Sprintf("org:%s is:pr %s:>%s sort:%s-%s", org, field, sinceStr, field, direction)

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
	hitLimit := false

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
			return nil, false, fmt.Errorf("failed to marshal request: %w", err)
		}

		// Make GraphQL request
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.github.com/graphql", bytes.NewReader(bodyBytes))
		if err != nil {
			return nil, false, fmt.Errorf("failed to create request: %w", err)
		}

		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, false, fmt.Errorf("failed to execute request: %w", err)
		}
		//nolint:revive,gocritic // defer-in-loop: proper HTTP response cleanup pattern
		defer func() {
			if err := resp.Body.Close(); err != nil {
				slog.Warn("Failed to close response body", "error", err)
			}
		}()

		if resp.StatusCode != http.StatusOK {
			return nil, false, fmt.Errorf("GraphQL request failed with status %d", resp.StatusCode)
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
			return nil, false, fmt.Errorf("failed to decode response: %w", err)
		}

		if len(result.Errors) > 0 {
			return nil, false, fmt.Errorf("GraphQL error: %s", result.Errors[0].Message)
		}

		totalCount := result.Data.Search.IssueCount
		pageSize := len(result.Data.Search.Nodes)
		hasNextPage := result.Data.Search.PageInfo.HasNextPage

		slog.Info("GraphQL search page fetched",
			"field", field,
			"direction", direction,
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

			// Check if we've hit the maxPRs limit
			if len(allPRs) >= maxPRs {
				hitLimit = true
				slog.Info("Reached max PRs limit (org)",
					"max_prs", maxPRs,
					"field", field,
					"direction", direction)
				return allPRs, hitLimit, nil
			}
		}

		// Check if we need to fetch more pages
		if !result.Data.Search.PageInfo.HasNextPage {
			break
		}
		cursor = &result.Data.Search.PageInfo.EndCursor
	}

	return allPRs, hitLimit, nil
}

// deduplicatePRsByOwnerRepoNumber removes duplicate PRs from a slice using owner+repo+number as key.
func deduplicatePRsByOwnerRepoNumber(prs []PRSummary) []PRSummary {
	type key struct {
		owner  string
		repo   string
		number int
	}
	seen := make(map[key]bool)
	var unique []PRSummary

	for _, pr := range prs {
		k := key{owner: pr.Owner, repo: pr.Repo, number: pr.Number}
		if !seen[k] {
			seen[k] = true
			unique = append(unique, pr)
		}
	}

	slog.Info("Deduplicated org PRs",
		"total", len(prs),
		"unique", len(unique),
		"duplicates", len(prs)-len(unique))

	return unique
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

// CalculateActualTimeWindow validates time coverage for the fetched PRs.
// With the multi-query approach, we fetch PRs to cover the full requested period.
// This function logs coverage statistics but always returns the requested period.
//
// Parameters:
//   - prs: List of PRs fetched (may be from multiple queries)
//   - requestedDays: Number of days originally requested
//
// Returns:
//   - actualDays: Always returns requestedDays (multi-query ensures coverage)
//   - hitLimit: Always returns false (no period adjustment needed)
func CalculateActualTimeWindow(prs []PRSummary, requestedDays int) (actualDays int, hitLimit bool) {
	// If no PRs, return requested days
	if len(prs) == 0 {
		return requestedDays, false
	}

	// Calculate coverage statistics for logging
	requestedSince := time.Now().AddDate(0, 0, -requestedDays)
	oldestTime := prs[len(prs)-1].UpdatedAt
	timeSinceOldestPR := time.Since(oldestTime)
	requestedDuration := time.Since(requestedSince)
	coverageGap := requestedDuration - timeSinceOldestPR

	slog.Info("Time coverage analysis",
		"requested_days", requestedDays,
		"total_prs", len(prs),
		"oldest_pr_age_days", int(timeSinceOldestPR.Hours()/24.0),
		"coverage_gap_days", int(coverageGap.Hours()/24.0),
		"newest_pr", prs[0].UpdatedAt.Format(time.RFC3339),
		"oldest_pr", oldestTime.Format(time.RFC3339))

	// Always return requested period - multi-query approach ensures best possible coverage
	return requestedDays, false
}
