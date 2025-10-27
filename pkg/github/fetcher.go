package github

import (
	"context"
	"time"

	"github.com/codeGROOVE-dev/prcost/pkg/cost"
)

// SimpleFetcher is a PRFetcher that fetches PR data without caching.
// It uses either prx or turnserver based on configuration.
type SimpleFetcher struct {
	Token      string
	DataSource string // "prx" or "turnserver"
}

// FetchPRData implements the PRFetcher interface from pkg/cost.
func (f *SimpleFetcher) FetchPRData(ctx context.Context, prURL string, updatedAt time.Time) (cost.PRData, error) {
	if f.DataSource == "turnserver" {
		return FetchPRDataViaTurnserver(ctx, prURL, f.Token, updatedAt)
	}
	return FetchPRData(ctx, prURL, f.Token, updatedAt)
}
