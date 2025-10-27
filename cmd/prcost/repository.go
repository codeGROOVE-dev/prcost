package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/codeGROOVE-dev/prcost/pkg/cost"
	"github.com/codeGROOVE-dev/prcost/pkg/github"
)

// countBotPRs counts how many PRs in the list are authored by bots.
// Uses the same bot detection logic as pkg/github/query.go:isBot().
func countBotPRs(prs []github.PRSummary) int {
	count := 0
	for _, pr := range prs {
		if isBotAuthor(pr.Author) {
			count++
		}
	}
	return count
}

// isBotAuthor returns true if the author name indicates a bot account.
// This matches the logic in pkg/github/query.go:isBot().
func isBotAuthor(author string) bool {
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

// analyzeRepository performs repository-wide cost analysis by sampling PRs.
// Uses library functions from pkg/github and pkg/cost for fetching, sampling,
// and extrapolation - all functionality is available to external clients.
func analyzeRepository(ctx context.Context, owner, repo string, sampleSize, days int, cfg cost.Config, token string, dataSource string) error {
	slog.Info("Fetching PR list from repository")

	// Calculate since date
	since := time.Now().AddDate(0, 0, -days)

	// Fetch all PRs modified since the date using library function
	prs, err := github.FetchPRsFromRepo(ctx, owner, repo, since, token)
	if err != nil {
		return fmt.Errorf("failed to fetch PRs: %w", err)
	}

	slog.Info("Fetched PRs from repository",
		"total_prs", len(prs),
		"since", since.Format("2006-01-02"))

	if len(prs) == 0 {
		fmt.Printf("\nNo PRs modified in the last %d days\n", days)
		return nil
	}

	// Calculate actual time window (may be less than requested if we hit API limit)
	actualDays, hitLimit := github.CalculateActualTimeWindow(prs, days)
	if hitLimit {
		fmt.Printf("\nNote: Hit GitHub API limit of 1000 PRs. Adjusting analysis period from %d to %d days.\n", days, actualDays)
	}

	// Count bot PRs before sampling
	botPRCount := countBotPRs(prs)
	humanPRCount := len(prs) - botPRCount

	// Sample PRs using time-bucket strategy (filters out bot PRs)
	samples := github.SamplePRs(prs, sampleSize)

	slog.Info("Sampled PRs for analysis",
		"total_prs", len(prs),
		"human_prs", humanPRCount,
		"bot_prs", botPRCount,
		"sample_size", len(samples),
		"requested_samples", sampleSize)

	if botPRCount > 0 {
		fmt.Printf("\nAnalyzing %d sampled PRs from %d human PRs (%d bot PRs excluded) modified in the last %d days...\n\n",
			len(samples), humanPRCount, botPRCount, actualDays)
	} else {
		fmt.Printf("\nAnalyzing %d sampled PRs from %d total PRs modified in the last %d days...\n\n",
			len(samples), len(prs), actualDays)
	}

	// Collect breakdowns from each sample using parallel processing
	var breakdowns []cost.Breakdown
	var mu sync.Mutex
	var wg sync.WaitGroup

	// Use a buffered channel for worker pool pattern (same as web server)
	concurrency := 8 // Process up to 8 PRs concurrently
	semaphore := make(chan struct{}, concurrency)

	for i, pr := range samples {
		wg.Add(1)
		go func(index int, prSummary github.PRSummary) {
			defer wg.Done()

			// Acquire semaphore slot
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			prURL := fmt.Sprintf("https://github.com/%s/%s/pull/%d", owner, repo, prSummary.Number)
			slog.Info("Processing sample PR",
				"repo", fmt.Sprintf("%s/%s", owner, repo),
				"number", prSummary.Number,
				"progress", fmt.Sprintf("%d/%d", index+1, len(samples)))

			// Fetch full PR data using configured data source
			var prData cost.PRData
			var err error
			if dataSource == "turnserver" {
				// Use turnserver with updatedAt for effective caching
				prData, err = github.FetchPRDataViaTurnserver(ctx, prURL, token, prSummary.UpdatedAt)
			} else {
				// Use prx with updatedAt for effective caching
				prData, err = github.FetchPRData(ctx, prURL, token, prSummary.UpdatedAt)
			}
			if err != nil {
				slog.Warn("Failed to fetch PR data, skipping", "pr_number", prSummary.Number, "source", dataSource, "error", err)
				return
			}

			// Calculate cost and accumulate with mutex protection
			breakdown := cost.Calculate(prData, cfg)
			mu.Lock()
			breakdowns = append(breakdowns, breakdown)
			mu.Unlock()
		}(i, pr)
	}

	// Wait for all goroutines to complete
	wg.Wait()

	if len(breakdowns) == 0 {
		return errors.New("no samples could be processed successfully")
	}

	// Count unique authors across all PRs (not just samples)
	totalAuthors := github.CountUniqueAuthors(prs)
	slog.Info("Counted unique authors across all PRs", "total_authors", totalAuthors)

	// Extrapolate costs from samples using library function
	extrapolated := cost.ExtrapolateFromSamples(breakdowns, len(prs), totalAuthors, actualDays, cfg)

	// Display results in itemized format
	printExtrapolatedResults(fmt.Sprintf("%s/%s", owner, repo), actualDays, &extrapolated, cfg)

	return nil
}

// analyzeOrganization performs organization-wide cost analysis by sampling PRs across all repos.
// Uses library functions from pkg/github and pkg/cost for fetching, sampling,
// and extrapolation - all functionality is available to external clients.
func analyzeOrganization(ctx context.Context, org string, sampleSize, days int, cfg cost.Config, token string, dataSource string) error {
	slog.Info("Fetching PR list from organization")

	// Calculate since date
	since := time.Now().AddDate(0, 0, -days)

	// Fetch all PRs across the org modified since the date using library function
	prs, err := github.FetchPRsFromOrg(ctx, org, since, token)
	if err != nil {
		return fmt.Errorf("failed to fetch PRs: %w", err)
	}

	slog.Info("Fetched PRs from organization",
		"total_prs", len(prs),
		"since", since.Format("2006-01-02"))

	if len(prs) == 0 {
		fmt.Printf("\nNo PRs modified in the last %d days\n", days)
		return nil
	}

	// Calculate actual time window (may be less than requested if we hit API limit)
	actualDays, hitLimit := github.CalculateActualTimeWindow(prs, days)
	if hitLimit {
		fmt.Printf("\nNote: Hit GitHub API limit of 1000 PRs. Adjusting analysis period from %d to %d days.\n", days, actualDays)
	}

	// Count bot PRs before sampling
	botPRCount := countBotPRs(prs)
	humanPRCount := len(prs) - botPRCount

	// Sample PRs using time-bucket strategy (filters out bot PRs)
	samples := github.SamplePRs(prs, sampleSize)

	slog.Info("Sampled PRs for analysis",
		"total_prs", len(prs),
		"human_prs", humanPRCount,
		"bot_prs", botPRCount,
		"sample_size", len(samples),
		"requested_samples", sampleSize)

	if botPRCount > 0 {
		fmt.Printf("\nAnalyzing %d sampled PRs from %d human PRs (%d bot PRs excluded) across %s (last %d days)...\n\n",
			len(samples), humanPRCount, botPRCount, org, actualDays)
	} else {
		fmt.Printf("\nAnalyzing %d sampled PRs from %d total PRs across %s (last %d days)...\n\n",
			len(samples), len(prs), org, actualDays)
	}

	// Collect breakdowns from each sample using parallel processing
	var breakdowns []cost.Breakdown
	var mu sync.Mutex
	var wg sync.WaitGroup

	// Use a buffered channel for worker pool pattern (same as web server)
	concurrency := 8 // Process up to 8 PRs concurrently
	semaphore := make(chan struct{}, concurrency)

	for i, pr := range samples {
		wg.Add(1)
		go func(index int, prSummary github.PRSummary) {
			defer wg.Done()

			// Acquire semaphore slot
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			prURL := fmt.Sprintf("https://github.com/%s/%s/pull/%d", prSummary.Owner, prSummary.Repo, prSummary.Number)
			slog.Info("Processing sample PR",
				"repo", fmt.Sprintf("%s/%s", prSummary.Owner, prSummary.Repo),
				"number", prSummary.Number,
				"progress", fmt.Sprintf("%d/%d", index+1, len(samples)))

			// Fetch full PR data using configured data source
			var prData cost.PRData
			var err error
			if dataSource == "turnserver" {
				// Use turnserver with updatedAt for effective caching
				prData, err = github.FetchPRDataViaTurnserver(ctx, prURL, token, prSummary.UpdatedAt)
			} else {
				// Use prx with updatedAt for effective caching
				prData, err = github.FetchPRData(ctx, prURL, token, prSummary.UpdatedAt)
			}
			if err != nil {
				slog.Warn("Failed to fetch PR data, skipping", "pr_number", prSummary.Number, "source", dataSource, "error", err)
				return
			}

			// Calculate cost and accumulate with mutex protection
			breakdown := cost.Calculate(prData, cfg)
			mu.Lock()
			breakdowns = append(breakdowns, breakdown)
			mu.Unlock()
		}(i, pr)
	}

	// Wait for all goroutines to complete
	wg.Wait()

	if len(breakdowns) == 0 {
		return errors.New("no samples could be processed successfully")
	}

	// Count unique authors across all PRs (not just samples)
	totalAuthors := github.CountUniqueAuthors(prs)
	slog.Info("Counted unique authors across all PRs", "total_authors", totalAuthors)

	// Extrapolate costs from samples using library function
	extrapolated := cost.ExtrapolateFromSamples(breakdowns, len(prs), totalAuthors, actualDays, cfg)

	// Display results in itemized format
	printExtrapolatedResults(fmt.Sprintf("%s (organization)", org), actualDays, &extrapolated, cfg)

	return nil
}

// formatTimeUnit intelligently scales time units based on magnitude.
// Once a value exceeds 2x a unit, it scales to the next unit:
// - < 1 hour: show as minutes
// - >= 1 hour and < 48 hours: show as hours
// - >= 48 hours and < 14 days: show as days
// - >= 14 days and < 56 days: show as weeks
// - >= 56 days and < 730 days: show as months
// - >= 730 days: show as years.
func formatTimeUnit(hours float64) string {
	// Show minutes for values less than 1 hour
	if hours < 1.0 {
		minutes := hours * 60.0
		// Use 1 decimal place for better precision and clearer addition
		return fmt.Sprintf("%.1f min", minutes)
	}

	if hours < 48 {
		return fmt.Sprintf("%.1f hrs", hours)
	}

	days := hours / 24.0
	if days < 14 {
		return fmt.Sprintf("%.1f days", days)
	}

	weeks := days / 7.0
	if weeks < 8 {
		return fmt.Sprintf("%.1f weeks", weeks)
	}

	months := days / 30.0
	if months < 24 {
		return fmt.Sprintf("%.1f months", months)
	}

	years := days / 365.0
	return fmt.Sprintf("%.1f years", years)
}

// printExtrapolatedResults displays extrapolated cost breakdown in itemized format.
//
//nolint:maintidx,revive // acceptable complexity/length for comprehensive display function
func printExtrapolatedResults(title string, days int, ext *cost.ExtrapolatedBreakdown, cfg cost.Config) {
	fmt.Println()
	fmt.Printf("  %s\n", title)
	avgOpenTime := formatTimeUnit(ext.AvgPRDurationHours)
	fmt.Printf("  Period: Last %d days  •  Total PRs: %d  •  Authors: %d  •  Sampled: %d  •  Avg Open Time: %s\n", days, ext.TotalPRs, ext.TotalAuthors, ext.SuccessfulSamples, avgOpenTime)
	fmt.Println()

	// Calculate average per PR
	avgAuthorNewCodeCost := ext.AuthorNewCodeCost / float64(ext.TotalPRs)
	avgAuthorAdaptationCost := ext.AuthorAdaptationCost / float64(ext.TotalPRs)
	avgAuthorGitHubCost := ext.AuthorGitHubCost / float64(ext.TotalPRs)
	avgAuthorGitHubContextCost := ext.AuthorGitHubContextCost / float64(ext.TotalPRs)
	avgAuthorTotalCost := ext.AuthorTotalCost / float64(ext.TotalPRs)
	avgAuthorNewCodeHours := ext.AuthorNewCodeHours / float64(ext.TotalPRs)
	avgAuthorAdaptationHours := ext.AuthorAdaptationHours / float64(ext.TotalPRs)
	avgAuthorGitHubHours := ext.AuthorGitHubHours / float64(ext.TotalPRs)
	avgAuthorGitHubContextHours := ext.AuthorGitHubContextHours / float64(ext.TotalPRs)
	avgAuthorTotalHours := ext.AuthorTotalHours / float64(ext.TotalPRs)

	avgParticipantReviewCost := ext.ParticipantReviewCost / float64(ext.TotalPRs)
	avgParticipantGitHubCost := ext.ParticipantGitHubCost / float64(ext.TotalPRs)
	avgParticipantContextCost := ext.ParticipantContextCost / float64(ext.TotalPRs)
	avgParticipantTotalCost := ext.ParticipantTotalCost / float64(ext.TotalPRs)
	avgParticipantReviewHours := ext.ParticipantReviewHours / float64(ext.TotalPRs)
	avgParticipantGitHubHours := ext.ParticipantGitHubHours / float64(ext.TotalPRs)
	avgParticipantContextHours := ext.ParticipantContextHours / float64(ext.TotalPRs)
	avgParticipantTotalHours := ext.ParticipantTotalHours / float64(ext.TotalPRs)

	avgDeliveryDelayCost := ext.DeliveryDelayCost / float64(ext.TotalPRs)
	avgCoordinationCost := ext.CoordinationCost / float64(ext.TotalPRs)
	avgCodeChurnCost := ext.CodeChurnCost / float64(ext.TotalPRs)
	avgDeliveryDelayHours := ext.DeliveryDelayHours / float64(ext.TotalPRs)
	avgCoordinationHours := ext.CoordinationHours / float64(ext.TotalPRs)
	avgCodeChurnHours := ext.CodeChurnHours / float64(ext.TotalPRs)

	avgTotalCost := ext.TotalCost / float64(ext.TotalPRs)
	avgTotalHours := ext.TotalHours / float64(ext.TotalPRs)

	// Show average PR breakdown with improved visual hierarchy
	fmt.Println("  ┌─────────────────────────────────────────────────────────────┐")
	headerText := fmt.Sprintf("Average PR (sampled over %d day period)", days)

	// Box has 61 dashes, inner content area is 60 chars (1 space + 60 chars content)
	const innerWidth = 60
	if len(headerText) > innerWidth {
		headerText = headerText[:innerWidth]
	}
	fmt.Printf("  │ %-60s│\n", headerText)
	fmt.Println("  └─────────────────────────────────────────────────────────────┘")
	fmt.Println()

	// Authors section
	fmt.Println("  Development Costs")
	fmt.Println("  ─────────────────")

	// Calculate kLOC for display
	avgNewLOC := float64(ext.TotalNewLines) / float64(ext.TotalPRs) / 1000.0
	avgModifiedLOC := float64(ext.TotalModifiedLines) / float64(ext.TotalPRs) / 1000.0

	// Format LOC with more precision for small values
	newLOCStr := fmt.Sprintf("%.1fk", avgNewLOC)
	if avgNewLOC < 1.0 && avgNewLOC >= 0.1 {
		newLOCStr = fmt.Sprintf("%.1fk", avgNewLOC)
	} else if avgNewLOC < 0.1 && avgNewLOC > 0 {
		newLOCStr = fmt.Sprintf("%.2fk", avgNewLOC)
	}
	modifiedLOCStr := fmt.Sprintf("%.1fk", avgModifiedLOC)
	if avgModifiedLOC < 1.0 && avgModifiedLOC >= 0.1 {
		modifiedLOCStr = fmt.Sprintf("%.1fk", avgModifiedLOC)
	} else if avgModifiedLOC < 0.1 && avgModifiedLOC > 0 {
		modifiedLOCStr = fmt.Sprintf("%.2fk", avgModifiedLOC)
	}

	fmt.Printf("    New Development           %12s    %s  (%s LOC)\n",
		formatWithCommas(avgAuthorNewCodeCost), formatTimeUnit(avgAuthorNewCodeHours), newLOCStr)
	fmt.Printf("    Adaptation                %12s    %s  (%s LOC)\n",
		formatWithCommas(avgAuthorAdaptationCost), formatTimeUnit(avgAuthorAdaptationHours), modifiedLOCStr)
	fmt.Printf("    GitHub Activity           %12s    %s\n",
		formatWithCommas(avgAuthorGitHubCost), formatTimeUnit(avgAuthorGitHubHours))
	fmt.Printf("    GitHub Context Switching  %12s    %s\n",
		formatWithCommas(avgAuthorGitHubContextCost), formatTimeUnit(avgAuthorGitHubContextHours))
	fmt.Println("                              ────────────")
	pct := (avgAuthorTotalCost / avgTotalCost) * 100
	fmt.Printf("    Subtotal                 $%12s    %s  (%.1f%%)\n",
		formatWithCommas(avgAuthorTotalCost), formatTimeUnit(avgAuthorTotalHours), pct)
	fmt.Println()

	// Participants section (if any participants)
	if ext.ParticipantTotalCost > 0 {
		fmt.Println("  Participant Costs")
		fmt.Println("  ─────────────────")
		if avgParticipantReviewCost > 0 {
			fmt.Printf("    Review Activity           %12s    %s\n",
				formatWithCommas(avgParticipantReviewCost), formatTimeUnit(avgParticipantReviewHours))
		}
		if avgParticipantGitHubCost > 0 {
			fmt.Printf("    GitHub Activity           %12s    %s\n",
				formatWithCommas(avgParticipantGitHubCost), formatTimeUnit(avgParticipantGitHubHours))
		}
		fmt.Printf("    Context Switching         %12s    %s\n",
			formatWithCommas(avgParticipantContextCost), formatTimeUnit(avgParticipantContextHours))
		fmt.Println("                              ────────────")
		pct := (avgParticipantTotalCost / avgTotalCost) * 100
		fmt.Printf("    Subtotal                 $%12s    %s  (%.1f%%)\n",
			formatWithCommas(avgParticipantTotalCost), formatTimeUnit(avgParticipantTotalHours), pct)
		fmt.Println()
	}

	// Merge Delay section
	fmt.Println("  Delay Costs")
	fmt.Println("  ───────────")
	avgOpenTime = formatTimeUnit(ext.AvgPRDurationHours)
	fmt.Printf("    Workstream blockage       %12s    %s  (avg open: %s)\n",
		formatWithCommas(avgDeliveryDelayCost), formatTimeUnit(avgDeliveryDelayHours), avgOpenTime)
	fmt.Printf("    Coordination              %12s    %s  (avg open: %s)\n",
		formatWithCommas(avgCoordinationCost), formatTimeUnit(avgCoordinationHours), avgOpenTime)
	avgMergeDelayCost := avgDeliveryDelayCost + avgCoordinationCost
	avgMergeDelayHours := avgDeliveryDelayHours + avgCoordinationHours
	fmt.Println("                              ────────────")
	pct = (avgMergeDelayCost / avgTotalCost) * 100
	fmt.Printf("    Subtotal                 $%12s    %s  (%.1f%%)\n",
		formatWithCommas(avgMergeDelayCost), formatTimeUnit(avgMergeDelayHours), pct)
	fmt.Println()

	// Future Costs section
	avgFutureReviewCost := ext.FutureReviewCost / float64(ext.TotalPRs)
	avgFutureMergeCost := ext.FutureMergeCost / float64(ext.TotalPRs)
	avgFutureContextCost := ext.FutureContextCost / float64(ext.TotalPRs)
	avgFutureReviewHours := ext.FutureReviewHours / float64(ext.TotalPRs)
	avgFutureMergeHours := ext.FutureMergeHours / float64(ext.TotalPRs)
	avgFutureContextHours := ext.FutureContextHours / float64(ext.TotalPRs)

	hasFutureCosts := ext.CodeChurnCost > 0.01 || ext.FutureReviewCost > 0.01 ||
		ext.FutureMergeCost > 0.01 || ext.FutureContextCost > 0.01

	if hasFutureCosts {
		fmt.Println("  Future Costs")
		fmt.Println("  ────────────")
		if ext.CodeChurnCost > 0.01 {
			fmt.Printf("    Code Churn                %12s    %s  (%d PRs)\n",
				formatWithCommas(avgCodeChurnCost), formatTimeUnit(avgCodeChurnHours), ext.CodeChurnPRCount)
		}
		if ext.FutureReviewCost > 0.01 {
			fmt.Printf("    %-26s%12s    %s  (%d PRs)\n",
				"Review",
				formatWithCommas(avgFutureReviewCost), formatTimeUnit(avgFutureReviewHours), ext.FutureReviewPRCount)
		}
		if ext.FutureMergeCost > 0.01 {
			fmt.Printf("    %-26s%12s    %s  (%d PRs)\n",
				"Merge",
				formatWithCommas(avgFutureMergeCost), formatTimeUnit(avgFutureMergeHours), ext.FutureMergePRCount)
		}
		if ext.FutureContextCost > 0.01 {
			fmt.Printf("    %-26s%12s    %s\n",
				"Context Switching",
				formatWithCommas(avgFutureContextCost), formatTimeUnit(avgFutureContextHours))
		}
		avgFutureCost := avgCodeChurnCost + avgFutureReviewCost + avgFutureMergeCost + avgFutureContextCost
		avgFutureHours := avgCodeChurnHours + avgFutureReviewHours + avgFutureMergeHours + avgFutureContextHours
		fmt.Println("                              ────────────")
		pct = (avgFutureCost / avgTotalCost) * 100
		fmt.Printf("    Subtotal                 $%12s    %s  (%.1f%%)\n",
			formatWithCommas(avgFutureCost), formatTimeUnit(avgFutureHours), pct)
		fmt.Println()
	}

	// Average Preventable Loss Total (before grand total)
	avgPreventableCost := avgCodeChurnCost + avgDeliveryDelayCost + avgCoordinationCost
	avgPreventableHours := avgCodeChurnHours + avgDeliveryDelayHours + avgCoordinationHours
	avgPreventablePct := (avgPreventableCost / avgTotalCost) * 100
	fmt.Printf("  Preventable Loss Total     $%12s    %s  (%.1f%%)\n",
		formatWithCommas(avgPreventableCost), formatTimeUnit(avgPreventableHours), avgPreventablePct)

	// Average total
	fmt.Println("  ═══════════════════════════════════════════════════════════════")
	fmt.Printf("  Average Total              $%12s    %s\n",
		formatWithCommas(avgTotalCost), formatTimeUnit(avgTotalHours))
	fmt.Println()
	fmt.Println()

	// Extrapolated total section with improved visual hierarchy
	fmt.Println("  ┌─────────────────────────────────────────────────────────────┐")
	headerText = fmt.Sprintf("Estimated costs within a %d day period (extrapolated)", days)

	// Box has 61 dashes, inner content area is 60 chars (1 space + 60 chars content)
	if len(headerText) > 60 {
		headerText = headerText[:60]
	}
	fmt.Printf("  │ %-60s│\n", headerText)
	fmt.Println("  └─────────────────────────────────────────────────────────────┘")
	fmt.Println()

	// Authors section (extrapolated)
	fmt.Println("  Development Costs")
	fmt.Println("  ─────────────────")

	// Calculate kLOC for display
	totalNewLOC := float64(ext.TotalNewLines) / 1000.0
	totalModifiedLOC := float64(ext.TotalModifiedLines) / 1000.0

	// Format LOC with appropriate precision based on magnitude
	totalNewLOCStr := fmt.Sprintf("%.0fk", totalNewLOC)
	if totalNewLOC < 10.0 {
		totalNewLOCStr = fmt.Sprintf("%.1fk", totalNewLOC)
	}
	totalModifiedLOCStr := fmt.Sprintf("%.0fk", totalModifiedLOC)
	if totalModifiedLOC < 10.0 {
		totalModifiedLOCStr = fmt.Sprintf("%.1fk", totalModifiedLOC)
	}

	fmt.Printf("    New Development           %12s    %s  (%s LOC)\n",
		formatWithCommas(ext.AuthorNewCodeCost), formatTimeUnit(ext.AuthorNewCodeHours), totalNewLOCStr)
	fmt.Printf("    Adaptation                %12s    %s  (%s LOC)\n",
		formatWithCommas(ext.AuthorAdaptationCost), formatTimeUnit(ext.AuthorAdaptationHours), totalModifiedLOCStr)
	fmt.Printf("    GitHub Activity           %12s    %s\n",
		formatWithCommas(ext.AuthorGitHubCost), formatTimeUnit(ext.AuthorGitHubHours))
	fmt.Printf("    GitHub Context Switching  %12s    %s\n",
		formatWithCommas(ext.AuthorGitHubContextCost), formatTimeUnit(ext.AuthorGitHubContextHours))
	fmt.Println("                              ────────────")
	pct = (ext.AuthorTotalCost / ext.TotalCost) * 100
	fmt.Printf("    Subtotal                 $%12s    %s  (%.1f%%)\n",
		formatWithCommas(ext.AuthorTotalCost), formatTimeUnit(ext.AuthorTotalHours), pct)
	fmt.Println()

	// Participants section (extrapolated, if any participants)
	if ext.ParticipantTotalCost > 0 {
		fmt.Println("  Participant Costs")
		fmt.Println("  ─────────────────")
		if ext.ParticipantReviewCost > 0 {
			fmt.Printf("    Review Activity           %12s    %s\n",
				formatWithCommas(ext.ParticipantReviewCost), formatTimeUnit(ext.ParticipantReviewHours))
		}
		if ext.ParticipantGitHubCost > 0 {
			fmt.Printf("    GitHub Activity           %12s    %s\n",
				formatWithCommas(ext.ParticipantGitHubCost), formatTimeUnit(ext.ParticipantGitHubHours))
		}
		fmt.Printf("    Context Switching         %12s    %s\n",
			formatWithCommas(ext.ParticipantContextCost), formatTimeUnit(ext.ParticipantContextHours))
		fmt.Println("                              ────────────")
		pct = (ext.ParticipantTotalCost / ext.TotalCost) * 100
		fmt.Printf("    Subtotal                 $%12s    %s  (%.1f%%)\n",
			formatWithCommas(ext.ParticipantTotalCost), formatTimeUnit(ext.ParticipantTotalHours), pct)
		fmt.Println()
	}

	// Merge Delay section (extrapolated)
	fmt.Println("  Delay Costs")
	fmt.Println("  ───────────")
	extAvgOpenTime := formatTimeUnit(ext.AvgPRDurationHours)
	fmt.Printf("    Workstream blockage       %12s    %s  (avg open: %s)\n",
		formatWithCommas(ext.DeliveryDelayCost), formatTimeUnit(ext.DeliveryDelayHours), extAvgOpenTime)
	fmt.Printf("    Coordination              %12s    %s  (avg open: %s)\n",
		formatWithCommas(ext.CoordinationCost), formatTimeUnit(ext.CoordinationHours), extAvgOpenTime)
	extMergeDelayCost := ext.DeliveryDelayCost + ext.CoordinationCost
	extMergeDelayHours := ext.DeliveryDelayHours + ext.CoordinationHours
	fmt.Println("                              ────────────")
	pct = (extMergeDelayCost / ext.TotalCost) * 100
	fmt.Printf("    Subtotal                 $%12s    %s  (%.1f%%)\n",
		formatWithCommas(extMergeDelayCost), formatTimeUnit(extMergeDelayHours), pct)
	fmt.Println()

	// Future Costs section (extrapolated)
	extHasFutureCosts := ext.CodeChurnCost > 0.01 || ext.FutureReviewCost > 0.01 ||
		ext.FutureMergeCost > 0.01 || ext.FutureContextCost > 0.01

	if extHasFutureCosts {
		fmt.Println("  Future Costs")
		fmt.Println("  ────────────")
		if ext.CodeChurnCost > 0.01 {
			totalKLOC := float64(ext.TotalNewLines+ext.TotalModifiedLines) / 1000.0
			churnLOCStr := fmt.Sprintf("%.0fk", totalKLOC)
			if totalKLOC < 10.0 {
				churnLOCStr = fmt.Sprintf("%.1fk", totalKLOC)
			}
			fmt.Printf("    Code Churn                %12s    %s  (%d PRs, ~%s LOC)\n",
				formatWithCommas(ext.CodeChurnCost), formatTimeUnit(ext.CodeChurnHours), ext.CodeChurnPRCount, churnLOCStr)
		}
		if ext.FutureReviewCost > 0.01 {
			fmt.Printf("    %-26s%12s    %s  (%d PRs)\n",
				"Review",
				formatWithCommas(ext.FutureReviewCost), formatTimeUnit(ext.FutureReviewHours), ext.FutureReviewPRCount)
		}
		if ext.FutureMergeCost > 0.01 {
			fmt.Printf("    %-26s%12s    %s  (%d PRs)\n",
				"Merge",
				formatWithCommas(ext.FutureMergeCost), formatTimeUnit(ext.FutureMergeHours), ext.FutureMergePRCount)
		}
		if ext.FutureContextCost > 0.01 {
			fmt.Printf("    %-26s%12s    %s\n",
				"Context Switching",
				formatWithCommas(ext.FutureContextCost), formatTimeUnit(ext.FutureContextHours))
		}
		extFutureCost := ext.CodeChurnCost + ext.FutureReviewCost + ext.FutureMergeCost + ext.FutureContextCost
		extFutureHours := ext.CodeChurnHours + ext.FutureReviewHours + ext.FutureMergeHours + ext.FutureContextHours
		fmt.Println("                              ────────────")
		pct = (extFutureCost / ext.TotalCost) * 100
		fmt.Printf("    Subtotal                 $%12s    %s  (%.1f%%)\n",
			formatWithCommas(extFutureCost), formatTimeUnit(extFutureHours), pct)
		fmt.Println()
	}

	// Preventable Loss Total (before grand total)
	preventableCost := ext.CodeChurnCost + ext.DeliveryDelayCost + ext.CoordinationCost
	preventableHours := ext.CodeChurnHours + ext.DeliveryDelayHours + ext.CoordinationHours
	preventablePct := (preventableCost / ext.TotalCost) * 100
	fmt.Printf("  Preventable Loss Total     $%12s    %s  (%.1f%%)\n",
		formatWithCommas(preventableCost), formatTimeUnit(preventableHours), preventablePct)

	// Extrapolated grand total
	fmt.Println("  ═══════════════════════════════════════════════════════════════")
	fmt.Printf("  Total                      $%12s    %s\n",
		formatWithCommas(ext.TotalCost), formatTimeUnit(ext.TotalHours))
	fmt.Println()

	// Print extrapolated efficiency score + annual waste
	printExtrapolatedEfficiency(ext, days, cfg)
}

// printExtrapolatedEfficiency prints the workflow efficiency + annual waste section for extrapolated totals.
func printExtrapolatedEfficiency(ext *cost.ExtrapolatedBreakdown, days int, cfg cost.Config) {
	// Calculate preventable waste: Code Churn + All Delay Costs
	preventableHours := ext.CodeChurnHours + ext.DeliveryDelayHours + ext.CoordinationHours
	preventableCost := ext.CodeChurnCost + ext.DeliveryDelayCost + ext.CoordinationCost

	// Calculate efficiency
	var efficiencyPct float64
	if ext.TotalHours > 0 {
		efficiencyPct = 100.0 * (ext.TotalHours - preventableHours) / ext.TotalHours
	} else {
		efficiencyPct = 100.0
	}

	grade, message := efficiencyGrade(efficiencyPct)

	// Calculate merge velocity grade based on average PR duration
	avgDurationDays := ext.AvgPRDurationHours / 24.0
	velocityGrade, velocityMessage := mergeVelocityGrade(avgDurationDays)

	// Calculate annual waste
	annualMultiplier := 365.0 / float64(days)
	annualWasteCost := preventableCost * annualMultiplier

	fmt.Println("  ┌─────────────────────────────────────────────────────────────┐")
	headerText := fmt.Sprintf("DEVELOPMENT EFFICIENCY: %s (%.1f%%) - %s", grade, efficiencyPct, message)

	// Box has 61 dashes, inner content area is 60 chars (1 space + 60 chars content)
	const innerWidth = 60
	if len(headerText) > innerWidth {
		headerText = headerText[:innerWidth]
	}
	fmt.Printf("  │ %-60s│\n", headerText)
	fmt.Println("  └─────────────────────────────────────────────────────────────┘")

	fmt.Println("  ┌─────────────────────────────────────────────────────────────┐")
	velocityHeader := fmt.Sprintf("MERGE VELOCITY: %s (%s) - %s", velocityGrade, formatTimeUnit(ext.AvgPRDurationHours), velocityMessage)
	if len(velocityHeader) > innerWidth {
		velocityHeader = velocityHeader[:innerWidth]
	}
	fmt.Printf("  │ %-60s│\n", velocityHeader)
	fmt.Println("  └─────────────────────────────────────────────────────────────┘")

	// Weekly waste per PR author
	if ext.WasteHoursPerAuthorPerWeek > 0 && ext.TotalAuthors > 0 {
		fmt.Printf("  Weekly waste per PR author:     $%12s    %s  (%d authors)\n",
			formatWithCommas(ext.WasteCostPerAuthorPerWeek),
			formatTimeUnit(ext.WasteHoursPerAuthorPerWeek),
			ext.TotalAuthors)
	}

	// Calculate headcount from annual waste
	annualCostPerHead := cfg.AnnualSalary * cfg.BenefitsMultiplier
	headcount := annualWasteCost / annualCostPerHead
	fmt.Printf("  If Sustained for 1 Year:        $%12s    %.1f headcount\n",
		formatWithCommas(annualWasteCost), headcount)
	fmt.Println()
}
