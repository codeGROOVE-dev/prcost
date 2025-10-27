package cost

import "log/slog"

// ExtrapolatedBreakdown represents cost estimates extrapolated from a sample
// of PRs to estimate total costs across a larger population.
type ExtrapolatedBreakdown struct {
	// Sample metadata
	TotalPRs                   int     `json:"total_prs"`                       // Total number of PRs in the population
	HumanPRs                   int     `json:"human_prs"`                       // Number of human-authored PRs
	BotPRs                     int     `json:"bot_prs"`                         // Number of bot-authored PRs
	SampledPRs                 int     `json:"sampled_prs"`                     // Number of PRs successfully sampled
	SuccessfulSamples          int     `json:"successful_samples"`              // Number of samples that processed successfully
	UniqueAuthors              int     `json:"unique_authors"`                  // Number of unique PR authors (excluding bots) in sample
	TotalAuthors               int     `json:"total_authors"`                   // Total unique authors across all PRs (not just samples)
	WasteHoursPerWeek          float64 `json:"waste_hours_per_week"`            // Preventable hours wasted per week (organizational)
	WasteCostPerWeek           float64 `json:"waste_cost_per_week"`             // Preventable cost wasted per week (organizational)
	WasteHoursPerAuthorPerWeek float64 `json:"waste_hours_per_author_per_week"` // Preventable hours wasted per author per week
	WasteCostPerAuthorPerWeek  float64 `json:"waste_cost_per_author_per_week"`  // Preventable cost wasted per author per week
	AvgPRDurationHours         float64 `json:"avg_pr_duration_hours"`           // Average PR open time in hours (all PRs)
	AvgHumanPRDurationHours    float64 `json:"avg_human_pr_duration_hours"`     // Average human PR open time in hours
	AvgBotPRDurationHours      float64 `json:"avg_bot_pr_duration_hours"`       // Average bot PR open time in hours

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

	// Author activity metrics (extrapolated)
	AuthorEvents   int `json:"author_events"`   // Total GitHub events by authors
	AuthorSessions int `json:"author_sessions"` // Total GitHub work sessions by authors

	// LOC metrics (extrapolated totals)
	TotalNewLines      int `json:"total_new_lines"`      // Total net new lines across all PRs
	TotalModifiedLines int `json:"total_modified_lines"` // Total modified lines across all PRs
	BotNewLines        int `json:"bot_new_lines"`        // Total net new lines from bot PRs
	BotModifiedLines   int `json:"bot_modified_lines"`   // Total modified lines from bot PRs
	OpenPRs            int `json:"open_prs"`             // Number of currently open PRs

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

	// Participant activity metrics (extrapolated)
	ParticipantEvents   int `json:"participant_events"`   // Total GitHub events by participants
	ParticipantSessions int `json:"participant_sessions"` // Total GitHub work sessions by participants
	ParticipantReviews  int `json:"participant_reviews"`  // Total number of reviews performed

	// Delay costs (extrapolated)
	DeliveryDelayCost    float64 `json:"delivery_delay_cost"`
	CodeChurnCost        float64 `json:"code_churn_cost"`
	AutomatedUpdatesCost float64 `json:"automated_updates_cost"`
	PRTrackingCost       float64 `json:"pr_tracking_cost"`
	FutureReviewCost     float64 `json:"future_review_cost"`
	FutureMergeCost      float64 `json:"future_merge_cost"`
	FutureContextCost    float64 `json:"future_context_cost"`
	DelayTotalCost       float64 `json:"delay_total_cost"`

	// Delay hours (extrapolated)
	DeliveryDelayHours    float64 `json:"delivery_delay_hours"`
	CodeChurnHours        float64 `json:"code_churn_hours"`
	AutomatedUpdatesHours float64 `json:"automated_updates_hours"`
	PRTrackingHours       float64 `json:"pr_tracking_hours"`
	FutureReviewHours     float64 `json:"future_review_hours"`
	FutureMergeHours      float64 `json:"future_merge_hours"`
	FutureContextHours    float64 `json:"future_context_hours"`
	DelayTotalHours       float64 `json:"delay_total_hours"`

	// Counts for future costs (extrapolated)
	CodeChurnPRCount      int     `json:"code_churn_pr_count"`     // Number of PRs with code churn
	FutureReviewPRCount   int     `json:"future_review_pr_count"`  // Number of PRs with future review costs
	FutureMergePRCount    int     `json:"future_merge_pr_count"`   // Number of PRs with future merge costs
	FutureContextSessions int     `json:"future_context_sessions"` // Estimated future context switching sessions
	AvgReworkPercentage   float64 `json:"avg_rework_percentage"`   // Average code drift/rework percentage

	// Grand totals
	TotalCost  float64 `json:"total_cost"`
	TotalHours float64 `json:"total_hours"`

	// R2R cost savings calculation
	UniqueNonBotUsers int     `json:"unique_non_bot_users"` // Count of unique non-bot users (authors + participants)
	R2RSavings        float64 `json:"r2r_savings"`          // Annual savings if R2R cuts PR time to 40 minutes
}

