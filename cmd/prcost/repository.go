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

	// Validate time coverage (logs statistics, always uses requested period)
	actualDays, _ := github.CalculateActualTimeWindow(prs, days)

	// Count bot PRs before sampling
	botPRCount := countBotPRs(prs)
	humanPRCount := len(prs) - botPRCount

	// Sample PRs using time-bucket strategy (includes all PRs)
	samples := github.SamplePRs(prs, sampleSize)

	slog.Info("Sampled PRs for analysis",
		"total_prs", len(prs),
		"human_prs", humanPRCount,
		"bot_prs", botPRCount,
		"sample_size", len(samples),
		"requested_samples", sampleSize)

	if botPRCount > 0 {
		fmt.Printf("\nAnalyzing %d sampled PRs from %d total PRs (%d human, %d bot) modified in the last %d days...\n\n",
			len(samples), len(prs), humanPRCount, botPRCount, actualDays)
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

	// Query for actual count of open PRs (not extrapolated from samples)
	openPRCount, err := github.CountOpenPRsInRepo(ctx, owner, repo, token)
	if err != nil {
		slog.Warn("Failed to count open PRs, using 0", "error", err)
		openPRCount = 0
	}

	// Extrapolate costs from samples using library function
	extrapolated := cost.ExtrapolateFromSamples(breakdowns, len(prs), totalAuthors, openPRCount, actualDays, cfg)

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

	// Validate time coverage (logs statistics, always uses requested period)
	actualDays, _ := github.CalculateActualTimeWindow(prs, days)

	// Count bot PRs before sampling
	botPRCount := countBotPRs(prs)
	humanPRCount := len(prs) - botPRCount

	// Sample PRs using time-bucket strategy (includes all PRs)
	samples := github.SamplePRs(prs, sampleSize)

	slog.Info("Sampled PRs for analysis",
		"total_prs", len(prs),
		"human_prs", humanPRCount,
		"bot_prs", botPRCount,
		"sample_size", len(samples),
		"requested_samples", sampleSize)

	if botPRCount > 0 {
		fmt.Printf("\nAnalyzing %d sampled PRs from %d total PRs (%d human, %d bot) across %s (last %d days)...\n\n",
			len(samples), len(prs), humanPRCount, botPRCount, org, actualDays)
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

	// Count open PRs across all unique repos in the organization
	uniqueRepos := make(map[string]bool)
	for _, pr := range prs {
		repoKey := pr.Owner + "/" + pr.Repo
		uniqueRepos[repoKey] = true
	}

	totalOpenPRs := 0
	for repoKey := range uniqueRepos {
		parts := strings.SplitN(repoKey, "/", 2)
		if len(parts) != 2 {
			continue
		}
		owner, repo := parts[0], parts[1]
		openCount, err := github.CountOpenPRsInRepo(ctx, owner, repo, token)
		if err != nil {
			slog.Warn("Failed to count open PRs for repo", "repo", repoKey, "error", err)
			continue
		}
		totalOpenPRs += openCount
	}
	slog.Info("Counted total open PRs across organization", "open_prs", totalOpenPRs, "repos", len(uniqueRepos))

	// Extrapolate costs from samples using library function
	extrapolated := cost.ExtrapolateFromSamples(breakdowns, len(prs), totalAuthors, totalOpenPRs, actualDays, cfg)

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

	// Show human/bot breakdown if there are bot PRs
	if ext.BotPRs > 0 {
		avgHumanOpenTime := formatTimeUnit(ext.AvgHumanPRDurationHours)
		avgBotOpenTime := formatTimeUnit(ext.AvgBotPRDurationHours)
		fmt.Printf("  Period: Last %d days  •  Total PRs: %d (%d human, %d bot)  •  Authors: %d  •  Sampled: %d\n",
			days, ext.TotalPRs, ext.HumanPRs, ext.BotPRs, ext.TotalAuthors, ext.SuccessfulSamples)
		fmt.Printf("  Avg Open Time: %s (human: %s, bot: %s)\n", avgOpenTime, avgHumanOpenTime, avgBotOpenTime)
	} else {
		fmt.Printf("  Period: Last %d days  •  Total PRs: %d  •  Authors: %d  •  Sampled: %d  •  Avg Open Time: %s\n",
			days, ext.TotalPRs, ext.TotalAuthors, ext.SuccessfulSamples, avgOpenTime)
	}
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
	avgCodeChurnCost := ext.CodeChurnCost / float64(ext.TotalPRs)
	avgAutomatedUpdatesCost := ext.AutomatedUpdatesCost / float64(ext.TotalPRs)
	avgPRTrackingCost := ext.PRTrackingCost / float64(ext.TotalPRs)
	avgDeliveryDelayHours := ext.DeliveryDelayHours / float64(ext.TotalPRs)
	avgCodeChurnHours := ext.CodeChurnHours / float64(ext.TotalPRs)
	avgAutomatedUpdatesHours := ext.AutomatedUpdatesHours / float64(ext.TotalPRs)
	avgPRTrackingHours := ext.PRTrackingHours / float64(ext.TotalPRs)

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
	// Calculate total LOC for header
	avgNewLOC := float64(ext.TotalNewLines) / float64(ext.TotalPRs) / 1000.0
	avgModifiedLOC := float64(ext.TotalModifiedLines) / float64(ext.TotalPRs) / 1000.0
	avgTotalLOC := avgNewLOC + avgModifiedLOC
	totalLOCStr := formatLOC(avgTotalLOC)
	newLOCStr := formatLOC(avgNewLOC)
	modifiedLOCStr := formatLOC(avgModifiedLOC)

	fmt.Printf("  Development Costs (%d PRs, %s total LOC)\n", ext.HumanPRs, totalLOCStr)
	fmt.Println("  ────────────────────────────────────────")

	fmt.Printf("    New Development            $%10s    %s  (%s LOC)\n",
		formatWithCommas(avgAuthorNewCodeCost), formatTimeUnit(avgAuthorNewCodeHours), newLOCStr)
	fmt.Printf("    Adaptation                 $%10s    %s  (%s LOC)\n",
		formatWithCommas(avgAuthorAdaptationCost), formatTimeUnit(avgAuthorAdaptationHours), modifiedLOCStr)
	fmt.Printf("    GitHub Activity            $%10s    %s\n",
		formatWithCommas(avgAuthorGitHubCost), formatTimeUnit(avgAuthorGitHubHours))
	fmt.Printf("    Context Switching          $%10s    %s\n",
		formatWithCommas(avgAuthorGitHubContextCost), formatTimeUnit(avgAuthorGitHubContextHours))

	// Show bot PR LOC even though cost is $0
	if ext.BotPRs > 0 {
		avgBotTotalLOC := float64(ext.BotNewLines+ext.BotModifiedLines) / float64(ext.TotalPRs) / 1000.0
		botLOCStr := formatLOC(avgBotTotalLOC)
		fmt.Printf("    Automated Updates              —         %s  (%d PRs, %s LOC)\n",
			formatTimeUnit(0.0), ext.BotPRs, botLOCStr)
	}
	fmt.Println("                                ──────────")
	pct := (avgAuthorTotalCost / avgTotalCost) * 100
	fmt.Printf("    Subtotal                   $%10s    %s  (%.1f%%)\n",
		formatWithCommas(avgAuthorTotalCost), formatTimeUnit(avgAuthorTotalHours), pct)
	fmt.Println()

	// Participants section (if any participants)
	if ext.ParticipantTotalCost > 0 {
		fmt.Println("  Participant Costs")
		fmt.Println("  ─────────────────")
		if avgParticipantReviewCost > 0 {
			fmt.Printf("    Review Activity            $%10s    %s\n",
				formatWithCommas(avgParticipantReviewCost), formatTimeUnit(avgParticipantReviewHours))
		}
		if avgParticipantGitHubCost > 0 {
			fmt.Printf("    GitHub Activity            $%10s    %s\n",
				formatWithCommas(avgParticipantGitHubCost), formatTimeUnit(avgParticipantGitHubHours))
		}
		fmt.Printf("    Context Switching          $%10s    %s\n",
			formatWithCommas(avgParticipantContextCost), formatTimeUnit(avgParticipantContextHours))
		fmt.Println("                                ──────────")
		participantPct := (avgParticipantTotalCost / avgTotalCost) * 100
		fmt.Printf("    Subtotal                   $%10s    %s  (%.1f%%)\n",
			formatWithCommas(avgParticipantTotalCost), formatTimeUnit(avgParticipantTotalHours), participantPct)
		fmt.Println()
	}

	// Merge Delay section
	avgHumanOpenTime := formatTimeUnit(ext.AvgHumanPRDurationHours)
	avgBotOpenTime := formatTimeUnit(ext.AvgBotPRDurationHours)
	delayCostsHeader := fmt.Sprintf("  Delay Costs (human PRs avg %s open", avgHumanOpenTime)
	if ext.BotPRs > 0 {
		delayCostsHeader += fmt.Sprintf(", bot PRs avg %s", avgBotOpenTime)
	}
	delayCostsHeader += ")"
	fmt.Println(delayCostsHeader)
	fmt.Println("  " + strings.Repeat("─", len(delayCostsHeader)-2))
	if avgDeliveryDelayCost > 0 {
		fmt.Printf("    Workstream blockage        $%10s    %s  (%d PRs)\n",
			formatWithCommas(avgDeliveryDelayCost), formatTimeUnit(avgDeliveryDelayHours), ext.HumanPRs)
	}
	if avgAutomatedUpdatesCost > 0 {
		fmt.Printf("    Automated Updates          $%10s    %s  (%d PRs)\n",
			formatWithCommas(avgAutomatedUpdatesCost), formatTimeUnit(avgAutomatedUpdatesHours), ext.BotPRs)
	}
	if avgPRTrackingCost > 0 {
		fmt.Printf("    PR Tracking           $%10s    %s  (%d open PRs)\n",
			formatWithCommas(avgPRTrackingCost), formatTimeUnit(avgPRTrackingHours), ext.OpenPRs)
	}
	avgMergeDelayCost := avgDeliveryDelayCost + avgAutomatedUpdatesCost + avgPRTrackingCost
	avgMergeDelayHours := avgDeliveryDelayHours + avgAutomatedUpdatesHours + avgPRTrackingHours
	fmt.Println("                                ──────────")
	pct = (avgMergeDelayCost / avgTotalCost) * 100
	fmt.Printf("    Subtotal                   $%10s    %s  (%.1f%%)\n",
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
			fmt.Printf("    Code Churn                 $%10s    %s  (%d PRs)\n",
				formatWithCommas(avgCodeChurnCost), formatTimeUnit(avgCodeChurnHours), ext.CodeChurnPRCount)
		}
		if ext.FutureReviewCost > 0.01 {
			fmt.Printf("    Review                     $%10s    %s  (%d PRs)\n",
				formatWithCommas(avgFutureReviewCost), formatTimeUnit(avgFutureReviewHours), ext.FutureReviewPRCount)
		}
		if ext.FutureMergeCost > 0.01 {
			fmt.Printf("    Merge                      $%10s    %s  (%d PRs)\n",
				formatWithCommas(avgFutureMergeCost), formatTimeUnit(avgFutureMergeHours), ext.FutureMergePRCount)
		}
		if ext.FutureContextCost > 0.01 {
			fmt.Printf("    Context Switching          $%10s    %s\n",
				formatWithCommas(avgFutureContextCost), formatTimeUnit(avgFutureContextHours))
		}
		avgFutureCost := avgCodeChurnCost + avgFutureReviewCost + avgFutureMergeCost + avgFutureContextCost
		avgFutureHours := avgCodeChurnHours + avgFutureReviewHours + avgFutureMergeHours + avgFutureContextHours
		fmt.Println("                                ──────────")
		pct = (avgFutureCost / avgTotalCost) * 100
		fmt.Printf("    Subtotal                   $%10s    %s  (%.1f%%)\n",
			formatWithCommas(avgFutureCost), formatTimeUnit(avgFutureHours), pct)
		fmt.Println()
	}

	// Average Preventable Loss Total (before grand total)
	avgPreventableCost := avgCodeChurnCost + avgDeliveryDelayCost + avgAutomatedUpdatesCost + avgPRTrackingCost
	avgPreventableHours := avgCodeChurnHours + avgDeliveryDelayHours + avgAutomatedUpdatesHours + avgPRTrackingHours
	avgPreventablePct := (avgPreventableCost / avgTotalCost) * 100
	fmt.Printf("  Preventable Loss Total       $%10s    %s  (%.1f%%)\n",
		formatWithCommas(avgPreventableCost), formatTimeUnit(avgPreventableHours), avgPreventablePct)

	// Average total
	fmt.Println("  ════════════════════════════════════════════════════")
	fmt.Printf("  Average Total                $%10s    %s\n",
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
	// Calculate kLOC for display
	totalNewLOC := float64(ext.TotalNewLines) / 1000.0
	totalModifiedLOC := float64(ext.TotalModifiedLines) / 1000.0
	totalNewLOCStr := formatLOC(totalNewLOC)
	totalModifiedLOCStr := formatLOC(totalModifiedLOC)

	// Calculate total LOC for header
	totalTotalLOC := totalNewLOC + totalModifiedLOC
	totalTotalLOCStr := formatLOC(totalTotalLOC)

	fmt.Printf("  Development Costs (%d PRs, %s total LOC)\n", ext.HumanPRs, totalTotalLOCStr)
	fmt.Println("  ────────────────────────────────────────")

	fmt.Printf("    New Development            $%10s    %s  (%s LOC)\n",
		formatWithCommas(ext.AuthorNewCodeCost), formatTimeUnit(ext.AuthorNewCodeHours), totalNewLOCStr)
	fmt.Printf("    Adaptation                 $%10s    %s  (%s LOC)\n",
		formatWithCommas(ext.AuthorAdaptationCost), formatTimeUnit(ext.AuthorAdaptationHours), totalModifiedLOCStr)
	fmt.Printf("    GitHub Activity            $%10s    %s\n",
		formatWithCommas(ext.AuthorGitHubCost), formatTimeUnit(ext.AuthorGitHubHours))
	fmt.Printf("    Context Switching          $%10s    %s\n",
		formatWithCommas(ext.AuthorGitHubContextCost), formatTimeUnit(ext.AuthorGitHubContextHours))

	// Show bot PR LOC even though cost is $0
	if ext.BotPRs > 0 {
		totalBotLOC := float64(ext.BotNewLines+ext.BotModifiedLines) / 1000.0
		botTotalLOCStr := formatLOC(totalBotLOC)
		fmt.Printf("    Automated Updates              —         %s  (%d PRs, %s LOC)\n",
			formatTimeUnit(0.0), ext.BotPRs, botTotalLOCStr)
	}
	fmt.Println("                                ──────────")
	pct = (ext.AuthorTotalCost / ext.TotalCost) * 100
	fmt.Printf("    Subtotal                   $%10s    %s  (%.1f%%)\n",
		formatWithCommas(ext.AuthorTotalCost), formatTimeUnit(ext.AuthorTotalHours), pct)
	fmt.Println()

	// Participants section (extrapolated, if any participants)
	if ext.ParticipantTotalCost > 0 {
		fmt.Println("  Participant Costs")
		fmt.Println("  ─────────────────")
		if ext.ParticipantReviewCost > 0 {
			fmt.Printf("    Review Activity            $%10s    %s\n",
				formatWithCommas(ext.ParticipantReviewCost), formatTimeUnit(ext.ParticipantReviewHours))
		}
		if ext.ParticipantGitHubCost > 0 {
			fmt.Printf("    GitHub Activity            $%10s    %s\n",
				formatWithCommas(ext.ParticipantGitHubCost), formatTimeUnit(ext.ParticipantGitHubHours))
		}
		fmt.Printf("    Context Switching          $%10s    %s\n",
			formatWithCommas(ext.ParticipantContextCost), formatTimeUnit(ext.ParticipantContextHours))
		fmt.Println("                                ──────────")
		pct = (ext.ParticipantTotalCost / ext.TotalCost) * 100
		fmt.Printf("    Subtotal                   $%10s    %s  (%.1f%%)\n",
			formatWithCommas(ext.ParticipantTotalCost), formatTimeUnit(ext.ParticipantTotalHours), pct)
		fmt.Println()
	}

	// Merge Delay section (extrapolated)
	extAvgHumanOpenTime := formatTimeUnit(ext.AvgHumanPRDurationHours)
	extAvgBotOpenTime := formatTimeUnit(ext.AvgBotPRDurationHours)
	extDelayCostsHeader := fmt.Sprintf("  Delay Costs (human PRs avg %s open", extAvgHumanOpenTime)
	if ext.BotPRs > 0 {
		extDelayCostsHeader += fmt.Sprintf(", bot PRs avg %s", extAvgBotOpenTime)
	}
	extDelayCostsHeader += ")"
	fmt.Println(extDelayCostsHeader)
	fmt.Println("  " + strings.Repeat("─", len(extDelayCostsHeader)-2))

	if ext.DeliveryDelayCost > 0 {
		fmt.Printf("    Workstream blockage        $%10s    %s  (%d PRs)\n",
			formatWithCommas(ext.DeliveryDelayCost), formatTimeUnit(ext.DeliveryDelayHours), ext.HumanPRs)
	}
	if ext.AutomatedUpdatesCost > 0 {
		fmt.Printf("    Automated Updates          $%10s    %s  (%d PRs)\n",
			formatWithCommas(ext.AutomatedUpdatesCost), formatTimeUnit(ext.AutomatedUpdatesHours), ext.BotPRs)
	}
	if ext.PRTrackingCost > 0 {
		fmt.Printf("    PR Tracking           $%10s    %s  (%d open PRs)\n",
			formatWithCommas(ext.PRTrackingCost), formatTimeUnit(ext.PRTrackingHours), ext.OpenPRs)
	}
	extMergeDelayCost := ext.DeliveryDelayCost + ext.CodeChurnCost + ext.AutomatedUpdatesCost + ext.PRTrackingCost
	extMergeDelayHours := ext.DeliveryDelayHours + ext.CodeChurnHours + ext.AutomatedUpdatesHours + ext.PRTrackingHours
	fmt.Println("                                ──────────")
	pct = (extMergeDelayCost / ext.TotalCost) * 100
	fmt.Printf("    Subtotal                   $%10s    %s  (%.1f%%)\n",
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
			churnLOCStr := formatLOC(totalKLOC)
			fmt.Printf("    Code Churn                 $%10s    %s  (%d PRs, ~%s LOC)\n",
				formatWithCommas(ext.CodeChurnCost), formatTimeUnit(ext.CodeChurnHours), ext.CodeChurnPRCount, churnLOCStr)
		}
		if ext.FutureReviewCost > 0.01 {
			fmt.Printf("    Review                     $%10s    %s  (%d PRs)\n",
				formatWithCommas(ext.FutureReviewCost), formatTimeUnit(ext.FutureReviewHours), ext.FutureReviewPRCount)
		}
		if ext.FutureMergeCost > 0.01 {
			fmt.Printf("    Merge                      $%10s    %s  (%d PRs)\n",
				formatWithCommas(ext.FutureMergeCost), formatTimeUnit(ext.FutureMergeHours), ext.FutureMergePRCount)
		}
		if ext.FutureContextCost > 0.01 {
			fmt.Printf("    Context Switching          $%10s    %s\n",
				formatWithCommas(ext.FutureContextCost), formatTimeUnit(ext.FutureContextHours))
		}
		extFutureCost := ext.CodeChurnCost + ext.FutureReviewCost + ext.FutureMergeCost + ext.FutureContextCost
		extFutureHours := ext.CodeChurnHours + ext.FutureReviewHours + ext.FutureMergeHours + ext.FutureContextHours
		fmt.Println("                                ──────────")
		pct = (extFutureCost / ext.TotalCost) * 100
		fmt.Printf("    Subtotal                   $%10s    %s  (%.1f%%)\n",
			formatWithCommas(extFutureCost), formatTimeUnit(extFutureHours), pct)
		fmt.Println()
	}

	// Preventable Loss Total (before grand total)
	preventableCost := ext.CodeChurnCost + ext.DeliveryDelayCost + ext.AutomatedUpdatesCost + ext.PRTrackingCost
	preventableHours := ext.CodeChurnHours + ext.DeliveryDelayHours + ext.AutomatedUpdatesHours + ext.PRTrackingHours
	preventablePct := (preventableCost / ext.TotalCost) * 100
	fmt.Printf("  Preventable Loss Total       $%10s    %s  (%.1f%%)\n",
		formatWithCommas(preventableCost), formatTimeUnit(preventableHours), preventablePct)

	// Extrapolated grand total
	fmt.Println("  ════════════════════════════════════════════════════")
	fmt.Printf("  Total                        $%10s    %s\n",
		formatWithCommas(ext.TotalCost), formatTimeUnit(ext.TotalHours))
	fmt.Println()

	// Print extrapolated efficiency score + annual waste
	printExtrapolatedEfficiency(ext, days, cfg)
}

// printExtrapolatedEfficiency prints the workflow efficiency + annual waste section for extrapolated totals.
func printExtrapolatedEfficiency(ext *cost.ExtrapolatedBreakdown, days int, cfg cost.Config) {
	// Calculate preventable waste: Code Churn + All Delay Costs + Automated Updates + PR Tracking
	preventableHours := ext.CodeChurnHours + ext.DeliveryDelayHours + ext.AutomatedUpdatesHours + ext.PRTrackingHours
	preventableCost := ext.CodeChurnCost + ext.DeliveryDelayCost + ext.AutomatedUpdatesCost + ext.PRTrackingCost

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
