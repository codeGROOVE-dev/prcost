// Package main implements a CLI tool to calculate the real-world cost of GitHub PRs.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/codeGROOVE-dev/prcost/pkg/cost"
	"github.com/codeGROOVE-dev/prcost/pkg/github"
)

func main() {
	// Define command-line flags
	salary := flag.Float64("salary", 249000, "Annual salary for cost calculation")
	benefits := flag.Float64("benefits", 1.3, "Benefits multiplier (1.3 = 30% benefits)")
	eventMinutes := flag.Float64("event-minutes", 10, "Minutes per GitHub event (commits, comments, etc.)")
	format := flag.String("format", "human", "Output format: human or json")
	verbose := flag.Bool("verbose", false, "Show verbose logging output")
	dataSource := flag.String("data-source", "prx", "Data source for PR data: prx (direct GitHub API) or turnserver")

	// Org/Repo sampling flags
	org := flag.String("org", "", "GitHub organization to analyze (optionally with --repo for single repo)")
	repo := flag.String("repo", "", "GitHub repository to analyze (requires --org)")
	samples := flag.Int("samples", 25, "Number of PRs to sample for extrapolation (25=fast/±20%, 50=slower/±14%)")
	days := flag.Int("days", 60, "Number of days to look back for PR modifications")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [options] <PR_URL>\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "       %s --org <org> [--repo <repo>] [options]\n\n", os.Args[0])
		fmt.Fprint(os.Stderr, "Calculate the real-world cost of GitHub pull requests.\n\n")
		fmt.Fprint(os.Stderr, "Modes:\n")
		fmt.Fprint(os.Stderr, "  Single PR:   Provide a PR URL as argument\n")
		fmt.Fprint(os.Stderr, "  Single Repo: Use --org and --repo to analyze one repository\n")
		fmt.Fprint(os.Stderr, "  Org-wide:    Use --org to analyze entire organization\n\n")
		fmt.Fprint(os.Stderr, "Options:\n")
		flag.PrintDefaults()
		fmt.Fprint(os.Stderr, "\nExamples:\n")
		fmt.Fprint(os.Stderr, "  Single PR:\n")
		fmt.Fprintf(os.Stderr, "    %s https://github.com/owner/repo/pull/123\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "    %s --salary 300000 https://github.com/owner/repo/pull/123\n\n", os.Args[0])
		fmt.Fprint(os.Stderr, "  Repository analysis:\n")
		fmt.Fprintf(os.Stderr, "    %s --org kubernetes --repo kubernetes\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "    %s --org myorg --repo myrepo --samples 50 --days 30\n\n", os.Args[0])
		fmt.Fprint(os.Stderr, "  Organization-wide analysis:\n")
		fmt.Fprintf(os.Stderr, "    %s --org chainguard-dev\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "    %s --org myorg --samples 50 --days 60\n", os.Args[0])
	}

	flag.Parse()

	// Setup structured logging to stderr (stdout is for results)
	// Only show errors by default, show info/debug with --verbose
	logLevel := slog.LevelError
	if *verbose {
		logLevel = slog.LevelInfo
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: logLevel,
	}))
	slog.SetDefault(logger)

	// Determine mode: single PR or org/repo sampling
	orgMode := *org != ""
	singlePRMode := flag.NArg() == 1

	// Validate mode selection
	// First check if --repo is specified without --org
	if *repo != "" && *org == "" {
		fmt.Fprint(os.Stderr, "Error: --repo requires --org to be specified\n\n")
		flag.Usage()
		os.Exit(1)
	}

	if orgMode && singlePRMode {
		fmt.Fprint(os.Stderr, "Error: Cannot use both --org and PR URL. Choose one mode.\n\n")
		flag.Usage()
		os.Exit(1)
	}

	if !orgMode && !singlePRMode {
		flag.Usage()
		os.Exit(1)
	}

	// Create cost configuration from flags
	cfg := cost.DefaultConfig()
	cfg.AnnualSalary = *salary
	cfg.BenefitsMultiplier = *benefits
	cfg.EventDuration = time.Duration(*eventMinutes) * time.Minute

	slog.Debug("Configuration",
		"salary", cfg.AnnualSalary,
		"benefits_multiplier", cfg.BenefitsMultiplier,
		"event_minutes", *eventMinutes,
		"delivery_delay_factor", cfg.DeliveryDelayFactor,
		"coordination_factor", cfg.CoordinationFactor)

	// Retrieve GitHub token from gh CLI
	ctx := context.Background()
	slog.Info("Retrieving GitHub authentication token")
	token, err := authToken(ctx)
	if err != nil {
		slog.Error("Failed to get GitHub token", "error", err)
		log.Fatalf("Failed to get GitHub token: %v\nPlease ensure 'gh' is installed and authenticated (run 'gh auth login')", err)
	}
	slog.Debug("Successfully retrieved GitHub token")

	// Execute based on mode
	if orgMode {
		// Org/Repo sampling mode
		if *repo != "" {
			// Single repository mode
			slog.Info("Starting repository analysis",
				"org", *org,
				"repo", *repo,
				"samples", *samples,
				"days", *days)

			err := analyzeRepository(ctx, *org, *repo, *samples, *days, cfg, token, *dataSource)
			if err != nil {
				log.Fatalf("Repository analysis failed: %v", err)
			}
		} else {
			// Organization-wide mode
			slog.Info("Starting organization-wide analysis",
				"org", *org,
				"samples", *samples,
				"days", *days)

			err := analyzeOrganization(ctx, *org, *samples, *days, cfg, token, *dataSource)
			if err != nil {
				log.Fatalf("Organization analysis failed: %v", err)
			}
		}
	} else {
		// Single PR mode
		prURL := flag.Arg(0)

		// Validate PR URL format
		if !strings.HasPrefix(prURL, "https://github.com/") || !strings.Contains(prURL, "/pull/") {
			log.Fatal("Invalid PR URL. Expected format: https://github.com/owner/repo/pull/123")
		}

		slog.Info("Starting PR cost analysis", "pr_url", prURL, "format", *format)

		// Fetch PR data using configured data source
		slog.Info("Fetching PR data", "source", *dataSource)
		var prData cost.PRData
		var err error
		if *dataSource == "turnserver" {
			// Use turnserver - pass time.Now() since we don't have updatedAt for single PR requests
			prData, err = github.FetchPRDataViaTurnserver(ctx, prURL, token, time.Now())
		} else {
			// Use prx - pass time.Now() since we don't have updatedAt for single PR requests
			prData, err = github.FetchPRData(ctx, prURL, token, time.Now())
		}
		if err != nil {
			slog.Error("Failed to fetch PR data", "source", *dataSource, "error", err)
			log.Fatalf("Failed to fetch PR data: %v", err)
		}
		slog.Info("Successfully fetched PR data",
			"lines_added", prData.LinesAdded,
			"author", prData.Author,
			"events", len(prData.Events))

		// Calculate costs
		slog.Info("Calculating PR costs")
		breakdown := cost.Calculate(prData, cfg)
		slog.Info("Cost calculation complete", "total_cost", breakdown.TotalCost)

		// Output in requested format
		switch *format {
		case "human":
			printHumanReadable(&breakdown, prURL)
		case "json":
			encoder := json.NewEncoder(os.Stdout)
			encoder.SetIndent("", "  ")
			if err := encoder.Encode(&breakdown); err != nil {
				log.Fatalf("Failed to output results: %v", err)
			}
		default:
			log.Fatalf("Unknown format: %s (must be human or json)", *format)
		}
	}
}

