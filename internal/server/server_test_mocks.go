package server

import (
	"fmt"
	"time"

	"github.com/codeGROOVE-dev/prcost/pkg/cost"
	"github.com/codeGROOVE-dev/prcost/pkg/github"
)

// Helper functions to create test data.
func newMockPRData(author string, linesAdded int, eventCount int) *cost.PRData {
	events := make([]cost.ParticipantEvent, eventCount)
	baseTime := time.Now().Add(-24 * time.Hour)
	for i := range eventCount {
		events[i] = cost.ParticipantEvent{
			Actor:     fmt.Sprintf("actor%d", i),
			Timestamp: baseTime.Add(time.Duration(i) * time.Hour),
		}
	}
	return &cost.PRData{
		Author:     author,
		LinesAdded: linesAdded,
		CreatedAt:  baseTime,
		Events:     events,
	}
}

func newMockPRSummaries(count int) []github.PRSummary {
	summaries := make([]github.PRSummary, count)
	for i := range count {
		summaries[i] = github.PRSummary{
			Number:    i + 1,
			Owner:     "test-owner",
			Repo:      "test-repo",
			Author:    fmt.Sprintf("author%d", i),
			UpdatedAt: time.Now().Add(-time.Duration(i) * time.Hour),
		}
	}
	return summaries
}