// ExtrapolateFromSamples calculates extrapolated cost estimates from a sample
// of PR breakdowns to estimate costs across a larger population.
//
// Parameters:
//   - breakdowns: Slice of Breakdown structs from successfully processed samples
//   - totalPRs: Total number of PRs in the population
//   - totalAuthors: Total number of unique authors across all PRs (not just samples)
//   - daysInPeriod: Number of days the sample covers (for per-week calculations)
//   - cfg: Configuration for hourly rate and hours per week calculation
//
// Returns:
//   - ExtrapolatedBreakdown with averaged costs scaled to total population
//
// The function computes the average cost per PR from the samples, then multiplies
// by the total PR count to estimate population-wide costs.
func ExtrapolateFromSamples(breakdowns []Breakdown, totalPRs, totalAuthors, actualOpenPRs int, daysInPeriod int, cfg Config) ExtrapolatedBreakdown {
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
	// Track unique non-bot users (authors + participants)
	uniqueNonBotUsers := make(map[string]bool)

	// Track bot vs human PR metrics
	var humanPRCount, botPRCount int
	var sumHumanPRDuration, sumBotPRDuration float64

	// Accumulate costs from all samples
	var sumAuthorNewCodeCost, sumAuthorAdaptationCost, sumAuthorGitHubCost, sumAuthorGitHubContextCost float64
	var sumAuthorNewCodeHours, sumAuthorAdaptationHours, sumAuthorGitHubHours, sumAuthorGitHubContextHours float64
	var sumParticipantReviewCost, sumParticipantGitHubCost, sumParticipantContextCost, sumParticipantCost float64
	var sumParticipantReviewHours, sumParticipantGitHubHours, sumParticipantContextHours, sumParticipantHours float64
	var sumDeliveryDelayCost, sumCodeChurnCost, sumAutomatedUpdatesCost, sumPRTrackingCost float64
	var sumFutureReviewCost, sumFutureMergeCost, sumFutureContextCost, sumDelayCost float64
	var sumDeliveryDelayHours, sumCodeChurnHours, sumAutomatedUpdatesHours, sumPRTrackingHours float64
	var sumFutureReviewHours, sumFutureMergeHours, sumFutureContextHours, sumDelayHours float64
	var sumAuthorHours float64
	var sumTotalCost float64
	var sumPRDuration float64
	var sumNewLines, sumModifiedLines int
	var sumBotNewLines, sumBotModifiedLines int
	var sumAuthorEvents, sumAuthorSessions int
	var sumParticipantEvents, sumParticipantSessions, sumParticipantReviews int
	var sumFutureContextSessions int
	var sumReworkPercentage float64
	var countCodeChurn, countFutureReview, countFutureMerge int

	for i := range breakdowns {
		breakdown := &breakdowns[i]

		// Track unique PR authors only (excluding bots)
		if !breakdown.AuthorBot {
			uniqueAuthors[breakdown.PRAuthor] = true
			uniqueNonBotUsers[breakdown.PRAuthor] = true
			humanPRCount++
			sumHumanPRDuration += breakdown.PRDuration
		} else {
			botPRCount++
			sumBotPRDuration += breakdown.PRDuration
			// Track bot PR LOC separately
			sumBotNewLines += breakdown.Author.NewLines
			sumBotModifiedLines += breakdown.Author.ModifiedLines
		}

		// Track unique participants (excluding bots)
		for _, p := range breakdown.Participants {
			// Participants from the Breakdown struct are already filtered to exclude bots
			uniqueNonBotUsers[p.Actor] = true
		}

		// Accumulate PR duration (all PRs)
		sumPRDuration += breakdown.PRDuration

		// Accumulate LOC metrics (all PRs)
		sumNewLines += breakdown.Author.NewLines
		sumModifiedLines += breakdown.Author.ModifiedLines

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
		sumAuthorEvents += breakdown.Author.Events
		sumAuthorSessions += breakdown.Author.Sessions

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
			sumParticipantEvents += p.Events
			sumParticipantSessions += p.Sessions
			if p.ReviewCost > 0 {
				sumParticipantReviews++ // Count reviewers (participants who performed reviews)
			}
		}

		// Accumulate delay costs
		sumDeliveryDelayCost += breakdown.DelayCostDetail.DeliveryDelayCost
		sumCodeChurnCost += breakdown.DelayCostDetail.CodeChurnCost
		sumAutomatedUpdatesCost += breakdown.DelayCostDetail.AutomatedUpdatesCost
		sumPRTrackingCost += breakdown.DelayCostDetail.PRTrackingCost
		sumFutureReviewCost += breakdown.DelayCostDetail.FutureReviewCost
		sumFutureMergeCost += breakdown.DelayCostDetail.FutureMergeCost
		sumFutureContextCost += breakdown.DelayCostDetail.FutureContextCost

		// Count PRs with each future cost type and accumulate rework percentage
		if breakdown.DelayCostDetail.CodeChurnCost > 0.01 {
			countCodeChurn++
			sumReworkPercentage += breakdown.DelayCostDetail.ReworkPercentage
		}
		if breakdown.DelayCostDetail.FutureReviewCost > 0.01 {
			countFutureReview++
		}
		if breakdown.DelayCostDetail.FutureMergeCost > 0.01 {
			countFutureMerge++
		}
		if breakdown.DelayCostDetail.FutureContextCost > 0.01 {
			// Future context cost assumes 3 sessions per open PR (review request, review, merge)
			sumFutureContextSessions += 3
		}
		sumDeliveryDelayHours += breakdown.DelayCostDetail.DeliveryDelayHours
		sumCodeChurnHours += breakdown.DelayCostDetail.CodeChurnHours
		sumAutomatedUpdatesHours += breakdown.DelayCostDetail.AutomatedUpdatesHours
		sumPRTrackingHours += breakdown.DelayCostDetail.PRTrackingHours
		sumFutureReviewHours += breakdown.DelayCostDetail.FutureReviewHours
		sumFutureMergeHours += breakdown.DelayCostDetail.FutureMergeHours
		sumFutureContextHours += breakdown.DelayCostDetail.FutureContextHours
		sumDelayCost += breakdown.DelayCost
		sumDelayHours += breakdown.DelayCostDetail.TotalDelayHours

		sumTotalCost += breakdown.TotalCost
	}

	// Calculate averages and extrapolate to total PRs
	samples := float64(successfulSamples)

	// Extrapolate LOC metrics
	extTotalNewLines := int(float64(sumNewLines) / samples * multiplier)
	extTotalModifiedLines := int(float64(sumModifiedLines) / samples * multiplier)
	extBotNewLines := int(float64(sumBotNewLines) / samples * multiplier)
	extBotModifiedLines := int(float64(sumBotModifiedLines) / samples * multiplier)

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
	extAuthorEvents := int(float64(sumAuthorEvents) / samples * multiplier)
	extAuthorSessions := int(float64(sumAuthorSessions) / samples * multiplier)

	extParticipantReviewCost := sumParticipantReviewCost / samples * multiplier
	extParticipantGitHubCost := sumParticipantGitHubCost / samples * multiplier
	extParticipantContextCost := sumParticipantContextCost / samples * multiplier
	extParticipantCost := sumParticipantCost / samples * multiplier
	extParticipantReviewHours := sumParticipantReviewHours / samples * multiplier
	extParticipantGitHubHours := sumParticipantGitHubHours / samples * multiplier
	extParticipantContextHours := sumParticipantContextHours / samples * multiplier
	extParticipantHours := sumParticipantHours / samples * multiplier
	extParticipantEvents := int(float64(sumParticipantEvents) / samples * multiplier)
	extParticipantSessions := int(float64(sumParticipantSessions) / samples * multiplier)
	extParticipantReviews := int(float64(sumParticipantReviews) / samples * multiplier)

	extDeliveryDelayCost := sumDeliveryDelayCost / samples * multiplier
	extCodeChurnCost := sumCodeChurnCost / samples * multiplier
	extAutomatedUpdatesCost := sumAutomatedUpdatesCost / samples * multiplier
	// Calculate Open PR Tracking cost based on actual open PRs (not from samples)
	// Formula: actualOpenPRs × (tracking_minutes_per_day / 60) × daysInPeriod × hourlyRate
	hourlyRate := cfg.AnnualSalary * cfg.BenefitsMultiplier / cfg.HoursPerYear
	extPRTrackingHours := float64(actualOpenPRs) * (cfg.PRTrackingMinutesPerDay / 60.0) * float64(daysInPeriod)
	extPRTrackingCost := extPRTrackingHours * hourlyRate
	extFutureReviewCost := sumFutureReviewCost / samples * multiplier
	extFutureMergeCost := sumFutureMergeCost / samples * multiplier
	extFutureContextCost := sumFutureContextCost / samples * multiplier
	extDeliveryDelayHours := sumDeliveryDelayHours / samples * multiplier
	extCodeChurnHours := sumCodeChurnHours / samples * multiplier
	extAutomatedUpdatesHours := sumAutomatedUpdatesHours / samples * multiplier
	extFutureReviewHours := sumFutureReviewHours / samples * multiplier
	extFutureMergeHours := sumFutureMergeHours / samples * multiplier
	extFutureContextHours := sumFutureContextHours / samples * multiplier
	extDelayTotal := sumDelayCost / samples * multiplier
	extDelayHours := sumDelayHours / samples * multiplier

	// Extrapolate future cost counts
	extCodeChurnPRCount := int(float64(countCodeChurn) / samples * multiplier)
	extFutureReviewPRCount := int(float64(countFutureReview) / samples * multiplier)
	extFutureMergePRCount := int(float64(countFutureMerge) / samples * multiplier)
	extFutureContextSessions := int(float64(sumFutureContextSessions) / samples * multiplier)
	// Use actual open PR count from repository query, not extrapolated from sample
	extOpenPRs := actualOpenPRs

	// Calculate average rework percentage (only for PRs with code churn)
	var avgReworkPercentage float64
	if countCodeChurn > 0 {
		avgReworkPercentage = sumReworkPercentage / float64(countCodeChurn)
	}

	extTotalCost := sumTotalCost / samples * multiplier
	extTotalHours := extAuthorHours + extParticipantHours + extDelayHours

	// Calculate waste per week metrics
	var wasteHoursPerWeek, wasteCostPerWeek float64
	var wasteHoursPerAuthorPerWeek, wasteCostPerAuthorPerWeek float64
	authorCount := len(uniqueAuthors)
	if daysInPeriod > 0 {
		// Preventable hours = code churn + delivery delay + automated updates + PR tracking
		preventableHours := extCodeChurnHours + extDeliveryDelayHours + extAutomatedUpdatesHours + extPRTrackingHours
		preventableCost := extCodeChurnCost + extDeliveryDelayCost + extAutomatedUpdatesCost + extPRTrackingCost

		// Calculate weeks in the period
		weeksInPeriod := float64(daysInPeriod) / 7.0

		// Wasted overhead per week (organizational)
		wasteHoursPerWeek = preventableHours / weeksInPeriod
		wasteCostPerWeek = preventableCost / weeksInPeriod

		// Wasted overhead per author per week
		if totalAuthors > 0 {
			wasteHoursPerAuthorPerWeek = wasteHoursPerWeek / float64(totalAuthors)
			wasteCostPerAuthorPerWeek = wasteCostPerWeek / float64(totalAuthors)
		}

		// Debug logging
		slog.Info("Waste per week calculation",
			"total_preventable_hours", preventableHours,
			"total_preventable_cost", preventableCost,
			"code_churn_hours", extCodeChurnHours,
			"delivery_delay_hours", extDeliveryDelayHours,
			"days_in_period", daysInPeriod,
			"weeks_in_period", weeksInPeriod,
			"waste_hours_per_week", wasteHoursPerWeek,
			"waste_cost_per_week", wasteCostPerWeek,
			"total_authors", totalAuthors,
			"waste_hours_per_author_per_week", wasteHoursPerAuthorPerWeek,
			"waste_cost_per_author_per_week", wasteCostPerAuthorPerWeek)
	}

	// Calculate average PR durations
	avgPRDuration := sumPRDuration / samples
	var avgHumanPRDuration, avgBotPRDuration float64
	if humanPRCount > 0 {
		avgHumanPRDuration = sumHumanPRDuration / float64(humanPRCount)
	}
	if botPRCount > 0 {
		avgBotPRDuration = sumBotPRDuration / float64(botPRCount)
	}

	// Extrapolate bot vs human PR counts
	extHumanPRs := int(float64(humanPRCount) / samples * multiplier)
	extBotPRs := int(float64(botPRCount) / samples * multiplier)

	// Calculate R2R savings
	// Formula: baseline annual waste - (re-modeled waste with 40min PRs) - (R2R subscription cost)
	// Baseline annual waste: preventable cost extrapolated to 52 weeks
	uniqueUserCount := len(uniqueNonBotUsers)
	baselineAnnualWaste := (extCodeChurnCost + extDeliveryDelayCost + extAutomatedUpdatesCost + extPRTrackingCost) * (52.0 / (float64(daysInPeriod) / 7.0))

	// Re-model with 40-minute PR merge times
	// We need to recalculate delivery delay and future costs assuming all PRs take 40 minutes (2/3 hour)
	const targetMergeTimeHours = 40.0 / 60.0 // 40 minutes in hours

	// Recalculate delivery delay cost with 40-minute PRs
	// Delivery delay formula: hourlyRate × deliveryDelayFactor × PR duration
	var remodelDeliveryDelayCost float64
	for range breakdowns {
		remodelDeliveryDelayCost += hourlyRate * cfg.DeliveryDelayFactor * targetMergeTimeHours
	}
	extRemodelDeliveryDelayCost := remodelDeliveryDelayCost / samples * multiplier

	// Recalculate code churn with 40-minute PRs
	// Code churn is proportional to PR duration (rework percentage increases with time)
	// For 40 minutes, rework percentage would be minimal (< 1 day, so ~0%)
	extRemodelCodeChurnCost := 0.0 // 40 minutes is too short for meaningful code churn

	// Recalculate automated updates cost
	// Automated updates are calculated based on PR duration
	// With 40-minute PRs, no bot updates would be needed (happens after 1 day)
	extRemodelAutomatedUpdatesCost := 0.0 // 40 minutes is too short for automated updates

	// Recalculate PR tracking cost
	// With faster merge times, we'd have fewer open PRs at any given time
	// Estimate: if current avg is X hours, and we reduce to 40 min, open PRs would be (40min / X hours) of current
	var extRemodelPRTrackingCost float64
	var currentAvgOpenTime float64
	if successfulSamples > 0 {
		currentAvgOpenTime = sumPRDuration / samples
	}
	if currentAvgOpenTime > 0 {
		openPRReductionRatio := targetMergeTimeHours / currentAvgOpenTime
		extRemodelPRTrackingCost = extPRTrackingCost * openPRReductionRatio
	} else {
		extRemodelPRTrackingCost = 0.0
	}

	// Calculate re-modeled annual waste
	remodelPreventablePerPeriod := extRemodelDeliveryDelayCost + extRemodelCodeChurnCost + extRemodelAutomatedUpdatesCost + extRemodelPRTrackingCost
	remodelAnnualWaste := remodelPreventablePerPeriod * (52.0 / (float64(daysInPeriod) / 7.0))

	// Subtract R2R subscription cost: $4/mo * 12 months * unique user count
	r2rAnnualCost := 4.0 * 12.0 * float64(uniqueUserCount)

	// Calculate savings
	r2rSavings := baselineAnnualWaste - remodelAnnualWaste - r2rAnnualCost
	if r2rSavings < 0 {
		r2rSavings = 0 // Don't show negative savings
	}

	return ExtrapolatedBreakdown{
		TotalPRs:                   totalPRs,
		HumanPRs:                   extHumanPRs,
		BotPRs:                     extBotPRs,
		SampledPRs:                 successfulSamples,
		SuccessfulSamples:          successfulSamples,
		UniqueAuthors:              authorCount,
		TotalAuthors:               totalAuthors,
		WasteHoursPerWeek:          wasteHoursPerWeek,
		WasteCostPerWeek:           wasteCostPerWeek,
		WasteHoursPerAuthorPerWeek: wasteHoursPerAuthorPerWeek,
		WasteCostPerAuthorPerWeek:  wasteCostPerAuthorPerWeek,
		AvgPRDurationHours:         avgPRDuration,
		AvgHumanPRDurationHours:    avgHumanPRDuration,
		AvgBotPRDurationHours:      avgBotPRDuration,

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

		AuthorEvents:   extAuthorEvents,
		AuthorSessions: extAuthorSessions,

		TotalNewLines:      extTotalNewLines,
		TotalModifiedLines: extTotalModifiedLines,
		BotNewLines:        extBotNewLines,
		BotModifiedLines:   extBotModifiedLines,
		OpenPRs:            extOpenPRs,

		ParticipantReviewCost:  extParticipantReviewCost,
		ParticipantGitHubCost:  extParticipantGitHubCost,
		ParticipantContextCost: extParticipantContextCost,
		ParticipantTotalCost:   extParticipantCost,

		ParticipantReviewHours:  extParticipantReviewHours,
		ParticipantGitHubHours:  extParticipantGitHubHours,
		ParticipantContextHours: extParticipantContextHours,
		ParticipantTotalHours:   extParticipantHours,

		ParticipantEvents:   extParticipantEvents,
		ParticipantSessions: extParticipantSessions,
		ParticipantReviews:  extParticipantReviews,

		DeliveryDelayCost:    extDeliveryDelayCost,
		CodeChurnCost:        extCodeChurnCost,
		AutomatedUpdatesCost: extAutomatedUpdatesCost,
		PRTrackingCost:       extPRTrackingCost,
		FutureReviewCost:     extFutureReviewCost,
		FutureMergeCost:      extFutureMergeCost,
		FutureContextCost:    extFutureContextCost,
		DelayTotalCost:       extDelayTotal,

		DeliveryDelayHours:    extDeliveryDelayHours,
		CodeChurnHours:        extCodeChurnHours,
		AutomatedUpdatesHours: extAutomatedUpdatesHours,
		PRTrackingHours:       extPRTrackingHours,
		FutureReviewHours:     extFutureReviewHours,
		FutureMergeHours:      extFutureMergeHours,
		FutureContextHours:    extFutureContextHours,
		DelayTotalHours:       extDelayHours,

		CodeChurnPRCount:      extCodeChurnPRCount,
		FutureReviewPRCount:   extFutureReviewPRCount,
		FutureMergePRCount:    extFutureMergePRCount,
		FutureContextSessions: extFutureContextSessions,
		AvgReworkPercentage:   avgReworkPercentage,

		TotalCost:  extTotalCost,
		TotalHours: extTotalHours,

		UniqueNonBotUsers: uniqueUserCount,
		R2RSavings:        r2rSavings,
	}
}