// authToken retrieves a GitHub token using the gh CLI.
func authToken(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "gh", "auth", "token")
	output, err := cmd.Output()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", errors.New("timeout getting auth token")
		}
		return "", fmt.Errorf("failed to get auth token (is 'gh' installed and authenticated?): %w", err)
	}

	token := strings.TrimSpace(string(output))
	return token, nil
}

// printHumanReadable outputs a detailed itemized bill in human-readable format.
func printHumanReadable(breakdown *cost.Breakdown, prURL string) {
	// Helper to format currency with commas
	formatCurrency := func(amount float64) string {
		return fmt.Sprintf("$%s", formatWithCommas(amount))
	}

	// Header with PR info
	fmt.Println()
	fmt.Printf("  %s\n", prURL)

	// Show author and duration
	authorLabel := breakdown.PRAuthor
	if breakdown.AuthorBot {
		authorLabel += " (bot)"
	}
	fmt.Printf("  Author: %s  •  Open: %s\n", authorLabel, formatTimeUnit(breakdown.PRDuration))
	fmt.Printf("  Rate: %s/hr  •  Benefits multiplier: %.1fx\n",
		formatCurrency(breakdown.HourlyRate),
		breakdown.BenefitsMultiplier)
	fmt.Println()

	// Author Costs (skip entire section if no costs)
	if breakdown.Author.TotalCost > 0 {
		fmt.Println("  Development Costs")
		fmt.Println("  ─────────────────")
		// Show development and adaptation separately (only if there are actual lines of code)
		if breakdown.Author.NewLines > 0 {
			fmt.Printf("    New Development           %12s    %d LOC • %s\n",
				formatCurrency(breakdown.Author.NewCodeCost), breakdown.Author.NewLines, formatTimeUnit(breakdown.Author.NewCodeHours))
		}
		if breakdown.Author.ModifiedLines > 0 {
			fmt.Printf("    Adaptation                %12s    %d LOC • %s\n",
				formatCurrency(breakdown.Author.AdaptationCost), breakdown.Author.ModifiedLines, formatTimeUnit(breakdown.Author.AdaptationHours))
		}
		if breakdown.Author.GitHubHours > 0 {
			fmt.Printf("    GitHub Activity           %12s    %d sessions • %s\n",
				formatCurrency(breakdown.Author.GitHubCost), breakdown.Author.Sessions, formatTimeUnit(breakdown.Author.GitHubHours))
		}
		if breakdown.Author.GitHubContextHours > 0 {
			fmt.Printf("    GitHub Context Switching  %12s    %s\n",
				formatCurrency(breakdown.Author.GitHubContextCost), formatTimeUnit(breakdown.Author.GitHubContextHours))
		}
		fmt.Println("                              ────────────")
		pct := (breakdown.Author.TotalCost / breakdown.TotalCost) * 100
		fmt.Printf("    Subtotal                  %12s    %s  (%.1f%%)\n",
			formatCurrency(breakdown.Author.TotalCost), formatTimeUnit(breakdown.Author.TotalHours), pct)
		fmt.Println()
	}

	// Participant Costs
	if len(breakdown.Participants) > 0 {
		// Sum all participant costs
		var totalParticipantCost float64
		var totalParticipantHours float64
		for _, p := range breakdown.Participants {
			totalParticipantCost += p.TotalCost
			totalParticipantHours += p.TotalHours
		}

		fmt.Println("  Participant Costs")
		fmt.Println("  ─────────────────")
		for _, p := range breakdown.Participants {
			fmt.Printf("    %s\n", p.Actor)
			// Only show review activity if they reviewed (LOC-based)
			if p.ReviewHours > 0 {
				fmt.Printf("      Review Activity         %12s    %s\n",
					formatCurrency(p.ReviewCost), formatTimeUnit(p.ReviewHours))
			}
			// Only show other events if they had non-review events
			if p.GitHubHours > 0 {
				fmt.Printf("      GitHub Activity         %12s    %d sessions • %s\n",
					formatCurrency(p.GitHubCost), p.Sessions, formatTimeUnit(p.GitHubHours))
			}
			// Always show context switching if there were sessions
			if p.Sessions > 0 {
				fmt.Printf("      Context Switching       %12s    %s\n",
					formatCurrency(p.GitHubContextCost), formatTimeUnit(p.GitHubContextHours))
			}
		}
		fmt.Println("                              ────────────")
		pct := (totalParticipantCost / breakdown.TotalCost) * 100
		fmt.Printf("    Subtotal                  %12s    %s  (%.1f%%)\n",
			formatCurrency(totalParticipantCost), formatTimeUnit(totalParticipantHours), pct)
		fmt.Println()
	}

	// Delay and Future Costs - only show if there are any delay costs
	if breakdown.DelayCost > 0 {
		printDelayCosts(breakdown, formatCurrency)
	}

	// Grand Total
	totalHours := breakdown.Author.TotalHours + breakdown.DelayCostDetail.TotalDelayHours
	for _, p := range breakdown.Participants {
		totalHours += p.TotalHours
	}
	fmt.Println("  ═══════════════════════════════════════════════════════════════")
	fmt.Printf("  Total                       %12s    %s\n",
		formatCurrency(breakdown.TotalCost), formatTimeUnit(totalHours))
	fmt.Println()

	// Print efficiency score
	printEfficiency(breakdown, formatCurrency)
}

