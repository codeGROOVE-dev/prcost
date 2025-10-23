// Package cost provides cost calculation and extrapolation for PRs.
package cost

// ExtrapolatedBreakdown represents cost estimates extrapolated from a sample
// of PRs to estimate total costs across a larger population.
type ExtrapolatedBreakdown struct {
	// Sample metadata
	TotalPRs          int     // Total number of PRs in the population
	SampledPRs        int     // Number of PRs successfully sampled
	SuccessfulSamples int     // Number of samples that processed successfully

	// Author costs (extrapolated)
	AuthorCodeCost          float64
	AuthorGitHubCost        float64
	AuthorGitHubContextCost float64
	AuthorTotalCost         float64

	// Author hours (extrapolated)
	AuthorCodeHours          float64
	AuthorGitHubHours        float64
	AuthorGitHubContextHours float64
	AuthorTotalHours         float64

	// Participant costs (extrapolated, combined across all reviewers)
	ParticipantGitHubCost   float64
	ParticipantContextCost  float64
	ParticipantTotalCost    float64

	// Participant hours (extrapolated)
	ParticipantGitHubHours   float64
	ParticipantContextHours  float64
	ParticipantTotalHours    float64

	// Delay costs (extrapolated)
	DeliveryDelayCost  float64
	CoordinationCost   float64
	CodeChurnCost      float64
	FutureReviewCost   float64
	FutureMergeCost    float64
	FutureContextCost  float64
	DelayTotalCost     float64

	// Delay hours (extrapolated)
	DeliveryDelayHours float64
	CoordinationHours  float64
	CodeChurnHours     float64
	FutureReviewHours  float64
	FutureMergeHours   float64
	FutureContextHours float64
	DelayTotalHours    float64

	// Grand totals
	TotalCost  float64
	TotalHours float64
}

