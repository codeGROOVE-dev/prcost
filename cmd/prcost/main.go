// Package main implements a CLI tool to calculate the real-world cost of GitHub PRs.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
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

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [options] <PR_URL>\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Calculate the real-world cost of a GitHub pull request.\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  %s https://github.com/owner/repo/pull/123\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s --salary 300000 --benefits 1.4 https://github.com/owner/repo/pull/123\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s --salary 200000 --benefits 1.25 --event-minutes 30 --format json https://github.com/owner/repo/pull/123\n", os.Args[0])
	}

	flag.Parse()

	// Validate that we have a PR URL
	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(1)
	}

	prURL := flag.Arg(0)

	// Validate PR URL format
	if !strings.HasPrefix(prURL, "https://github.com/") || !strings.Contains(prURL, "/pull/") {
		log.Fatalf("Invalid PR URL. Expected format: https://github.com/owner/repo/pull/123")
	}

	// Create cost configuration from flags
	cfg := cost.DefaultConfig()
	cfg.AnnualSalary = *salary
	cfg.BenefitsMultiplier = *benefits
	cfg.EventDuration = time.Duration(*eventMinutes) * time.Minute
	cfg.DelayCostFactor = *overheadFactor

	// Get GitHub token from gh CLI
	ctx := context.Background()
	token, err := getGitHubToken(ctx)
	if err != nil {
		log.Fatalf("Failed to get GitHub token: %v\nPlease ensure 'gh' is installed and authenticated (run 'gh auth login')", err)
	}

	// Fetch PR data
	prData, err := github.FetchPRData(ctx, prURL, token)
	if err != nil {
		log.Fatalf("Failed to fetch PR data: %v", err)
	}

	// Calculate costs
	breakdown := cost.Calculate(prData, cfg)

	// Output in requested format
	switch *format {
	case "human":
		printHumanReadable(breakdown, prURL)
	case "json":
		printJSON(breakdown)
	default:
		log.Fatalf("Unknown format: %s (must be human or json)", *format)
	}
}

// getGitHubToken retrieves a GitHub token using the gh CLI.
func getGitHubToken(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "gh", "auth", "token")
	output, err := cmd.Output()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("timeout getting auth token")
		}
		return "", fmt.Errorf("failed to get auth token (is 'gh' installed and authenticated?): %w", err)
	}

	token := strings.TrimSpace(string(output))
	return token, nil
}

// printHumanReadable outputs a detailed itemized bill in human-readable format.
func printHumanReadable(b cost.Breakdown, prURL string) {
	fmt.Printf("PULL REQUEST COST ANALYSIS\n")
	fmt.Printf("==========================\n\n")
	fmt.Printf("PR URL:      %s\n", prURL)
	fmt.Printf("Hourly Rate: $%.2f ($%.0f salary * %.1fX total benefits multiplier)\n\n",
		b.HourlyRate, b.AnnualSalary, b.BenefitsMultiplier)

	// Author Costs
	fmt.Printf("AUTHOR COSTS\n")
	fmt.Printf("  Code Cost (COCOMO)          $%10.2f   (%d LOC, %.2f hrs)\n",
		b.Author.CodeCost, b.Author.LinesAdded, b.Author.CodeHours)
	fmt.Printf("  Code Context Switching      $%10.2f   (%.2f hrs)\n",
		b.Author.CodeContextCost, b.Author.CodeContextHours)
	fmt.Printf("  GitHub Time                 $%10.2f   (%d events, %.2f hrs)\n",
		b.Author.GitHubCost, b.Author.Events, b.Author.GitHubHours)
	fmt.Printf("  GitHub Context Switching    $%10.2f   (%d sessions, %.2f hrs)\n",
		b.Author.GitHubContextCost, b.Author.Sessions, b.Author.GitHubContextHours)
	fmt.Printf("  ---\n")
	fmt.Printf("  Author Subtotal             $%10.2f   (%.2f hrs total)\n\n",
		b.Author.TotalCost, b.Author.TotalHours)

	// Participant Costs
	if len(b.Participants) > 0 {
		fmt.Printf("PARTICIPANT COSTS\n")
		for _, p := range b.Participants {
			fmt.Printf("  %s\n", p.Actor)
			fmt.Printf("    Event Time                $%10.2f   (%d events, %.2f hrs)\n",
				p.GitHubCost, p.Events, p.GitHubHours)
			fmt.Printf("    Context Switching         $%10.2f   (%d sessions, %.2f hrs)\n",
				p.GitHubContextCost, p.Sessions, p.GitHubContextHours)
			fmt.Printf("    Subtotal                  $%10.2f   (%.2f hrs total)\n",
				p.TotalCost, p.TotalHours)
		}

		// Sum all participant costs
		var totalParticipantCost float64
		var totalParticipantHours float64
		for _, p := range b.Participants {
			totalParticipantCost += p.TotalCost
			totalParticipantHours += p.TotalHours
		}
		fmt.Printf("  ---\n")
		fmt.Printf("  Participants Subtotal       $%10.2f   (%.2f hrs total)\n\n",
			totalParticipantCost, totalParticipantHours)
	}

	// Delay Cost
	fmt.Printf("DELAY COST\n")
	if b.DelayCapped {
		fmt.Printf("  %-32s $%10.2f   (%.0f hrs, capped at 60 days)\n",
			"Project Delay (20%)", b.DelayCostDetail.ProjectDelayCost, b.DelayCostDetail.ProjectDelayHours)
	} else {
		fmt.Printf("  %-32s $%10.2f   (%.2f hrs)\n",
			"Project Delay (20%)", b.DelayCostDetail.ProjectDelayCost, b.DelayCostDetail.ProjectDelayHours)
	}

	if b.DelayCostDetail.ReworkPercentage > 0 {
		label := fmt.Sprintf("Code Updates (%.0f%% rework)", b.DelayCostDetail.ReworkPercentage)
		fmt.Printf("  %-32s $%10.2f   (%.2f hrs)\n",
			label, b.DelayCostDetail.CodeUpdatesCost, b.DelayCostDetail.CodeUpdatesHours)
	}

	fmt.Printf("  %-32s $%10.2f   (%.2f hrs)\n",
		"Future GitHub (3 events)", b.DelayCostDetail.FutureGitHubCost, b.DelayCostDetail.FutureGitHubHours)
	fmt.Printf("  ---\n")

	if b.DelayCapped {
		fmt.Printf("  Total Delay Cost            $%10.2f   (actual: %.0f hours open)\n\n",
			b.DelayCost, b.DelayHours)
	} else {
		fmt.Printf("  Total Delay Cost            $%10.2f\n\n", b.DelayCost)
	}

	// Total
	fmt.Printf("==========================\n")
	fmt.Printf("TOTAL COST                  $%10.2f\n", b.TotalCost)
	fmt.Printf("==========================\n")
}

// printJSON outputs the cost breakdown in JSON format.
func printJSON(b cost.Breakdown) {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(b); err != nil {
		log.Fatalf("Failed to encode JSON: %v", err)
	}
}