// printDelayCosts prints delay and future costs section.
func printDelayCosts(breakdown *cost.Breakdown, formatCurrency func(float64) string) {
	// Merge Delay Costs
	fmt.Println("  Delay Costs")
	fmt.Println("  ───────────")

	if breakdown.DelayCostDetail.DeliveryDelayHours > 0 {
		cappedSuffix := ""
		if breakdown.DelayCapped {
			cappedSuffix = " (capped)"
		}
		fmt.Printf("    Workstream blockage       %12s    %s%s\n",
			formatCurrency(breakdown.DelayCostDetail.DeliveryDelayCost),
			formatTimeUnit(breakdown.DelayCostDetail.DeliveryDelayHours),
			cappedSuffix)
	}

	if breakdown.DelayCostDetail.CoordinationHours > 0 {
		cappedSuffix := ""
		if breakdown.DelayCapped {
			cappedSuffix = " (capped)"
		}
		fmt.Printf("    Coordination              %12s    %s%s\n",
			formatCurrency(breakdown.DelayCostDetail.CoordinationCost),
			formatTimeUnit(breakdown.DelayCostDetail.CoordinationHours),
			cappedSuffix)
	}

	mergeDelayCost := breakdown.DelayCostDetail.DeliveryDelayCost + breakdown.DelayCostDetail.CoordinationCost
	mergeDelayHours := breakdown.DelayCostDetail.DeliveryDelayHours + breakdown.DelayCostDetail.CoordinationHours
	fmt.Println("                              ────────────")
	pct := (mergeDelayCost / breakdown.TotalCost) * 100
	fmt.Printf("    Subtotal                  %12s    %s  (%.1f%%)\n",
		formatCurrency(mergeDelayCost), formatTimeUnit(mergeDelayHours), pct)
	fmt.Println()

	// Future Costs
	hasFutureCosts := breakdown.DelayCostDetail.ReworkPercentage > 0 ||
		breakdown.DelayCostDetail.FutureReviewCost > 0 ||
		breakdown.DelayCostDetail.FutureMergeCost > 0 ||
		breakdown.DelayCostDetail.FutureContextCost > 0

	if hasFutureCosts {
		printFutureCosts(breakdown, formatCurrency)
	}
}

