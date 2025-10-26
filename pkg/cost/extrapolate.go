package cost

// ExtrapolatedBreakdown represents cost estimates extrapolated from a sample
// of PRs to estimate total costs across a larger population.
type ExtrapolatedBreakdown struct {
	// Sample metadata
	TotalPRs                      int     `json:"total_prs"`                           // Total number of PRs in the population
	SampledPRs                    int     `json:"sampled_prs"`                         // Number of PRs successfully sampled
	SuccessfulSamples             int     `json:"successful_samples"`                  // Number of samples that processed successfully
	UniqueAuthors                 int     `json:"unique_authors"`                      // Number of unique PR authors (excluding bots)
	AvgWasteHoursPerAuthorPerWeek float64 `json:"avg_waste_hours_per_author_per_week"` // Average preventable hours per author per week
	AvgWasteCostPerAuthorPerYear  float64 `json:"avg_waste_cost_per_author_per_year"`  // Average preventable cost per author per year
	AvgPRDurationHours            float64 `json:"avg_pr_duration_hours"`               // Average PR open time in hours

	// Author costs (extrapolated)
	AuthorNewCodeCost       float64 `json:"author_new_code_cost"`
	AuthorAdaptationCost    float64 `json:"author_adaptation_cost"`
	AuthorGitHubCost        float64 `json:"author_github_cost"`
	AuthorGitHubContextCost float64 `json:"author_github_context_cost"`
	AuthorTotalCost         float64 `json:"author_total_cost"`

	// Author hours (extrapolated)
	AuthorNewCodeHours       float64 `json:"author_new_code_hours"`
	AuthorAdaptationHours    float64 `json:"author_adaptation_hours"`
	AuthorGitHubHours        float64 `json:"author_github_hours"`
	AuthorGitHubContextHours float64 `json:"author_github_context_hours"`
	AuthorTotalHours         float64 `json:"author_total_hours"`

	// Participant costs (extrapolated, combined across all reviewers)
	ParticipantReviewCost  float64 `json:"participant_review_cost"`
	ParticipantGitHubCost  float64 `json:"participant_github_cost"`
	ParticipantContextCost float64 `json:"participant_context_cost"`
	ParticipantTotalCost   float64 `json:"participant_total_cost"`

	// Participant hours (extrapolated)
	ParticipantReviewHours  float64 `json:"participant_review_hours"`
	ParticipantGitHubHours  float64 `json:"participant_github_hours"`
	ParticipantContextHours float64 `json:"participant_context_hours"`
	ParticipantTotalHours   float64 `json:"participant_total_hours"`

	// Delay costs (extrapolated)
	DeliveryDelayCost float64 `json:"delivery_delay_cost"`
	CoordinationCost  float64 `json:"coordination_cost"`
	CodeChurnCost     float64 `json:"code_churn_cost"`
	FutureReviewCost  float64 `json:"future_review_cost"`
	FutureMergeCost   float64 `json:"future_merge_cost"`
	FutureContextCost float64 `json:"future_context_cost"`
	DelayTotalCost    float64 `json:"delay_total_cost"`

	// Delay hours (extrapolated)
	DeliveryDelayHours float64 `json:"delivery_delay_hours"`
	CoordinationHours  float64 `json:"coordination_hours"`
	CodeChurnHours     float64 `json:"code_churn_hours"`
	FutureReviewHours  float64 `json:"future_review_hours"`
	FutureMergeHours   float64 `json:"future_merge_hours"`
	FutureContextHours float64 `json:"future_context_hours"`
	DelayTotalHours    float64 `json:"delay_total_hours"`

	// Grand totals
	TotalCost  float64 `json:"total_cost"`
	TotalHours float64 `json:"total_hours"`
}

