// Package main debugs session calculation for a specific PR.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/codeGROOVE-dev/prcost/pkg/cost"
	"github.com/codeGROOVE-dev/prcost/pkg/github"
)

func main() {
	ctx := context.Background()
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		fmt.Fprintln(os.Stderr, "GITHUB_TOKEN not set")
		os.Exit(1)
	}

	prURL := "https://github.com/chainguard-dev/malcontent/pull/1155"

	prData, err := github.FetchPRData(ctx, prURL, token, time.Now())
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("PR Author: %s\n", prData.Author)
	fmt.Printf("Total Events: %d\n", len(prData.Events))
	fmt.Println("\nAll Events (sorted by time):")

	// Collect author events
	var authorEvents []cost.ParticipantEvent
	for _, event := range prData.Events {
		if event.Kind == "commit" || event.Actor == prData.Author {
			authorEvents = append(authorEvents, event)
		}
		fmt.Printf("  %s | %-10s | %s\n",
			event.Timestamp.Format("2006-01-02 15:04:05"),
			event.Actor,
			event.Kind)
	}

	fmt.Printf("\nAuthor Events: %d\n", len(authorEvents))

	// Manually trace through session logic
	fmt.Println("\nSession Breakdown:")

	cfg := cost.DefaultConfig()
	gapThreshold := cfg.SessionGapThreshold
	contextIn := cfg.ContextSwitchInDuration
	contextOut := cfg.ContextSwitchOutDuration
	eventDur := cfg.EventDuration

	fmt.Printf("Gap Threshold: %v\n", gapThreshold)
	fmt.Printf("Context Switch In: %v\n", contextIn)
	fmt.Printf("Context Switch Out: %v\n", contextOut)
	fmt.Printf("Event Duration: %v\n\n", eventDur)

	// Sort events by time
	events := make([]cost.ParticipantEvent, len(authorEvents))
	copy(events, authorEvents)
	// Sort manually
	for i := 0; i < len(events); i++ {
		for j := i + 1; j < len(events); j++ {
			if events[j].Timestamp.Before(events[i].Timestamp) {
				events[i], events[j] = events[j], events[i]
			}
		}
	}

	var totalGitHub time.Duration
	var totalContext time.Duration
	sessionNum := 0

	i := 0
	for i < len(events) {
		sessionNum++
		start := i

		// Find end of session
		end := start
		for end+1 < len(events) {
			gap := events[end+1].Timestamp.Sub(events[end].Timestamp)
			if gap > gapThreshold {
				break
			}
			end++
		}

		eventsInSession := end - start + 1
		fmt.Printf("Session %d: %d events\n", sessionNum, eventsInSession)

		// Context in
		totalContext += contextIn
		fmt.Printf("  Context In: %v\n", contextIn)

		// First event
		totalGitHub += eventDur
		fmt.Printf("  Event %d: %v (default duration)\n", 0, eventDur)

		// Gaps between events
		for j := start; j < end; j++ {
			gap := events[j+1].Timestamp.Sub(events[j].Timestamp)
			counted := gap
			if gap > eventDur {
				counted = eventDur
			}
			totalGitHub += counted
			fmt.Printf("  Gap %d->%d: %v (actual: %v)\n", j-start, j-start+1, counted, gap)
		}

		// Context out
		totalContext += contextOut
		fmt.Printf("  Context Out: %v\n", contextOut)
		fmt.Printf("  Session Total - GitHub: %v, Context: %v\n\n",
			(time.Duration(eventsInSession) * eventDur),
			contextIn+contextOut)

		i = end + 1
	}

	fmt.Println("TOTALS:")
	fmt.Printf("  Sessions: %d\n", sessionNum)
	fmt.Printf("  GitHub Time: %v (%.2f hrs)\n", totalGitHub, totalGitHub.Hours())
	fmt.Printf("  Context Time: %v (%.2f hrs)\n", totalContext, totalContext.Hours())

	// Now compare with actual calculation
	breakdown := cost.Calculate(prData, cfg)
	fmt.Println("\nACTUAL (from Calculate):")
	fmt.Printf("  Sessions: %d\n", breakdown.Author.Sessions)
	fmt.Printf("  GitHub Time: %.2f hrs\n", breakdown.Author.GitHubHours)
	fmt.Printf("  Context Time: %.2f hrs\n", breakdown.Author.GitHubContextHours)

	// Output as JSON
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	fmt.Println("\nFull Breakdown:")
	if err := enc.Encode(breakdown.Author); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to encode JSON: %v\n", err)
	}
}