// printFutureCosts prints future costs subsection.
func printFutureCosts(breakdown *cost.Breakdown, formatCurrency func(float64) string) {
	fmt.Println("  Future Costs")
	fmt.Println("  ────────────")

	if breakdown.DelayCostDetail.ReworkPercentage > 0 {
		label := fmt.Sprintf("Code Churn (%.0f%% drift)", breakdown.DelayCostDetail.ReworkPercentage)
		fmt.Printf("    %-26s%12s    %s\n",
			label,
			formatCurrency(breakdown.DelayCostDetail.CodeChurnCost),
			formatTimeUnit(breakdown.DelayCostDetail.CodeChurnHours))
	}

	if breakdown.DelayCostDetail.FutureReviewCost > 0 {
		fmt.Printf("    %-26s%12s    %s\n",
			"Review",
			formatCurrency(breakdown.DelayCostDetail.FutureReviewCost),
			formatTimeUnit(breakdown.DelayCostDetail.FutureReviewHours))
	}

	if breakdown.DelayCostDetail.FutureMergeCost > 0 {
		fmt.Printf("    %-26s%12s    %s\n",
			"Merge",
			formatCurrency(breakdown.DelayCostDetail.FutureMergeCost),
			formatTimeUnit(breakdown.DelayCostDetail.FutureMergeHours))
	}

	if breakdown.DelayCostDetail.FutureContextCost > 0 {
		fmt.Printf("    %-26s%12s    %s\n",
			"Context Switching",
			formatCurrency(breakdown.DelayCostDetail.FutureContextCost),
			formatTimeUnit(breakdown.DelayCostDetail.FutureContextHours))
	}

	futureCost := breakdown.DelayCostDetail.CodeChurnCost +
		breakdown.DelayCostDetail.FutureReviewCost +
		breakdown.DelayCostDetail.FutureMergeCost +
		breakdown.DelayCostDetail.FutureContextCost
	futureHours := breakdown.DelayCostDetail.CodeChurnHours +
		breakdown.DelayCostDetail.FutureReviewHours +
		breakdown.DelayCostDetail.FutureMergeHours +
		breakdown.DelayCostDetail.FutureContextHours
	fmt.Println("                              ────────────")
	pct := (futureCost / breakdown.TotalCost) * 100
	fmt.Printf("    Subtotal                  %12s    %s  (%.1f%%)\n",
		formatCurrency(futureCost), formatTimeUnit(futureHours), pct)
	fmt.Println()
}

// formatWithCommas formats a float with commas for thousands separators.
func formatWithCommas(amount float64) string {
	// Format with 2 decimal places
	s := fmt.Sprintf("%.2f", amount)

	// Split into integer and decimal parts
	parts := strings.Split(s, ".")
	intPart := parts[0]
	decPart := parts[1]

	// Add commas to integer part
	var result []rune
	for i, r := range intPart {
		if i > 0 && (len(intPart)-i)%3 == 0 {
			result = append(result, ',')
		}
		result = append(result, r)
	}

	return string(result) + "." + decPart
}