// ExtrapolateFromSamples calculates extrapolated cost estimates from a sample
// of PR breakdowns to estimate costs across a larger population.
//
// Parameters:
//   - breakdowns: Slice of Breakdown structs from successfully processed samples
//   - totalPRs: Total number of PRs in the population
//   - daysInPeriod: Number of days the sample covers (for annualizing per-author metrics)
//   - cfg: Configuration for hourly rate and hours per week calculation
//
// Returns:
//   - ExtrapolatedBreakdown with averaged costs scaled to total population
//
// The function computes the average cost per PR from the samples, then multiplies
// by the total PR count to estimate population-wide costs.
func ExtrapolateFromSamples(breakdowns []Breakdown, totalPRs int, daysInPeriod int, cfg Config) ExtrapolatedBreakdown {
	if len(breakdowns) == 0 {
		return ExtrapolatedBreakdown{
			TotalPRs:          totalPRs,
			SampledPRs:        0,
			SuccessfulSamples: 0,
		}
	}

	successfulSamples := len(breakdowns)
	multiplier := float64(totalPRs)

	// Track unique PR authors (excluding bots)
	uniqueAuthors := make(map[string]bool)

	// Accumulate costs from all samples
	var sumAuthorNewCodeCost, sumAuthorAdaptationCost, sumAuthorGitHubCost, sumAuthorGitHubContextCost float64
	var sumAuthorNewCodeHours, sumAuthorAdaptationHours, sumAuthorGitHubHours, sumAuthorGitHubContextHours float64
	var sumParticipantReviewCost, sumParticipantGitHubCost, sumParticipantContextCost, sumParticipantCost float64
	var sumParticipantReviewHours, sumParticipantGitHubHours, sumParticipantContextHours, sumParticipantHours float64
	var sumDeliveryDelayCost, sumCoordinationCost, sumCodeChurnCost float64
	var sumFutureReviewCost, sumFutureMergeCost, sumFutureContextCost, sumDelayCost float64
	var sumDeliveryDelayHours, sumCoordinationHours, sumCodeChurnHours float64
	var sumFutureReviewHours, sumFutureMergeHours, sumFutureContextHours, sumDelayHours float64
	var sumAuthorHours float64
	var sumTotalCost float64
	var sumPRDuration float64

	for i := range breakdowns {
		breakdown := &breakdowns[i]

		// Track unique PR authors only (excluding bots)
		if !breakdown.AuthorBot {
			uniqueAuthors[breakdown.PRAuthor] = true
		}

		// Accumulate PR duration
		sumPRDuration += breakdown.PRDuration

		// Accumulate author costs
		sumAuthorNewCodeCost += breakdown.Author.NewCodeCost
		sumAuthorAdaptationCost += breakdown.Author.AdaptationCost
		sumAuthorGitHubCost += breakdown.Author.GitHubCost
		sumAuthorGitHubContextCost += breakdown.Author.GitHubContextCost
		sumAuthorNewCodeHours += breakdown.Author.NewCodeHours
		sumAuthorAdaptationHours += breakdown.Author.AdaptationHours
		sumAuthorGitHubHours += breakdown.Author.GitHubHours
		sumAuthorGitHubContextHours += breakdown.Author.GitHubContextHours
		sumAuthorHours += breakdown.Author.TotalHours

		// Accumulate participant costs (combined across all participants)
		for _, p := range breakdown.Participants {
			sumParticipantReviewCost += p.ReviewCost
			sumParticipantGitHubCost += p.GitHubCost
			sumParticipantContextCost += p.GitHubContextCost
			sumParticipantCost += p.TotalCost
			sumParticipantReviewHours += p.ReviewHours
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

	extAuthorNewCodeCost := sumAuthorNewCodeCost / samples * multiplier
	extAuthorAdaptationCost := sumAuthorAdaptationCost / samples * multiplier
	extAuthorGitHubCost := sumAuthorGitHubCost / samples * multiplier
	extAuthorGitHubContextCost := sumAuthorGitHubContextCost / samples * multiplier
	extAuthorNewCodeHours := sumAuthorNewCodeHours / samples * multiplier
	extAuthorAdaptationHours := sumAuthorAdaptationHours / samples * multiplier
	extAuthorGitHubHours := sumAuthorGitHubHours / samples * multiplier
	extAuthorGitHubContextHours := sumAuthorGitHubContextHours / samples * multiplier
	extAuthorTotal := extAuthorNewCodeCost + extAuthorAdaptationCost + extAuthorGitHubCost + extAuthorGitHubContextCost
	extAuthorHours := sumAuthorHours / samples * multiplier

	extParticipantReviewCost := sumParticipantReviewCost / samples * multiplier
	extParticipantGitHubCost := sumParticipantGitHubCost / samples * multiplier
	extParticipantContextCost := sumParticipantContextCost / samples * multiplier
	extParticipantCost := sumParticipantCost / samples * multiplier
	extParticipantReviewHours := sumParticipantReviewHours / samples * multiplier
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

	// Calculate per-author metrics based on actual extrapolated PR activity
	// This divides the total preventable waste by authors and time period
	var avgWasteHoursPerAuthorPerWeek, avgWasteCostPerAuthorPerYear float64
	authorCount := len(uniqueAuthors)
	if authorCount > 0 && daysInPeriod > 0 {
		// Preventable hours = code churn + delivery delay + coordination
		preventableHours := extCodeChurnHours + extDeliveryDelayHours + extCoordinationHours

		// Calculate weeks in the period
		weeksInPeriod := float64(daysInPeriod) / 7.0

		// Wasted time per author per week based on actual extrapolated PR activity
		avgWasteHoursPerAuthorPerWeek = preventableHours / float64(authorCount) / weeksInPeriod

		// Calculate hourly rate for cost
		hourlyRate := (cfg.AnnualSalary * cfg.BenefitsMultiplier) / cfg.HoursPerYear

		// Annual cost = weekly hours × 52 weeks × hourly rate
		avgWasteCostPerAuthorPerYear = avgWasteHoursPerAuthorPerWeek * 52.0 * hourlyRate
	}

	// Calculate average PR duration
	avgPRDuration := sumPRDuration / samples

	return ExtrapolatedBreakdown{
		TotalPRs:                      totalPRs,
		SampledPRs:                    successfulSamples,
		SuccessfulSamples:             successfulSamples,
		UniqueAuthors:                 authorCount,
		AvgWasteHoursPerAuthorPerWeek: avgWasteHoursPerAuthorPerWeek,
		AvgWasteCostPerAuthorPerYear:  avgWasteCostPerAuthorPerYear,
		AvgPRDurationHours:            avgPRDuration,

		AuthorNewCodeCost:       extAuthorNewCodeCost,
		AuthorAdaptationCost:    extAuthorAdaptationCost,
		AuthorGitHubCost:        extAuthorGitHubCost,
		AuthorGitHubContextCost: extAuthorGitHubContextCost,
		AuthorTotalCost:         extAuthorTotal,

		AuthorNewCodeHours:       extAuthorNewCodeHours,
		AuthorAdaptationHours:    extAuthorAdaptationHours,
		AuthorGitHubHours:        extAuthorGitHubHours,
		AuthorGitHubContextHours: extAuthorGitHubContextHours,
		AuthorTotalHours:         extAuthorHours,

		ParticipantReviewCost:  extParticipantReviewCost,
		ParticipantGitHubCost:  extParticipantGitHubCost,
		ParticipantContextCost: extParticipantContextCost,
		ParticipantTotalCost:   extParticipantCost,

		ParticipantReviewHours:  extParticipantReviewHours,
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
