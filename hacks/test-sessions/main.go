// Package main tests session grouping logic.
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/codeGROOVE-dev/prcost/pkg/cost"
	"github.com/codeGROOVE-dev/prcost/pkg/github"
)

func main() {
	ctx := context.Background()
	token := "" // Will use cached data

	prURL := "https://github.com/chainguard-dev/malcontent/pull/1155"

	prData, err := github.FetchPRData(ctx, prURL, token, time.Now())
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	fmt.Printf("Total events: %d\n", len(prData.Events))
	fmt.Printf("Author: %s\n\n", prData.Author)

	// Collect author events
	var authorEvents []cost.ParticipantEvent
	for _, event := range prData.Events {
		if event.Kind == "commit" || event.Actor == prData.Author {
			authorEvents = append(authorEvents, event)
		}
	}

	fmt.Printf("Author events: %d\n", len(authorEvents))

	// Sort and group into sessions
	sorted := make([]cost.ParticipantEvent, len(authorEvents))
	copy(sorted, authorEvents)

	// Simple sort
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[j].Timestamp.Before(sorted[i].Timestamp) {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	cfg := cost.DefaultConfig()
	fmt.Printf("\nSessionGapThreshold: %v\n", cfg.SessionGapThreshold)
	fmt.Printf("EventDuration: %v\n\n", cfg.EventDuration)

	// Group into sessions
	type session struct {
		events []cost.ParticipantEvent
	}
	var sessions []session

	i := 0
	for i < len(sorted) {
		sess := session{events: []cost.ParticipantEvent{sorted[i]}}
		start := i
		end := start

		for end+1 < len(sorted) {
			gap := sorted[end+1].Timestamp.Sub(sorted[end].Timestamp)
			if gap > cfg.SessionGapThreshold {
				break
			}
			end++
			sess.events = append(sess.events, sorted[end])
		}

		sessions = append(sessions, sess)
		i = end + 1
	}

	fmt.Printf("Sessions: %d\n\n", len(sessions))

	totalGitHubTime := time.Duration(0)
	for idx, sess := range sessions {
		eventsInSession := len(sess.events)
		sessionTime := time.Duration(eventsInSession) * cfg.EventDuration
		totalGitHubTime += sessionTime

		fmt.Printf("Session %d: %d events, %v GitHub time\n", idx+1, eventsInSession, sessionTime)
		for _, e := range sess.events {
			fmt.Printf("  %s  %s  %s\n", e.Timestamp.Format("2006-01-02 15:04:05"), e.Kind, e.Actor)
		}
	}

	fmt.Printf("\nTotal GitHub time: %v (%.2f hrs)\n", totalGitHubTime, totalGitHubTime.Hours())
	fmt.Printf("Expected: %d events × %v = %v (%.2f hrs)\n",
		len(authorEvents), cfg.EventDuration,
		time.Duration(len(authorEvents))*cfg.EventDuration,
		(time.Duration(len(authorEvents)) * cfg.EventDuration).Hours())
}
