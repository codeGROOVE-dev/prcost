package cost

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

var (
	errNoSamples = errors.New("no samples provided")
	errNoFetcher = errors.New("fetcher is required")
)

// PRFetcher is an interface for fetching PR data.
// This allows different implementations (with/without caching, different data sources).
type PRFetcher interface {
	// FetchPRData fetches full PR data for analysis.
	FetchPRData(ctx context.Context, prURL string, updatedAt time.Time) (PRData, error)
}

// AnalysisRequest contains parameters for analyzing a set of PRs.
//
//nolint:govet // fieldalignment: struct field order optimized for API clarity
type AnalysisRequest struct {
	Fetcher     PRFetcher       // PR data fetcher
	Config      Config          // Cost calculation configuration
	Samples     []PRSummaryInfo // PRs to analyze
	Logger      *slog.Logger    // Optional logger for progress
	Concurrency int             // Number of concurrent fetches (0 = sequential)
}

// PRSummaryInfo contains basic PR information needed for fetching and analysis.
type PRSummaryInfo struct {
	UpdatedAt time.Time
	Owner     string
	Repo      string
	State     string // "OPEN", "CLOSED", "MERGED"
	Number    int
	Merged    bool // Whether the PR was merged
}

// AnalysisResult contains the breakdowns from analyzed PRs.
type AnalysisResult struct {
	Breakdowns []Breakdown
	Skipped    int // Number of PRs that failed to fetch
}

// AnalyzePRs processes a set of PRs and returns their cost breakdowns.
// This is the shared code path used by both CLI and server.
func AnalyzePRs(ctx context.Context, req *AnalysisRequest) (*AnalysisResult, error) {
	if len(req.Samples) == 0 {
		return nil, errNoSamples
	}

	if req.Fetcher == nil {
		return nil, errNoFetcher
	}

	// Default to sequential processing if concurrency not specified
	concurrency := req.Concurrency
	if concurrency <= 0 {
		concurrency = 1
	}

	var breakdowns []Breakdown
	var mu sync.Mutex
	var skipped int

	// Sequential processing
	if concurrency == 1 {
		for i, pr := range req.Samples {
			prURL := fmt.Sprintf("https://github.com/%s/%s/pull/%d", pr.Owner, pr.Repo, pr.Number)

			if req.Logger != nil {
				req.Logger.InfoContext(ctx, "Processing sample PR",
					"repo", fmt.Sprintf("%s/%s", pr.Owner, pr.Repo),
					"number", pr.Number,
					"progress", fmt.Sprintf("%d/%d", i+1, len(req.Samples)))
			}

			prData, err := req.Fetcher.FetchPRData(ctx, prURL, pr.UpdatedAt)
			if err != nil {
				if req.Logger != nil {
					req.Logger.WarnContext(ctx, "Failed to fetch PR data, skipping",
						"pr_number", pr.Number, "error", err)
				}
				skipped++
				continue
			}

			breakdown := Calculate(prData, req.Config)
			breakdowns = append(breakdowns, breakdown)
		}
	} else {
		// Parallel processing with semaphore
		var wg sync.WaitGroup
		semaphore := make(chan struct{}, concurrency)

		for i, pr := range req.Samples {
			wg.Add(1)
			go func(index int, prInfo PRSummaryInfo) {
				defer wg.Done()

				// Acquire semaphore slot
				semaphore <- struct{}{}
				defer func() { <-semaphore }()

				prURL := fmt.Sprintf("https://github.com/%s/%s/pull/%d", prInfo.Owner, prInfo.Repo, prInfo.Number)

				if req.Logger != nil {
					req.Logger.InfoContext(ctx, "Processing sample PR",
						"repo", fmt.Sprintf("%s/%s", prInfo.Owner, prInfo.Repo),
						"number", prInfo.Number,
						"progress", fmt.Sprintf("%d/%d", index+1, len(req.Samples)))
				}

				prData, err := req.Fetcher.FetchPRData(ctx, prURL, prInfo.UpdatedAt)
				if err != nil {
					if req.Logger != nil {
						req.Logger.WarnContext(ctx, "Failed to fetch PR data, skipping",
							"pr_number", prInfo.Number, "error", err)
					}
					mu.Lock()
					skipped++
					mu.Unlock()
					return
				}

				breakdown := Calculate(prData, req.Config)
				mu.Lock()
				breakdowns = append(breakdowns, breakdown)
				mu.Unlock()
			}(i, pr)
		}

		wg.Wait()
	}

	if len(breakdowns) == 0 {
		return nil, fmt.Errorf("no samples could be processed successfully (%d skipped)", skipped)
	}

	return &AnalysisResult{
		Breakdowns: breakdowns,
		Skipped:    skipped,
	}, nil
}
