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
	salary := flag.Float64("salary", 250000, "Annual salary for cost calculation")
	benefits := flag.Float64("benefits", 1.3, "Benefits multiplier (1.3 = 30% benefits)")
	eventMinutes := flag.Float64("event-minutes", 20, "Minutes per review event")
	format := flag.String("format", "human", "Output format: human or json")
	verbose := flag.Bool("verbose", false, "Show verbose logging output")

	// Org/Repo sampling flags
	org := flag.String("org", "", "GitHub organization to analyze (optionally with --repo for single repo)")
	repo := flag.String("repo", "", "GitHub repository to analyze (requires --org)")
	samples := flag.Int("samples", 25, "Number of PRs to sample for extrapolation")
	days := flag.Int("days", 90, "Number of days to look back for PR modifications")

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
		fmt.Fprintf(os.Stderr, "  Single PR:\n")
		fmt.Fprintf(os.Stderr, "    %s https://github.com/owner/repo/pull/123\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "    %s --salary 300000 https://github.com/owner/repo/pull/123\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  Repository analysis:\n")
		fmt.Fprintf(os.Stderr, "    %s --org kubernetes --repo kubernetes\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "    %s --org myorg --repo myrepo --samples 50 --days 30\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  Organization-wide analysis:\n")
		fmt.Fprintf(os.Stderr, "    %s --org chainguard-dev\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "    %s --org myorg --samples 100 --days 60\n", os.Args[0])
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
		fmt.Fprintf(os.Stderr, "Error: --repo requires --org to be specified\n\n")
		flag.Usage()
		os.Exit(1)
	}

	if orgMode && singlePRMode {
		fmt.Fprintf(os.Stderr, "Error: Cannot use both --org and PR URL. Choose one mode.\n\n")
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

			err := analyzeRepository(ctx, *org, *repo, *samples, *days, cfg, token)
			if err != nil {
				log.Fatalf("Repository analysis failed: %v", err)
			}
		} else {
			// Organization-wide mode
			slog.Info("Starting organization-wide analysis",
				"org", *org,
				"samples", *samples,
				"days", *days)

			err := analyzeOrganization(ctx, *org, *samples, *days, cfg, token)
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

		// Fetch PR data
		slog.Info("Fetching PR data from GitHub")
		prData, err := github.FetchPRData(ctx, prURL, token)
		if err != nil {
			slog.Error("Failed to fetch PR data", "error", err)
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
			if err := printJSON(&breakdown); err != nil {
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
	fmt.Printf("  Rate: %s/hr  •  Salary: %s  •  Benefits: %.1fx\n",
		formatCurrency(breakdown.HourlyRate),
		formatCurrency(breakdown.AnnualSalary),
		breakdown.BenefitsMultiplier)
	fmt.Println()

	// Author Costs
	fmt.Println("  Author")
	fmt.Println("  ──────")
	fmt.Printf("    Development Effort        %12s    %d LOC • %s\n",
		formatCurrency(breakdown.Author.CodeCost), breakdown.Author.LinesAdded, formatTimeUnit(breakdown.Author.CodeHours))
	fmt.Printf("    GitHub Activity           %12s    %d events • %s\n",
		formatCurrency(breakdown.Author.GitHubCost), breakdown.Author.Events, formatTimeUnit(breakdown.Author.GitHubHours))
	fmt.Printf("    GitHub Context Switching  %12s    %d sessions • %s\n",
		formatCurrency(breakdown.Author.GitHubContextCost), breakdown.Author.Sessions, formatTimeUnit(breakdown.Author.GitHubContextHours))
	fmt.Println("                              ────────────")
	fmt.Printf("    Subtotal                  %12s    %s\n",
		formatCurrency(breakdown.Author.TotalCost), formatTimeUnit(breakdown.Author.TotalHours))
	fmt.Println()

	// Participant Costs
	if len(breakdown.Participants) > 0 {
		// Sum all participant costs
		var totalParticipantCost float64
		var totalParticipantHours float64
		for _, p := range breakdown.Participants {
			totalParticipantCost += p.TotalCost
			totalParticipantHours += p.TotalHours
		}

		fmt.Println("  Participants")
		fmt.Println("  ────────────")
		for _, p := range breakdown.Participants {
			fmt.Printf("    %s\n", p.Actor)
			fmt.Printf("      Review Activity         %12s    %d events • %s\n",
				formatCurrency(p.GitHubCost), p.Events, formatTimeUnit(p.GitHubHours))
			fmt.Printf("      Context Switching       %12s    %d sessions • %s\n",
				formatCurrency(p.GitHubContextCost), p.Sessions, formatTimeUnit(p.GitHubContextHours))
		}
		fmt.Println("                              ────────────")
		fmt.Printf("    Subtotal                  %12s    %s\n",
			formatCurrency(totalParticipantCost), formatTimeUnit(totalParticipantHours))
		fmt.Println()
	}

	// Merge Delay Costs
	fmt.Println("  Merge Delay")
	fmt.Println("  ───────────")
	if breakdown.DelayCapped {
		fmt.Printf("    Cost of Delay           %12s    %s (capped)\n",
			formatCurrency(breakdown.DelayCostDetail.DeliveryDelayCost), formatTimeUnit(breakdown.DelayCostDetail.DeliveryDelayHours))
	} else {
		fmt.Printf("    Cost of Delay           %12s    %s\n",
			formatCurrency(breakdown.DelayCostDetail.DeliveryDelayCost), formatTimeUnit(breakdown.DelayCostDetail.DeliveryDelayHours))
	}

	if breakdown.DelayCapped {
		fmt.Printf("    Cognitive Load          %12s    %s (capped)\n",
			formatCurrency(breakdown.DelayCostDetail.CoordinationCost), formatTimeUnit(breakdown.DelayCostDetail.CoordinationHours))
	} else {
		fmt.Printf("    Cognitive Load          %12s    %s\n",
			formatCurrency(breakdown.DelayCostDetail.CoordinationCost), formatTimeUnit(breakdown.DelayCostDetail.CoordinationHours))
	}

	mergeDelayCost := breakdown.DelayCostDetail.DeliveryDelayCost + breakdown.DelayCostDetail.CoordinationCost
	mergeDelayHours := breakdown.DelayCostDetail.DeliveryDelayHours + breakdown.DelayCostDetail.CoordinationHours
	fmt.Println("                              ────────────")
	fmt.Printf("    Subtotal                %12s    %s\n",
		formatCurrency(mergeDelayCost), formatTimeUnit(mergeDelayHours))
	fmt.Println()

	// Future Costs
	hasFutureCosts := breakdown.DelayCostDetail.ReworkPercentage > 0 ||
		breakdown.DelayCostDetail.FutureReviewCost > 0 ||
		breakdown.DelayCostDetail.FutureMergeCost > 0 ||
		breakdown.DelayCostDetail.FutureContextCost > 0

	if hasFutureCosts {
		fmt.Println("  Future Costs")
		fmt.Println("  ────────────")

		if breakdown.DelayCostDetail.ReworkPercentage > 0 {
			fmt.Printf("    Code Churn (%.0f%% drift) %12s    %s\n",
				breakdown.DelayCostDetail.ReworkPercentage,
				formatCurrency(breakdown.DelayCostDetail.CodeChurnCost),
				formatTimeUnit(breakdown.DelayCostDetail.CodeChurnHours))
		}

		if breakdown.DelayCostDetail.FutureReviewCost > 0 {
			fmt.Printf("    Review                  %12s    %s\n",
				formatCurrency(breakdown.DelayCostDetail.FutureReviewCost), formatTimeUnit(breakdown.DelayCostDetail.FutureReviewHours))
		}

		if breakdown.DelayCostDetail.FutureMergeCost > 0 {
			fmt.Printf("    Merge                   %12s    %s\n",
				formatCurrency(breakdown.DelayCostDetail.FutureMergeCost), formatTimeUnit(breakdown.DelayCostDetail.FutureMergeHours))
		}

		if breakdown.DelayCostDetail.FutureContextCost > 0 {
			fmt.Printf("    Context Switching       %12s    %s\n",
				formatCurrency(breakdown.DelayCostDetail.FutureContextCost), formatTimeUnit(breakdown.DelayCostDetail.FutureContextHours))
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
		fmt.Printf("    Subtotal                %12s    %s\n",
			formatCurrency(futureCost), formatTimeUnit(futureHours))
		fmt.Println()
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

// printJSON outputs the cost breakdown in JSON format.
func printJSON(breakdown *cost.Breakdown) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(breakdown)
}