// ExtrapolateFromSamples calculates extrapolated cost estimates from a sample
// of PR breakdowns to estimate costs across a larger population.
//
// Parameters:
//   - breakdowns: Slice of Breakdown structs from successfully processed samples
//   - totalPRs: Total number of PRs in the population
//
// Returns:
//   - ExtrapolatedBreakdown with averaged costs scaled to total population
//
// The function computes the average cost per PR from the samples, then multiplies
// by the total PR count to estimate population-wide costs.
func ExtrapolateFromSamples(breakdowns []Breakdown, totalPRs int) ExtrapolatedBreakdown {
	if len(breakdowns) == 0 {
		return ExtrapolatedBreakdown{
			TotalPRs:          totalPRs,
			SampledPRs:        0,
			SuccessfulSamples: 0,
		}
	}

	successfulSamples := len(breakdowns)
	multiplier := float64(totalPRs)

	// Accumulate costs from all samples
	var sumAuthorCodeCost, sumAuthorGitHubCost, sumAuthorGitHubContextCost float64
	var sumAuthorCodeHours, sumAuthorGitHubHours, sumAuthorGitHubContextHours float64
	var sumParticipantGitHubCost, sumParticipantContextCost, sumParticipantCost float64
	var sumParticipantGitHubHours, sumParticipantContextHours, sumParticipantHours float64
	var sumDeliveryDelayCost, sumCoordinationCost, sumCodeChurnCost float64
	var sumFutureReviewCost, sumFutureMergeCost, sumFutureContextCost, sumDelayCost float64
	var sumDeliveryDelayHours, sumCoordinationHours, sumCodeChurnHours float64
	var sumFutureReviewHours, sumFutureMergeHours, sumFutureContextHours, sumDelayHours float64
	var sumAuthorHours float64
	var sumTotalCost float64

	for _, breakdown := range breakdowns {
		// Accumulate author costs
		sumAuthorCodeCost += breakdown.Author.CodeCost
		sumAuthorGitHubCost += breakdown.Author.GitHubCost
		sumAuthorGitHubContextCost += breakdown.Author.GitHubContextCost
		sumAuthorCodeHours += breakdown.Author.CodeHours
		sumAuthorGitHubHours += breakdown.Author.GitHubHours
		sumAuthorGitHubContextHours += breakdown.Author.GitHubContextHours
		sumAuthorHours += breakdown.Author.TotalHours

		// Accumulate participant costs (combined across all participants)
		for _, p := range breakdown.Participants {
			sumParticipantGitHubCost += p.GitHubCost
			sumParticipantContextCost += p.GitHubContextCost
			sumParticipantCost += p.TotalCost
			sumParticipantGitHubHours += p.GitHubHours
			sumParticipantContextHours += p.GitHubContextHours
			sumParticipantHours += p.TotalHours
		}

		// Accumulate delay costs
		sumDeliveryDelayCost += breakdown.DelayCostDetail.DeliveryDelayCost
		sumCoordinationCost += breakdown.DelayCostDetail.CoordinationCost
		sumCodeChurnCost += breakdown.DelayCostDetail.CodeChurnCost
		sumFutureReviewCost += breakdown.DelayCostDetail.FutureReviewCost
		sumFutureMergeCost += breakdown.DelayCostDetail.FutureMergeCost
		sumFutureContextCost += breakdown.DelayCostDetail.FutureContextCost
		sumDeliveryDelayHours += breakdown.DelayCostDetail.DeliveryDelayHours
		sumCoordinationHours += breakdown.DelayCostDetail.CoordinationHours
		sumCodeChurnHours += breakdown.DelayCostDetail.CodeChurnHours
		sumFutureReviewHours += breakdown.DelayCostDetail.FutureReviewHours
		sumFutureMergeHours += breakdown.DelayCostDetail.FutureMergeHours
		sumFutureContextHours += breakdown.DelayCostDetail.FutureContextHours
		sumDelayCost += breakdown.DelayCost
		sumDelayHours += breakdown.DelayCostDetail.TotalDelayHours

		sumTotalCost += breakdown.TotalCost
	}

	// Calculate averages and extrapolate to total PRs
	samples := float64(successfulSamples)

	extAuthorCodeCost := sumAuthorCodeCost / samples * multiplier
	extAuthorGitHubCost := sumAuthorGitHubCost / samples * multiplier
	extAuthorGitHubContextCost := sumAuthorGitHubContextCost / samples * multiplier
	extAuthorCodeHours := sumAuthorCodeHours / samples * multiplier
	extAuthorGitHubHours := sumAuthorGitHubHours / samples * multiplier
	extAuthorGitHubContextHours := sumAuthorGitHubContextHours / samples * multiplier
	extAuthorTotal := extAuthorCodeCost + extAuthorGitHubCost + extAuthorGitHubContextCost
	extAuthorHours := sumAuthorHours / samples * multiplier

	extParticipantGitHubCost := sumParticipantGitHubCost / samples * multiplier
	extParticipantContextCost := sumParticipantContextCost / samples * multiplier
	extParticipantCost := sumParticipantCost / samples * multiplier
	extParticipantGitHubHours := sumParticipantGitHubHours / samples * multiplier
	extParticipantContextHours := sumParticipantContextHours / samples * multiplier
	extParticipantHours := sumParticipantHours / samples * multiplier

	extDeliveryDelayCost := sumDeliveryDelayCost / samples * multiplier
	extCoordinationCost := sumCoordinationCost / samples * multiplier
	extCodeChurnCost := sumCodeChurnCost / samples * multiplier
	extFutureReviewCost := sumFutureReviewCost / samples * multiplier
	extFutureMergeCost := sumFutureMergeCost / samples * multiplier
	extFutureContextCost := sumFutureContextCost / samples * multiplier
	extDeliveryDelayHours := sumDeliveryDelayHours / samples * multiplier
	extCoordinationHours := sumCoordinationHours / samples * multiplier
	extCodeChurnHours := sumCodeChurnHours / samples * multiplier
	extFutureReviewHours := sumFutureReviewHours / samples * multiplier
	extFutureMergeHours := sumFutureMergeHours / samples * multiplier
	extFutureContextHours := sumFutureContextHours / samples * multiplier
	extDelayTotal := sumDelayCost / samples * multiplier
	extDelayHours := sumDelayHours / samples * multiplier

	extTotalCost := sumTotalCost / samples * multiplier
	extTotalHours := extAuthorHours + extParticipantHours + extDelayHours

	return ExtrapolatedBreakdown{
		TotalPRs:          totalPRs,
		SampledPRs:        successfulSamples,
		SuccessfulSamples: successfulSamples,

		AuthorCodeCost:          extAuthorCodeCost,
		AuthorGitHubCost:        extAuthorGitHubCost,
		AuthorGitHubContextCost: extAuthorGitHubContextCost,
		AuthorTotalCost:         extAuthorTotal,

		AuthorCodeHours:          extAuthorCodeHours,
		AuthorGitHubHours:        extAuthorGitHubHours,
		AuthorGitHubContextHours: extAuthorGitHubContextHours,
		AuthorTotalHours:         extAuthorHours,

		ParticipantGitHubCost:  extParticipantGitHubCost,
		ParticipantContextCost: extParticipantContextCost,
		ParticipantTotalCost:   extParticipantCost,

		ParticipantGitHubHours:  extParticipantGitHubHours,
		ParticipantContextHours: extParticipantContextHours,
		ParticipantTotalHours:   extParticipantHours,

		DeliveryDelayCost: extDeliveryDelayCost,
		CoordinationCost:  extCoordinationCost,
		CodeChurnCost:     extCodeChurnCost,
		FutureReviewCost:  extFutureReviewCost,
		FutureMergeCost:   extFutureMergeCost,
		FutureContextCost: extFutureContextCost,
		DelayTotalCost:    extDelayTotal,

		DeliveryDelayHours: extDeliveryDelayHours,
		CoordinationHours:  extCoordinationHours,
		CodeChurnHours:     extCodeChurnHours,
		FutureReviewHours:  extFutureReviewHours,
		FutureMergeHours:   extFutureMergeHours,
		FutureContextHours: extFutureContextHours,
		DelayTotalHours:    extDelayHours,

		TotalCost:  extTotalCost,
		TotalHours: extTotalHours,
	}
}