// efficiencyGrade returns a letter grade and message based on efficiency percentage (MIT scale).
func efficiencyGrade(efficiencyPct float64) (grade, message string) {
	switch {
	case efficiencyPct >= 97:
		return "A+", "Impeccable"
	case efficiencyPct >= 93:
		return "A", "Excellent"
	case efficiencyPct >= 90:
		return "A-", "Nearly excellent"
	case efficiencyPct >= 87:
		return "B+", "Acceptable+"
	case efficiencyPct >= 83:
		return "B", "Acceptable"
	case efficiencyPct >= 80:
		return "B-", "Nearly acceptable"
	case efficiencyPct >= 70:
		return "C", "Average"
	case efficiencyPct >= 60:
		return "D", "Not good my friend."
	default:
		return "F", "Failing"
	}
}

// mergeVelocityGrade returns a grade based on average PR open time in days.
// A+: 4h, A: 8h, A-: 12h, B+: 18h, B: 24h, B-: 36h, C: 100h, D: 120h, F: 120h+.
func mergeVelocityGrade(avgOpenDays float64) (grade, message string) {
	switch {
	case avgOpenDays <= 0.1667: // 4 hours
		return "A+", "Impeccable"
	case avgOpenDays <= 0.3333: // 8 hours
		return "A", "Excellent"
	case avgOpenDays <= 0.5: // 12 hours
		return "A-", "Nearly excellent"
	case avgOpenDays <= 0.75: // 18 hours
		return "B+", "Acceptable+"
	case avgOpenDays <= 1.0: // 24 hours
		return "B", "Acceptable"
	case avgOpenDays <= 1.5: // 36 hours
		return "B-", "Nearly acceptable"
	case avgOpenDays <= 4.1667: // 100 hours
		return "C", "Average"
	case avgOpenDays <= 5.0: // 120 hours
		return "D", "Not good my friend."
	default:
		return "F", "Failing"
	}
}

// printEfficiency prints the workflow efficiency section for a single PR.
func printEfficiency(breakdown *cost.Breakdown, formatCurrency func(float64) string) {
	// Calculate preventable waste: Code Churn + All Delay Costs
	preventableHours := breakdown.DelayCostDetail.CodeChurnHours +
		breakdown.DelayCostDetail.DeliveryDelayHours +
		breakdown.DelayCostDetail.CoordinationHours
	preventableCost := breakdown.DelayCostDetail.CodeChurnCost +
		breakdown.DelayCostDetail.DeliveryDelayCost +
		breakdown.DelayCostDetail.CoordinationCost

	// Calculate total hours
	totalHours := breakdown.Author.TotalHours + breakdown.DelayCostDetail.TotalDelayHours
	for _, p := range breakdown.Participants {
		totalHours += p.TotalHours
	}

	// Calculate efficiency
	var efficiencyPct float64
	if totalHours > 0 {
		efficiencyPct = 100.0 * (totalHours - preventableHours) / totalHours
	} else {
		efficiencyPct = 100.0
	}

	grade, message := efficiencyGrade(efficiencyPct)

	// Calculate merge velocity grade based on PR duration
	prDurationDays := breakdown.PRDuration / 24.0
	velocityGrade, velocityMessage := mergeVelocityGrade(prDurationDays)

	fmt.Println("  ┌─────────────────────────────────────────────────────────────┐")
	headerText := fmt.Sprintf("DEVELOPMENT EFFICIENCY: %s (%.1f%%) - %s", grade, efficiencyPct, message)
	padding := 60 - len(headerText)
	if padding < 0 {
		padding = 0
	}
	fmt.Printf("  │ %s%*s│\n", headerText, padding, "")
	fmt.Println("  └─────────────────────────────────────────────────────────────┘")

	fmt.Println("  ┌─────────────────────────────────────────────────────────────┐")
	velocityHeader := fmt.Sprintf("MERGE VELOCITY: %s (%s) - %s", velocityGrade, formatTimeUnit(breakdown.PRDuration), velocityMessage)
	velPadding := 60 - len(velocityHeader)
	if velPadding < 0 {
		velPadding = 0
	}
	fmt.Printf("  │ %s%*s│\n", velocityHeader, velPadding, "")
	fmt.Println("  └─────────────────────────────────────────────────────────────┘")

	fmt.Printf("  Preventable Waste:         $%12s    %s\n",
		formatWithCommas(preventableCost), formatTimeUnit(preventableHours))
	fmt.Println()
}
