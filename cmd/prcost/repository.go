package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/codeGROOVE-dev/prcost/pkg/cost"
	"github.com/codeGROOVE-dev/prcost/pkg/github"
)

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

	// Sample PRs using time-bucket strategy (library function)
	samples := github.SamplePRs(prs, sampleSize)

	slog.Info("Sampled PRs for analysis",
		"sample_size", len(samples),
		"requested_samples", sampleSize)

	fmt.Printf("\nAnalyzing %d sampled PRs from %d total PRs modified in the last %d days...\n\n",
		len(samples), len(prs), days)

	// Collect breakdowns from each sample
	var breakdowns []cost.Breakdown

	for i, pr := range samples {
		prURL := fmt.Sprintf("https://github.com/%s/%s/pull/%d", owner, repo, pr.Number)
		slog.Info("Processing sample PR",
			"repo", fmt.Sprintf("%s/%s", owner, repo),
			"number", pr.Number,
			"progress", fmt.Sprintf("%d/%d", i+1, len(samples)))

		// Fetch full PR data using configured data source
		var prData cost.PRData
		var err error
		if dataSource == "turnserver" {
			// Use turnserver with updatedAt for effective caching
			prData, err = github.FetchPRDataViaTurnserver(ctx, prURL, token, pr.UpdatedAt)
		} else {
			// Use prx with updatedAt for effective caching
			prData, err = github.FetchPRData(ctx, prURL, token, pr.UpdatedAt)
		}
		if err != nil {
			slog.Warn("Failed to fetch PR data, skipping", "pr_number", pr.Number, "source", dataSource, "error", err)
			continue
		}

		// Calculate cost and accumulate
		breakdown := cost.Calculate(prData, cfg)
		breakdowns = append(breakdowns, breakdown)
	}

	if len(breakdowns) == 0 {
		return errors.New("no samples could be processed successfully")
	}

	// Extrapolate costs from samples using library function
	extrapolated := cost.ExtrapolateFromSamples(breakdowns, len(prs))

	// Display results in itemized format
	printExtrapolatedResults(fmt.Sprintf("%s/%s", owner, repo), days, &extrapolated)

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

	// Sample PRs using time-bucket strategy (library function)
	samples := github.SamplePRs(prs, sampleSize)

	slog.Info("Sampled PRs for analysis",
		"sample_size", len(samples),
		"requested_samples", sampleSize)

	fmt.Printf("\nAnalyzing %d sampled PRs from %d total PRs across %s (last %d days)...\n\n",
		len(samples), len(prs), org, days)

	// Collect breakdowns from each sample
	var breakdowns []cost.Breakdown

	for i, pr := range samples {
		prURL := fmt.Sprintf("https://github.com/%s/%s/pull/%d", pr.Owner, pr.Repo, pr.Number)
		slog.Info("Processing sample PR",
			"repo", fmt.Sprintf("%s/%s", pr.Owner, pr.Repo),
			"number", pr.Number,
			"progress", fmt.Sprintf("%d/%d", i+1, len(samples)))

		// Fetch full PR data using configured data source
		var prData cost.PRData
		var err error
		if dataSource == "turnserver" {
			// Use turnserver with updatedAt for effective caching
			prData, err = github.FetchPRDataViaTurnserver(ctx, prURL, token, pr.UpdatedAt)
		} else {
			// Use prx with updatedAt for effective caching
			prData, err = github.FetchPRData(ctx, prURL, token, pr.UpdatedAt)
		}
		if err != nil {
			slog.Warn("Failed to fetch PR data, skipping", "pr_number", pr.Number, "source", dataSource, "error", err)
			continue
		}

		// Calculate cost and accumulate
		breakdown := cost.Calculate(prData, cfg)
		breakdowns = append(breakdowns, breakdown)
	}

	if len(breakdowns) == 0 {
		return errors.New("no samples could be processed successfully")
	}

	// Extrapolate costs from samples using library function
	extrapolated := cost.ExtrapolateFromSamples(breakdowns, len(prs))

	// Display results in itemized format
	printExtrapolatedResults(fmt.Sprintf("%s (organization)", org), days, &extrapolated)

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
func printExtrapolatedResults(title string, days int, ext *cost.ExtrapolatedBreakdown) {
	fmt.Println()
	fmt.Printf("  %s\n", title)
	fmt.Printf("  Period: Last %d days  •  Total PRs: %d  •  Sampled: %d\n", days, ext.TotalPRs, ext.SuccessfulSamples)
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
	fmt.Printf("  │ Average PR (sampled over %d day period)%*s│\n", days, 51-len(strconv.Itoa(days)), "")
	fmt.Println("  └─────────────────────────────────────────────────────────────┘")
	fmt.Println()

	// Authors section
	fmt.Println("  Development Costs")
	fmt.Println("  ─────────────────")
	fmt.Printf("    New Development           %12s    %s\n",
		formatWithCommas(avgAuthorNewCodeCost), formatTimeUnit(avgAuthorNewCodeHours))
	fmt.Printf("    Adaptation                %12s    %s\n",
		formatWithCommas(avgAuthorAdaptationCost), formatTimeUnit(avgAuthorAdaptationHours))
	fmt.Printf("    GitHub Activity           %12s    %s\n",
		formatWithCommas(avgAuthorGitHubCost), formatTimeUnit(avgAuthorGitHubHours))
	fmt.Printf("    GitHub Context Switching  %12s    %s\n",
		formatWithCommas(avgAuthorGitHubContextCost), formatTimeUnit(avgAuthorGitHubContextHours))
	fmt.Println("                              ────────────")
	pct := (avgAuthorTotalCost / avgTotalCost) * 100
	fmt.Printf("    Subtotal                  %12s    %s  (%.1f%%)\n",
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
			fmt.Printf("    GitHub Events             %12s    %s\n",
				formatWithCommas(avgParticipantGitHubCost), formatTimeUnit(avgParticipantGitHubHours))
		}
		fmt.Printf("    Context Switching         %12s    %s\n",
			formatWithCommas(avgParticipantContextCost), formatTimeUnit(avgParticipantContextHours))
		fmt.Println("                              ────────────")
		pct := (avgParticipantTotalCost / avgTotalCost) * 100
		fmt.Printf("    Subtotal                  %12s    %s  (%.1f%%)\n",
			formatWithCommas(avgParticipantTotalCost), formatTimeUnit(avgParticipantTotalHours), pct)
		fmt.Println()
	}

	// Merge Delay section
	fmt.Println("  Delay Costs")
	fmt.Println("  ───────────")
	fmt.Printf("    Project                   %12s    %s\n",
		formatWithCommas(avgDeliveryDelayCost), formatTimeUnit(avgDeliveryDelayHours))
	fmt.Printf("    Coordination              %12s    %s\n",
		formatWithCommas(avgCoordinationCost), formatTimeUnit(avgCoordinationHours))
	avgMergeDelayCost := avgDeliveryDelayCost + avgCoordinationCost
	avgMergeDelayHours := avgDeliveryDelayHours + avgCoordinationHours
	fmt.Println("                              ────────────")
	pct = (avgMergeDelayCost / avgTotalCost) * 100
	fmt.Printf("    Subtotal                  %12s    %s  (%.1f%%)\n",
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
			fmt.Printf("    Code Churn                %12s    %s\n",
				formatWithCommas(avgCodeChurnCost), formatTimeUnit(avgCodeChurnHours))
		}
		if ext.FutureReviewCost > 0.01 {
			fmt.Printf("    %-26s%12s    %s\n",
				"Review",
				formatWithCommas(avgFutureReviewCost), formatTimeUnit(avgFutureReviewHours))
		}
		if ext.FutureMergeCost > 0.01 {
			fmt.Printf("    %-26s%12s    %s\n",
				"Merge",
				formatWithCommas(avgFutureMergeCost), formatTimeUnit(avgFutureMergeHours))
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
		fmt.Printf("    Subtotal                  %12s    %s  (%.1f%%)\n",
			formatWithCommas(avgFutureCost), formatTimeUnit(avgFutureHours), pct)
		fmt.Println()
	}

	// Average total
	fmt.Println("  ═══════════════════════════════════════════════════════════════")
	fmt.Printf("  Average Total               %12s    %s\n",
		formatWithCommas(avgTotalCost), formatTimeUnit(avgTotalHours))
	fmt.Println()
	fmt.Println()

	// Extrapolated total section with improved visual hierarchy
	fmt.Println("  ┌─────────────────────────────────────────────────────────────┐")
	fmt.Printf("  │ Estimated Total Changes (extrapolated across %d day period)%*s│\n", days, 35-len(strconv.Itoa(days)), "")
	fmt.Printf("  │ Total PRs: %d%*s│\n", ext.TotalPRs, 60-len(strconv.Itoa(ext.TotalPRs)), "")
	fmt.Println("  └─────────────────────────────────────────────────────────────┘")
	fmt.Println()

	// Authors section (extrapolated)
	fmt.Println("  Development Costs")
	fmt.Println("  ─────────────────")
	fmt.Printf("    New Development           %12s    %s\n",
		formatWithCommas(ext.AuthorNewCodeCost), formatTimeUnit(ext.AuthorNewCodeHours))
	fmt.Printf("    Adaptation                %12s    %s\n",
		formatWithCommas(ext.AuthorAdaptationCost), formatTimeUnit(ext.AuthorAdaptationHours))
	fmt.Printf("    GitHub Activity           %12s    %s\n",
		formatWithCommas(ext.AuthorGitHubCost), formatTimeUnit(ext.AuthorGitHubHours))
	fmt.Printf("    GitHub Context Switching  %12s    %s\n",
		formatWithCommas(ext.AuthorGitHubContextCost), formatTimeUnit(ext.AuthorGitHubContextHours))
	fmt.Println("                              ────────────")
	pct = (ext.AuthorTotalCost / ext.TotalCost) * 100
	fmt.Printf("    Subtotal                  %12s    %s  (%.1f%%)\n",
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
			fmt.Printf("    GitHub Events             %12s    %s\n",
				formatWithCommas(ext.ParticipantGitHubCost), formatTimeUnit(ext.ParticipantGitHubHours))
		}
		fmt.Printf("    Context Switching         %12s    %s\n",
			formatWithCommas(ext.ParticipantContextCost), formatTimeUnit(ext.ParticipantContextHours))
		fmt.Println("                              ────────────")
		pct = (ext.ParticipantTotalCost / ext.TotalCost) * 100
		fmt.Printf("    Subtotal                  %12s    %s  (%.1f%%)\n",
			formatWithCommas(ext.ParticipantTotalCost), formatTimeUnit(ext.ParticipantTotalHours), pct)
		fmt.Println()
	}

	// Merge Delay section (extrapolated)
	fmt.Println("  Delay Costs")
	fmt.Println("  ───────────")
	fmt.Printf("    Project                   %12s    %s\n",
		formatWithCommas(ext.DeliveryDelayCost), formatTimeUnit(ext.DeliveryDelayHours))
	fmt.Printf("    Coordination              %12s    %s\n",
		formatWithCommas(ext.CoordinationCost), formatTimeUnit(ext.CoordinationHours))
	extMergeDelayCost := ext.DeliveryDelayCost + ext.CoordinationCost
	extMergeDelayHours := ext.DeliveryDelayHours + ext.CoordinationHours
	fmt.Println("                              ────────────")
	pct = (extMergeDelayCost / ext.TotalCost) * 100
	fmt.Printf("    Subtotal                  %12s    %s  (%.1f%%)\n",
		formatWithCommas(extMergeDelayCost), formatTimeUnit(extMergeDelayHours), pct)
	fmt.Println()

	// Future Costs section (extrapolated)
	extHasFutureCosts := ext.CodeChurnCost > 0.01 || ext.FutureReviewCost > 0.01 ||
		ext.FutureMergeCost > 0.01 || ext.FutureContextCost > 0.01

	if extHasFutureCosts {
		fmt.Println("  Future Costs")
		fmt.Println("  ────────────")
		if ext.CodeChurnCost > 0.01 {
			fmt.Printf("    Code Churn                %12s    %s\n",
				formatWithCommas(ext.CodeChurnCost), formatTimeUnit(ext.CodeChurnHours))
		}
		if ext.FutureReviewCost > 0.01 {
			fmt.Printf("    %-26s%12s    %s\n",
				"Review",
				formatWithCommas(ext.FutureReviewCost), formatTimeUnit(ext.FutureReviewHours))
		}
		if ext.FutureMergeCost > 0.01 {
			fmt.Printf("    %-26s%12s    %s\n",
				"Merge",
				formatWithCommas(ext.FutureMergeCost), formatTimeUnit(ext.FutureMergeHours))
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
		fmt.Printf("    Subtotal                  %12s    %s  (%.1f%%)\n",
			formatWithCommas(extFutureCost), formatTimeUnit(extFutureHours), pct)
		fmt.Println()
	}

	// Extrapolated grand total
	fmt.Println("  ═══════════════════════════════════════════════════════════════")
	fmt.Printf("  Total                       %12s    %s\n",
		formatWithCommas(ext.TotalCost), formatTimeUnit(ext.TotalHours))
	fmt.Println()
}
