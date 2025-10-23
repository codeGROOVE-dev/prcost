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
	overheadFactor := flag.Float64("overhead-factor", 0.25, "Delay cost factor (0.25 = 25%)")
	format := flag.String("format", "human", "Output format: human or json")
	verbose := flag.Bool("verbose", false, "Show verbose logging output")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [options] <PR_URL>\n\n", os.Args[0])
		fmt.Fprint(os.Stderr, "Calculate the real-world cost of a GitHub pull request.\n\n")
		fmt.Fprint(os.Stderr, "Options:\n")
		flag.PrintDefaults()
		fmt.Fprint(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  %s https://github.com/owner/repo/pull/123\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s --salary 300000 --benefits 1.4 https://github.com/owner/repo/pull/123\n", os.Args[0])
		fmt.Fprintf(os.Stderr,
			"  %s --salary 200000 --benefits 1.25 --event-minutes 30 --format json https://github.com/owner/repo/pull/123\n",
			os.Args[0])
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

	// Validate that we have a PR URL
	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(1)
	}

	prURL := flag.Arg(0)

	// Validate PR URL format
	if !strings.HasPrefix(prURL, "https://github.com/") || !strings.Contains(prURL, "/pull/") {
		log.Fatal("Invalid PR URL. Expected format: https://github.com/owner/repo/pull/123")
	}

	slog.Info("Starting PR cost analysis", "pr_url", prURL, "format", *format)

	// Create cost configuration from flags
	cfg := cost.DefaultConfig()
	cfg.AnnualSalary = *salary
	cfg.BenefitsMultiplier = *benefits
	cfg.EventDuration = time.Duration(*eventMinutes) * time.Minute
	cfg.DelayCostFactor = *overheadFactor

	slog.Debug("Configuration",
		"salary", cfg.AnnualSalary,
		"benefits_multiplier", cfg.BenefitsMultiplier,
		"event_minutes", *eventMinutes,
		"delay_cost_factor", cfg.DelayCostFactor)

	// Retrieve GitHub token from gh CLI
	ctx := context.Background()
	slog.Info("Retrieving GitHub authentication token")
	token, err := authToken(ctx)
	if err != nil {
		slog.Error("Failed to get GitHub token", "error", err)
		log.Fatalf("Failed to get GitHub token: %v\nPlease ensure 'gh' is installed and authenticated (run 'gh auth login')", err)
	}
	slog.Debug("Successfully retrieved GitHub token")

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
	fmt.Printf("    Code Creation             %12s    %d LOC • %.1f hrs\n",
		formatCurrency(breakdown.Author.CodeCost), breakdown.Author.LinesAdded, breakdown.Author.CodeHours)
	fmt.Printf("    Code Context Switching    %12s    %.1f hrs\n",
		formatCurrency(breakdown.Author.CodeContextCost), breakdown.Author.CodeContextHours)
	fmt.Printf("    GitHub Activity           %12s    %d events • %.1f hrs\n",
		formatCurrency(breakdown.Author.GitHubCost), breakdown.Author.Events, breakdown.Author.GitHubHours)
	fmt.Printf("    GitHub Context Switching  %12s    %d sessions • %.1f hrs\n",
		formatCurrency(breakdown.Author.GitHubContextCost), breakdown.Author.Sessions, breakdown.Author.GitHubContextHours)
	fmt.Println("                              ────────────")
	fmt.Printf("    Subtotal                  %12s    %.1f hrs\n",
		formatCurrency(breakdown.Author.TotalCost), breakdown.Author.TotalHours)
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

		fmt.Println("  Reviewers")
		fmt.Println("  ─────────")
		for _, p := range breakdown.Participants {
			fmt.Printf("    %s\n", p.Actor)
			fmt.Printf("      Review Activity         %12s    %d events • %.1f hrs\n",
				formatCurrency(p.GitHubCost), p.Events, p.GitHubHours)
			fmt.Printf("      Context Switching       %12s    %d sessions • %.1f hrs\n",
				formatCurrency(p.GitHubContextCost), p.Sessions, p.GitHubContextHours)
		}
		fmt.Println("                              ────────────")
		fmt.Printf("    Subtotal                  %12s    %.1f hrs\n",
			formatCurrency(totalParticipantCost), totalParticipantHours)
		fmt.Println()
	}

	// Delay Costs
	fmt.Println("  Delay")
	fmt.Println("  ─────")
	if breakdown.DelayCapped {
		fmt.Printf("    Delivery Delay (20%%)      %12s    %.0f hrs (capped)\n",
			formatCurrency(breakdown.DelayCostDetail.ProjectDelayCost), breakdown.DelayCostDetail.ProjectDelayHours)
	} else {
		fmt.Printf("    Delivery Delay (20%%)      %12s    %.1f hrs\n",
			formatCurrency(breakdown.DelayCostDetail.ProjectDelayCost), breakdown.DelayCostDetail.ProjectDelayHours)
	}

	if breakdown.DelayCostDetail.ReworkPercentage > 0 {
		fmt.Printf("    Code Updates (%.0f%% drift)   %12s    %.1f hrs\n",
			breakdown.DelayCostDetail.ReworkPercentage,
			formatCurrency(breakdown.DelayCostDetail.CodeUpdatesCost),
			breakdown.DelayCostDetail.CodeUpdatesHours)
	}

	if breakdown.DelayCostDetail.FutureGitHubCost > 0 {
		fmt.Printf("    Est. Future GitHub        %12s    %.1f hrs\n",
			formatCurrency(breakdown.DelayCostDetail.FutureGitHubCost), breakdown.DelayCostDetail.FutureGitHubHours)
	}
	fmt.Println("                              ────────────")
	fmt.Printf("    Subtotal                  %12s    %.1f hrs\n",
		formatCurrency(breakdown.DelayCost), breakdown.DelayCostDetail.TotalDelayHours)
	fmt.Println()

	// Grand Total
	totalHours := breakdown.Author.TotalHours + breakdown.DelayCostDetail.TotalDelayHours
	for _, p := range breakdown.Participants {
		totalHours += p.TotalHours
	}
	fmt.Println("  ═══════════════════════════════════════════════════════════════")
	fmt.Printf("  Total                       %12s    %.1f hrs\n",
		formatCurrency(breakdown.TotalCost), totalHours)
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
