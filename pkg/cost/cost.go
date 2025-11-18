// Package cost calculates the real-world cost of GitHub pull requests.
// Costs are broken down into detailed components with itemized inputs.
package cost

import (
	"cmp"
	"log/slog"
	"math"
	"slices"
	"time"

	"github.com/codeGROOVE-dev/prcost/pkg/cocomo"
)

// Config holds all tunable parameters for cost calculation.
type Config struct {
	// Annual salary used for calculating hourly rate (default: $249,000)
	// Source: Average Staff Software Engineer salary, 2025 Glassdoor
	// https://www.glassdoor.com/Salaries/staff-software-engineer-salary-SRCH_KO0,23.htm
	AnnualSalary float64

	// Benefits multiplier applied to salary (default: 1.3 = 30% benefits)
	BenefitsMultiplier float64

	// Hours per year for calculating hourly rate (default: 2080)
	HoursPerYear float64

	// Time per GitHub event (default: 10 minutes)
	EventDuration time.Duration

	// Time for context switching in - starting a new session (default: 3 minutes)
	// Source: Microsoft Research - Iqbal & Horvitz (2007)
	// "Disruption and Recovery of Computing Tasks: Field Study, Analysis, and Directions"
	// https://erichorvitz.com/CHI_2007_Iqbal_Horvitz.pdf
	ContextSwitchInDuration time.Duration

	// Time for context switching out - ending a session (default: 16 minutes 33 seconds)
	// Source: Microsoft Research - Iqbal & Horvitz (2007)
	// "Disruption and Recovery of Computing Tasks: Field Study, Analysis, and Directions"
	// https://erichorvitz.com/CHI_2007_Iqbal_Horvitz.pdf
	ContextSwitchOutDuration time.Duration

	// Session gap threshold (default: 20 minutes)
	// Events within this gap are considered part of the same session
	SessionGapThreshold time.Duration

	// Delivery delay factor as percentage of hourly rate (default: 0.15 = 15%)
	// Represents opportunity cost of blocked value delivery
	DeliveryDelayFactor float64

	// Automated updates factor for bot-authored PRs (default: 0.01 = 1%)
	// Represents overhead of tracking automated dependency updates and bot-driven changes
	AutomatedUpdatesFactor float64

	// PR tracking cost in minutes per day (default: 1.0 minute per day)
	// Applied to PRs open >24 hours to represent ongoing triage/tracking overhead
	PRTrackingMinutesPerDay float64

	// Maximum time after last event to count for project delay (default: 14 days / 2 weeks)
	// Only counts delay costs up to this many days after the last event on the PR
	MaxDelayAfterLastEvent time.Duration

	// Maximum total project delay duration (default: 90 days / 3 months)
	// Absolute cap on project delay costs regardless of PR age
	MaxProjectDelay time.Duration

	// Maximum duration for code drift calculation (default: 90 days / 3 months)
	// Code drift is capped at this duration (affects rework percentage)
	MaxCodeDrift time.Duration

	// Code review inspection rate in lines per hour (default: 275 LOC/hour)
	// Based on IEEE/Fagan inspection research showing optimal rates of 150-400 LOC/hour
	// - Fagan inspection (thorough): ~22 LOC/hour
	// - Industry standard: 150-200 LOC/hour
	// - Fast/lightweight: up to 400 LOC/hour
	// - Average: 275 LOC/hour (midpoint of optimal range)
	// Used for both past and future review time estimates
	// Formula: review_hours = LOC / inspection_rate
	ReviewInspectionRate float64

	// ModificationCostFactor is the cost multiplier for modified code vs new code (default: 0.4)
	// Based on COCOMO II research showing that modifying existing code is cheaper than writing new code.
	// - New code: 1.0x (full cost)
	// - Modified code: 0.2-0.4x (20-40% of new code cost)
	// Default of 0.4 (40%) represents the upper end of the typical range.
	// Modification is cheaper because architecture is established and patterns are known.
	ModificationCostFactor float64

	// WeeklyChurnRate is the probability that code becomes stale per week (default: 0.0229 = 2.29%)
	// Used to calculate rework percentage for open PRs based on time since last commit.
	// Formula: rework = 1 - (1 - weekly_rate)^weeks
	//
	// Default of 2.29% per week is based on empirical analysis across organizations:
	// - 60% of analyzed organizations had churn rates of 2.29%/week or lower
	// - 40% had higher churn rates
	// - Younger companies tend to have higher churn rates
	// - Results in 70% annual churn, reasonable for active development
	//
	// Examples from empirical data:
	// - 0.0018 (0.18%/week) - Adobe (mature, stable codebase)
	// - 0.0229 (2.29%/week) - 60th percentile (default)
	// - 0.0831 (8.31%/week) - Chainguard (young company, fast-moving)
	//
	// Recommended values for different project types:
	// - 0.010 (1.0%/week) → 41% annual churn - stable projects, mature codebases
	// - 0.0229 (2.29%/week) → 70% annual churn - typical active development (default, 60th percentile)
	// - 0.030 (3.0%/week) → 78% annual churn - fast-moving projects
	// - 0.040 (4.0%/week) → 88% annual churn - very high churn
	// - 0.080+ (8%+/week) → 99%+ annual churn - extremely fast-moving, younger companies
	WeeklyChurnRate float64

	// TargetMergeTimeHours is the target merge time in hours for efficiency modeling (default: 1.5 hours / 90 minutes)
	// Used to calculate potential savings if merge times were reduced to this target.
	// This represents a realistic goal for well-optimized PR workflows.
	TargetMergeTimeHours float64

	// COCOMO configuration for estimating code writing effort
	COCOMO cocomo.Config
}

// DefaultConfig returns reasonable defaults for cost calculation.
func DefaultConfig() Config {
	return Config{
		AnnualSalary:             249000.0,                        // Average Staff Software Engineer salary (2025, Glassdoor)
		BenefitsMultiplier:       1.3,                             // 30% benefits overhead
		HoursPerYear:             2080.0,                          // Standard full-time hours
		EventDuration:            10 * time.Minute,                // 10 minutes per GitHub event
		ContextSwitchInDuration:  3 * time.Minute,                 // 3 min to context switch in (Microsoft Research)
		ContextSwitchOutDuration: 16*time.Minute + 33*time.Second, // 16m33s to context switch out (Microsoft Research)
		SessionGapThreshold:      20 * time.Minute,                // Events within 20 min are same session
		DeliveryDelayFactor:      0.20,                            // 20% opportunity cost
		AutomatedUpdatesFactor:   0.01,                            // 1% overhead for bot PRs
		PRTrackingMinutesPerDay:  10.0 / 60.0,                     // 10 seconds/person/day per open PR
		MaxDelayAfterLastEvent:   14 * 24 * time.Hour,             // 14 days (2 weeks) after last event
		MaxProjectDelay:          90 * 24 * time.Hour,             // 90 days absolute max
		MaxCodeDrift:             90 * 24 * time.Hour,             // 90 days
		ReviewInspectionRate:     275.0,                           // 275 LOC/hour (average of optimal 150-400 range)
		ModificationCostFactor:   0.4,                             // Modified code costs 40% of new code
		WeeklyChurnRate:          0.0229,                          // 2.29% per week (70% annual, 60th percentile empirical)
		TargetMergeTimeHours:     1.5,                             // 1.5 hours (90 minutes) target for efficiency modeling
		COCOMO:                   cocomo.DefaultConfig(),
	}
}

// ParticipantEvent represents a single event by a participant.
type ParticipantEvent struct {
	Timestamp time.Time
	Actor     string
	Kind      string // Event type: "commit", "review", "comment", etc.
}

// PRData contains all information needed to calculate PR costs.
type PRData struct {
	CreatedAt    time.Time
	ClosedAt     time.Time
	Author       string
	Events       []ParticipantEvent
	LinesAdded   int
	LinesDeleted int
	AuthorBot    bool
}

// AuthorCostDetail breaks down the author's costs.
type AuthorCostDetail struct {
	NewCodeCost       float64 `json:"new_code_cost"`       // COCOMO cost for new development (net new lines)
	AdaptationCost    float64 `json:"adaptation_cost"`     // COCOMO cost for code adaptation (modified lines)
	GitHubCost        float64 `json:"github_cost"`         // Cost of GitHub interactions (commits, comments, etc.)
	GitHubContextCost float64 `json:"github_context_cost"` // Cost of context switching for GitHub sessions

	// Supporting details
	NewLines           int     `json:"new_lines"`            // Net new lines of code
	ModifiedLines      int     `json:"modified_lines"`       // Lines modified from existing code
	LinesAdded         int     `json:"lines_added"`          // Total lines added (new + modified)
	Events             int     `json:"events"`               // Number of author events
	Sessions           int     `json:"sessions"`             // Number of GitHub work sessions
	NewCodeHours       float64 `json:"new_code_hours"`       // Hours for new development (COCOMO)
	AdaptationHours    float64 `json:"adaptation_hours"`     // Hours for code adaptation (COCOMO)
	GitHubHours        float64 `json:"github_hours"`         // Hours spent on GitHub interactions
	GitHubContextHours float64 `json:"github_context_hours"` // Hours spent context switching for GitHub
	TotalHours         float64 `json:"total_hours"`          // Total hours (sum of above)
	TotalCost          float64 `json:"total_cost"`           // Total author cost
}

// ParticipantCostDetail breaks down a participant's costs.
type ParticipantCostDetail struct {
	Actor             string  `json:"actor"`               // Participant username
	ReviewCost        float64 `json:"review_cost"`         // Cost of code review (LOC-based, once per reviewer)
	GitHubCost        float64 `json:"github_cost"`         // Cost of other GitHub events (non-review)
	GitHubContextCost float64 `json:"github_context_cost"` // Cost of context switching for GitHub sessions

	// Supporting details
	Events             int     `json:"events"`               // Number of participant events
	Sessions           int     `json:"sessions"`             // Number of GitHub work sessions
	ReviewHours        float64 `json:"review_hours"`         // Hours spent reviewing code (LOC-based)
	GitHubHours        float64 `json:"github_hours"`         // Hours spent on other GitHub events
	GitHubContextHours float64 `json:"github_context_hours"` // Hours spent context switching for GitHub
	TotalHours         float64 `json:"total_hours"`          // Total hours (sum of above)
	TotalCost          float64 `json:"total_cost"`           // Total participant cost
}

// DelayCostDetail holds itemized delay costs.
type DelayCostDetail struct {
	DeliveryDelayCost    float64 `json:"delivery_delay_cost"`    // Opportunity cost - blocked value delivery (15% factor)
	CodeChurnCost        float64 `json:"code_churn_cost"`        // COCOMO cost for rework/merge conflicts
	AutomatedUpdatesCost float64 `json:"automated_updates_cost"` // Overhead for bot-authored PRs (1% factor)
	PRTrackingCost       float64 `json:"pr_tracking_cost"`       // Daily tracking cost for PRs open >24 hours (1 min/day)

	// Future costs (estimated for open PRs) - split across 2 people
	FutureReviewCost  float64 `json:"future_review_cost"`  // Cost for future review events (2 events × 20 min)
	FutureMergeCost   float64 `json:"future_merge_cost"`   // Cost for future merge event (1 event × 20 min)
	FutureContextCost float64 `json:"future_context_cost"` // Cost for future context switching (3 events × 40 min)

	// Supporting details
	DeliveryDelayHours    float64 `json:"delivery_delay_hours"`    // Hours of delivery delay
	CodeChurnHours        float64 `json:"code_churn_hours"`        // Hours for code churn
	AutomatedUpdatesHours float64 `json:"automated_updates_hours"` // Hours of automated update tracking
	PRTrackingHours       float64 `json:"pr_tracking_hours"`       // Hours of PR tracking (for PRs open >24 hours)
	FutureReviewHours     float64 `json:"future_review_hours"`     // Hours for future review events
	FutureMergeHours      float64 `json:"future_merge_hours"`      // Hours for future merge event
	FutureContextHours    float64 `json:"future_context_hours"`    // Hours for future context switching
	ReworkPercentage      float64 `json:"rework_percentage"`       // Percentage of code requiring rework (1%-41%)
	TotalDelayCost        float64 `json:"total_delay_cost"`        // Total delay cost (sum of above)
	TotalDelayHours       float64 `json:"total_delay_hours"`       // Total delay hours
}

// Breakdown shows fully itemized costs for a pull request.
type Breakdown struct {
	PRAuthor           string                  `json:"pr_author"`
	Participants       []ParticipantCostDetail `json:"participants"`
	Author             AuthorCostDetail        `json:"author"`
	DelayCostDetail    DelayCostDetail         `json:"delay_cost_detail"`
	AnnualSalary       float64                 `json:"annual_salary"`
	HourlyRate         float64                 `json:"hourly_rate"`
	DelayHours         float64                 `json:"delay_hours"`
	BenefitsMultiplier float64                 `json:"benefits_multiplier"`
	DelayCost          float64                 `json:"delay_cost"`
	PRDuration         float64                 `json:"pr_duration"`
	TotalCost          float64                 `json:"total_cost"`
	AuthorBot          bool                    `json:"author_bot"`
	DelayCapped        bool                    `json:"delay_capped"`
}

// Calculate computes the total cost of a pull request with detailed breakdowns.
//
//nolint:revive,maintidx // function-length/complexity: acceptable for core business logic
func Calculate(data PRData, cfg Config) Breakdown {
	// Defensive check: avoid division by zero
	if cfg.HoursPerYear == 0 {
		cfg.HoursPerYear = 2080 // Standard full-time hours per year
	}
	hourlyRate := (cfg.AnnualSalary * cfg.BenefitsMultiplier) / cfg.HoursPerYear

	// Calculate author costs
	authorCost := calculateAuthorCost(data, cfg, hourlyRate)

	// Calculate participant costs (everyone except author)
	participantCosts := calculateParticipantCosts(data, cfg, hourlyRate)

	// Calculate delay cost with itemized breakdown (always shown)
	// Use ClosedAt if PR is closed, otherwise use current time
	endTime := time.Now()
	if !data.ClosedAt.IsZero() {
		endTime = data.ClosedAt
	}
	delayHours := endTime.Sub(data.CreatedAt).Hours()
	// Defensive check: if endTime is before CreatedAt (bad data), treat as zero delay
	if delayHours < 0 {
		delayHours = 0
	}
	delayDays := delayHours / 24.0

	// Find the last event timestamp to determine time since last activity
	var lastEventTime time.Time
	if len(data.Events) > 0 {
		// Find the most recent event
		lastEventTime = data.Events[0].Timestamp
		for _, event := range data.Events {
			if event.Timestamp.After(lastEventTime) {
				lastEventTime = event.Timestamp
			}
		}
	} else {
		// No events, use CreatedAt
		lastEventTime = data.CreatedAt
	}

	// Calculate time since last event (using endTime)
	timeSinceLastEvent := endTime.Sub(lastEventTime).Hours()
	if timeSinceLastEvent < 0 {
		timeSinceLastEvent = 0
	}

	// Log delay calculation details
	slog.Info("Delay calculation",
		"pr_created_at", data.CreatedAt.Format(time.RFC3339),
		"pr_closed_at", data.ClosedAt.Format(time.RFC3339),
		"calculation_time", endTime.Format(time.RFC3339),
		"last_event_time", lastEventTime.Format(time.RFC3339),
		"total_delay_hours", delayHours,
		"total_delay_days", delayDays,
		"hours_since_last_event", timeSinceLastEvent,
		"days_since_last_event", timeSinceLastEvent/24.0)

	// Cap Project Delay in three ways:
	// 1. Minimum threshold: PRs open < 30 minutes have no delay cost (fast turnaround)
	// 2. Only count up to MaxDelayAfterLastEvent (default: 14 days) after the last event
	// 3. Absolute maximum of MaxProjectDelay (default: 90 days) total
	var capped bool
	var cappedHrs float64

	cappedHrs = delayHours

	// First, apply minimum threshold: no delay costs for PRs open < 30 minutes
	// Rationale: PRs merged within 30 minutes have no meaningful delay or coordination overhead
	const minDelayThreshold = 0.5 // 30 minutes in hours
	if cappedHrs < minDelayThreshold {
		cappedHrs = 0
		slog.Info("Applied delay minimum threshold - no delay costs for fast turnaround",
			"delay_hours", delayHours,
			"threshold_hours", minDelayThreshold)
	}

	// Second, apply the "2 weeks after last event" cap
	maxAfterEvent := cfg.MaxDelayAfterLastEvent.Hours()
	if cappedHrs > 0 && timeSinceLastEvent > maxAfterEvent {
		// Reduce delay by the excess time since last event
		excessHours := timeSinceLastEvent - maxAfterEvent
		cappedHrs = delayHours - excessHours
		if cappedHrs < 0 {
			cappedHrs = 0
		}
		capped = true
		slog.Info("Applied delay cap: time since last event",
			"max_hours_after_event", maxAfterEvent,
			"actual_hours_since_event", timeSinceLastEvent,
			"excess_hours", excessHours,
			"capped_delay_hours", cappedHrs)
	}

	// Third, apply the absolute maximum cap
	maxTotal := cfg.MaxProjectDelay.Hours()
	if cappedHrs > maxTotal {
		beforeCap := cappedHrs
		cappedHrs = maxTotal
		capped = true
		slog.Info("Applied delay cap: absolute maximum",
			"max_total_hours", maxTotal,
			"delay_before_cap", beforeCap,
			"capped_delay_hours", cappedHrs)
	}

	// 1a. Delivery Delay: Opportunity cost of blocked value (default 15%)
	// The 15% represents the percentage of team capacity consumed by this blocked PR
	// Bot-authored PRs get 0% delivery delay (no human waiting)
	var deliveryDelayCost, deliveryDelayHours float64
	if !data.AuthorBot {
		deliveryDelayCost = hourlyRate * cappedHrs * cfg.DeliveryDelayFactor
		deliveryDelayHours = cappedHrs * cfg.DeliveryDelayFactor // Productivity-equivalent hours
		slog.Info("Delivery delay calculation",
			"pr_duration_hours", delayHours,
			"capped_hours", cappedHrs,
			"delay_factor", cfg.DeliveryDelayFactor,
			"delivery_delay_hours", deliveryDelayHours,
			"delivery_delay_cost", deliveryDelayCost)
	}

	// 1b. Automated Updates Overhead: Tracking overhead for bot PRs (default 1%)
	// The 1% represents the overhead of tracking automated dependency updates and bot-driven changes
	var automatedUpdatesCost, automatedUpdatesHours float64

	if data.AuthorBot {
		// Bot PRs: Use Automated Updates factor (default 1%)
		automatedUpdatesCost = hourlyRate * cappedHrs * cfg.AutomatedUpdatesFactor
		automatedUpdatesHours = cappedHrs * cfg.AutomatedUpdatesFactor
	}

	// 2. Code Churn (Rework): Probability-based drift formula
	// Only calculated for open PRs - closed PRs won't need future updates
	//
	// Formula: Probability that a line becomes stale over time
	//   drift = 1 - (1 - weeklyChurn)^(weeks)
	//   Default: weeklyChurn = 2.29% (0.0229) - empirical 60th percentile
	//
	// This models the cumulative probability that any given line in the PR needs rework
	// due to codebase changes. The weekly churn rate is configurable to match project velocity.
	//
	// Default (2.29% per week) drift percentages:
	// -  1 week: ~2.3% drift
	// -  2 weeks: ~4.5% drift
	// -  3 weeks: ~6.7% drift
	// -  1 month: ~8.9% drift
	// -  2 months: ~16.9% drift
	// -  3 months: ~24.3% drift
	// -  1 year: ~70% annual churn (empirical data from org analysis)

	var reworkLOC int
	var codeChurnHours float64
	var codeChurnCost float64
	var reworkPercentage float64

	isClosed := !data.ClosedAt.IsZero()

	// Find the most recent commit event from the author
	// Code churn is calculated from the last commit to now (only for open PRs)
	var lastAuthorCommitTime time.Time
	for _, event := range data.Events {
		if event.Actor == data.Author && event.Kind == "commit" {
			if lastAuthorCommitTime.IsZero() || event.Timestamp.After(lastAuthorCommitTime) {
				lastAuthorCommitTime = event.Timestamp
			}
		}
	}

	// Calculate drift days from last commit (not from PR creation)
	var driftDays float64
	if !lastAuthorCommitTime.IsZero() {
		driftHours := time.Since(lastAuthorCommitTime).Hours()
		if driftHours < 0 {
			driftHours = 0
		}
		driftDays = driftHours / 24.0

		slog.Info("Code churn calculation",
			"pr_closed", isClosed,
			"last_author_commit", lastAuthorCommitTime.Format(time.RFC3339),
			"drift_days", driftDays)
	} else if !isClosed {
		slog.Info("No author commits found for code churn calculation", "pr_closed", isClosed)
	}

	if !isClosed && driftDays >= 3.0 {
		// Cap days at configured maximum for drift calculation (default: 90 days)
		maxDriftDays := cfg.MaxCodeDrift.Hours() / 24.0
		cappedDriftDays := driftDays
		if cappedDriftDays > maxDriftDays {
			cappedDriftDays = maxDriftDays
		}

		// Probability-based drift using configurable weekly churn rate
		// Formula: rework = 1 - (1 - weekly_rate)^weeks
		// Default: 1% per week → 41% annual churn
		weeks := cappedDriftDays / 7.0
		reworkPercentage = 1.0 - math.Pow(1.0-cfg.WeeklyChurnRate, weeks)

		reworkLOC = int(float64(data.LinesAdded) * reworkPercentage)

		// Ensure minimum of 1 LOC for PRs >= 3 days since last commit
		if reworkLOC < 1 && driftDays >= 3.0 {
			reworkLOC = 1
			if data.LinesAdded > 0 {
				reworkPercentage = 1.0 / float64(data.LinesAdded)
			}
		}

		if reworkLOC > 0 {
			reworkEffort := cocomo.EstimateEffort(reworkLOC, cfg.COCOMO)
			codeChurnHours = reworkEffort.Hours()
			codeChurnCost = codeChurnHours * hourlyRate
			// Recalculate actual percentage for display
			if data.LinesAdded > 0 {
				reworkPercentage = float64(reworkLOC) / float64(data.LinesAdded)
			}
		}
	}

	// 3. Future GitHub time: split across 2 people (reviewer + author)
	// Only calculated for open PRs - closed PRs won't have future activity
	//
	// Research-based approach using IEEE/Fagan inspection rates:
	//
	// Review Time Calculation:
	// Based on empirical research showing code review inspection rates of 150-400 LOC/hour,
	// with 275 LOC/hour being the average (configurable via ReviewInspectionRate).
	//
	// References:
	// - Fagan, M. E. (1976). Design and Code Inspections to Reduce Errors in Program Development.
	//   IBM Systems Journal, 15(3), 182-211.
	// - IEEE Std 1028-2008: IEEE Standard for Software Reviews and Audits
	// - Empirical data: Optimal code review rates are 150-400 LOC/hour for effective defect detection
	//
	// Breakdown:
	// - Review: LOC / inspection_rate (e.g., 649 LOC / 275 LOC/hr = 2.4 hrs)
	// - Merge: 1 merge event × 20 min = 0.33 hrs (author performs merge)
	// - Context Switching: 2 sessions × (20 min in + 20 min out) = 1.33 hrs
	//   (1 session for reviewer, 1 session for author merge)
	//
	// Example for 649 LOC PR:
	// - Review: 2.4 hrs (size-dependent)
	// - Merge: 0.33 hrs (fixed)
	// - Context: 1.33 hrs (fixed for 2 sessions)
	// - Total: 4.1 hrs
	var futureReviewHours float64
	var futureReviewCost float64
	var futureMergeHours float64
	var futureMergeCost float64
	var futureContextHours float64
	var futureContextCost float64

	if !isClosed {
		// Review: Based on inspection rate (LOC / rate)
		// Defensive check: avoid division by zero
		if cfg.ReviewInspectionRate <= 0 {
			cfg.ReviewInspectionRate = 200.0 // Default to industry standard
		}
		futureReviewHours = float64(data.LinesAdded) / cfg.ReviewInspectionRate
		futureReviewCost = futureReviewHours * hourlyRate

		// Merge: 1 event × event duration
		futureMergeDuration := cfg.EventDuration
		futureMergeHours = futureMergeDuration.Hours()
		futureMergeCost = futureMergeHours * hourlyRate

		// Context Switching: 2 sessions × (context in + context out)
		// 1 session for reviewer, 1 session for author merge
		futureContextDuration := 2 * (cfg.ContextSwitchInDuration + cfg.ContextSwitchOutDuration)
		futureContextHours = futureContextDuration.Hours()
		futureContextCost = futureContextHours * hourlyRate
	}

	// 4. PR Tracking: Daily tracking cost for PRs open >24 hours (default: 1 minute/day)
	// Applied to PRs open >24 hours to represent ongoing triage/tracking overhead
	var prTrackingCost, prTrackingHours float64
	if !isClosed {
		daysOpen := delayHours / 24.0
		prTrackingHours = (cfg.PRTrackingMinutesPerDay / 60.0) * daysOpen
		prTrackingCost = prTrackingHours * hourlyRate
	}

	// Total delay cost
	futureTotalCost := futureReviewCost + futureMergeCost + futureContextCost
	futureTotalHours := futureReviewHours + futureMergeHours + futureContextHours
	delayCost := deliveryDelayCost + codeChurnCost + automatedUpdatesCost + prTrackingCost + futureTotalCost
	totalDelayHours := deliveryDelayHours + codeChurnHours + automatedUpdatesHours + prTrackingHours + futureTotalHours

	delayCostDetail := DelayCostDetail{
		DeliveryDelayCost:     deliveryDelayCost,
		CodeChurnCost:         codeChurnCost,
		AutomatedUpdatesCost:  automatedUpdatesCost,
		PRTrackingCost:        prTrackingCost,
		FutureReviewCost:      futureReviewCost,
		FutureMergeCost:       futureMergeCost,
		FutureContextCost:     futureContextCost,
		DeliveryDelayHours:    deliveryDelayHours,
		CodeChurnHours:        codeChurnHours,
		AutomatedUpdatesHours: automatedUpdatesHours,
		PRTrackingHours:       prTrackingHours,
		FutureReviewHours:     futureReviewHours,
		FutureMergeHours:      futureMergeHours,
		FutureContextHours:    futureContextHours,
		ReworkPercentage:      reworkPercentage * 100.0, // Store as percentage (0-100 scale, e.g., 41.0 = 41%)
		TotalDelayCost:        delayCost,
		TotalDelayHours:       totalDelayHours,
	}

	// Calculate total cost
	totalCost := authorCost.TotalCost + delayCost
	for _, pc := range participantCosts {
		totalCost += pc.TotalCost
	}

	// Log final breakdown summary
	slog.Info("PR breakdown summary",
		"pr_author", data.Author,
		"pr_duration_hours", delayHours,
		"delivery_delay_hours", deliveryDelayHours,
		"code_churn_hours", codeChurnHours,
		"total_cost", totalCost,
		"author_cost", authorCost.TotalCost,
		"delay_cost", delayCost)

	return Breakdown{
		Author:             authorCost,
		Participants:       participantCosts,
		DelayCost:          delayCost,
		DelayCostDetail:    delayCostDetail,
		DelayHours:         delayHours,
		DelayCapped:        capped,
		HourlyRate:         hourlyRate,
		AnnualSalary:       cfg.AnnualSalary,
		BenefitsMultiplier: cfg.BenefitsMultiplier,
		PRAuthor:           data.Author,
		PRDuration:         delayHours,
		AuthorBot:          data.AuthorBot,
		TotalCost:          totalCost,
	}
}

// calculateAuthorCost computes the author's costs broken down by type.
func calculateAuthorCost(data PRData, cfg Config, hourlyRate float64) AuthorCostDetail {
	// 1. Code Cost: COCOMO-based estimation for development effort
	// COCOMO II includes all overhead: understanding existing code, testing, integration, etc.
	//
	// Split into modified vs new lines:
	// - Modified lines = min(additions, deletions) - these are changes to existing code
	// - New lines = additions - modified lines - these are net new code
	// Modified code costs less because architecture is already in place
	modifiedLines := min(data.LinesAdded, data.LinesDeleted)
	newLines := data.LinesAdded - modifiedLines

	var newCodeHours, adaptationHours, newCodeCost, adaptationCost float64

	// Skip code costs for bot authors (they don't have human development time)
	if !data.AuthorBot {
		// Calculate effort separately for new and modified code
		newEffort := cocomo.EstimateEffort(newLines, cfg.COCOMO)
		modifiedEffort := cocomo.EstimateEffort(modifiedLines, cfg.COCOMO)

		// Apply modification cost factor (modified code is cheaper)
		newCodeHours = newEffort.Hours()
		adaptationHours = modifiedEffort.Hours() * cfg.ModificationCostFactor
		newCodeCost = newCodeHours * hourlyRate
		adaptationCost = adaptationHours * hourlyRate
	}

	// 2. GitHub Cost + GitHub Context Cost: Based on author's events
	// Include all commits (even if Actor != data.Author) plus author's non-commit events
	var authorEvents []ParticipantEvent
	for _, event := range data.Events {
		// All commits go to Author, regardless of Actor
		// (commits may be attributed to full name instead of GitHub username)
		// Non-commit events only if from the author
		if event.Kind == "commit" || event.Actor == data.Author {
			authorEvents = append(authorEvents, event)
		}
	}
	githubHours, githubContextHours, sessions := calculateSessionCosts(authorEvents, cfg)
	githubCost := githubHours * hourlyRate
	githubContextCost := githubContextHours * hourlyRate

	totalHours := newCodeHours + adaptationHours + githubHours + githubContextHours
	totalCost := newCodeCost + adaptationCost + githubCost + githubContextCost

	return AuthorCostDetail{
		NewCodeCost:        newCodeCost,
		AdaptationCost:     adaptationCost,
		GitHubCost:         githubCost,
		GitHubContextCost:  githubContextCost,
		NewLines:           newLines,
		ModifiedLines:      modifiedLines,
		LinesAdded:         data.LinesAdded,
		Events:             len(authorEvents),
		Sessions:           sessions,
		NewCodeHours:       newCodeHours,
		AdaptationHours:    adaptationHours,
		GitHubHours:        githubHours,
		GitHubContextHours: githubContextHours,
		TotalHours:         totalHours,
		TotalCost:          totalCost,
	}
}

// calculateParticipantCosts computes costs for all participants except the author.
// Excludes commits (which are attributed to the author).
//
// Cost breakdown:
// 1. Review Cost - LOC-based, once per reviewer (anyone with review/review_comment events)
// 2. Other Events - Session-based for non-review events (comments, assignments, etc.)
// 3. Context Switching - Session-based on ALL events (review events have 0 duration but count for sessions).
func calculateParticipantCosts(data PRData, cfg Config, hourlyRate float64) []ParticipantCostDetail {
	// Group events by actor (excluding author and excluding commits)
	eventsByActor := make(map[string][]ParticipantEvent)
	for _, event := range data.Events {
		// Skip commits (all commits go to Author)
		if event.Kind == "commit" {
			continue
		}
		// Skip events by the author (already in Author section)
		if event.Actor != data.Author {
			eventsByActor[event.Actor] = append(eventsByActor[event.Actor], event)
		}
	}

	var participantCosts []ParticipantCostDetail

	for actor, events := range eventsByActor {
		// Check if this person is a reviewer (has review or review_comment events)
		isReviewer := false
		for _, event := range events {
			if event.Kind == "review" || event.Kind == "review_comment" {
				isReviewer = true
				break
			}
		}

		// Calculate review cost (LOC-based, once per reviewer)
		var reviewHours float64
		var reviewCost float64
		if isReviewer {
			inspectionRate := cfg.ReviewInspectionRate
			if inspectionRate <= 0 {
				inspectionRate = 275.0 // Default to average
			}
			reviewHours = float64(data.LinesAdded) / inspectionRate
			reviewCost = reviewHours * hourlyRate
		}

		// Calculate session-based costs (all events, but review events have 0 duration)
		// calculateSessionCosts automatically gives review events 0 duration
		otherEventsHours, contextHours, sessions := calculateSessionCosts(events, cfg)
		otherEventsCost := otherEventsHours * hourlyRate
		contextCost := contextHours * hourlyRate

		slog.Info("Participant cost breakdown",
			"actor", actor,
			"is_reviewer", isReviewer,
			"total_events", len(events),
			"review_hours", reviewHours,
			"other_events_hours", otherEventsHours,
			"context_hours", contextHours,
			"sessions", sessions)

		totalHours := reviewHours + otherEventsHours + contextHours
		totalCost := reviewCost + otherEventsCost + contextCost

		participantCosts = append(participantCosts, ParticipantCostDetail{
			Actor:              actor,
			GitHubCost:         otherEventsCost, // Other Events cost
			GitHubContextCost:  contextCost,     // Context switching
			ReviewCost:         reviewCost,      // Review cost (new field)
			Events:             len(events),
			Sessions:           sessions,
			GitHubHours:        otherEventsHours, // Other Events hours
			GitHubContextHours: contextHours,     // Context switching hours
			ReviewHours:        reviewHours,      // Review hours (new field)
			TotalHours:         totalHours,
			TotalCost:          totalCost,
		})
	}

	// Sort by total cost descending for consistent output
	slices.SortFunc(participantCosts, func(a, b ParticipantCostDetail) int {
		return cmp.Compare(b.TotalCost, a.TotalCost)
	})

	return participantCosts
}

// calculateSessionCosts computes GitHub and context switching costs based on event sessions.
//
// Session Logic:
// - Events within SessionGapThreshold (default 20 min) are part of the same session
// - Events >20 min apart start a new session
//
// GitHub Time Calculation:
// - Each event counts as EventDuration (default 10 min)
// - Gaps between events within a session don't add time (assumed to be part of the work)
//
// Context Switching (Microsoft Research: Iqbal & Horvitz 2007):
// - First session: ContextSwitchInDuration (3 min) at start
// - Between sessions: min(ContextSwitchOutDuration + ContextSwitchInDuration, gap) to avoid double-counting
//   - If gap >= (16.55 + 3 = 19.55 min): full context out + context in
//   - If gap < 19.55 min: split gap proportionally based on in/out ratio
//
// - Last session: ContextSwitchOutDuration (16.55 min) at end
//
// Example: 3 events in one session, then 1 event 30 min later
// - Session 1: 3 (context in) + 3×10 (events) + (context out handled by gap)
// - Gap: 30 min (> 19.55), so full context overhead = 16.55 out + 3 in
// - Session 2: (3 context in from gap) + 1×10 (event) + 16.55 (context out)
// - Total context: 3 + 16.55 + 3 + 16.55 = 39.1 min.
func calculateSessionCosts(events []ParticipantEvent, cfg Config) (githubHours, contextHours float64, sessions int) {
	if len(events) == 0 {
		return 0, 0, 0
	}

	// Sort events by timestamp
	sorted := make([]ParticipantEvent, len(events))
	copy(sorted, events)
	slices.SortFunc(sorted, func(a, b ParticipantEvent) int {
		return a.Timestamp.Compare(b.Timestamp)
	})

	gapThreshold := cfg.SessionGapThreshold
	contextIn := cfg.ContextSwitchInDuration
	contextOut := cfg.ContextSwitchOutDuration
	eventDur := cfg.EventDuration

	// Group events into sessions
	type session struct {
		start int
		end   int
	}
	var sessionGroups []session

	i := 0
	for i < len(sorted) {
		start := i
		end := start

		// Find the end of this session (events within SessionGapThreshold)
		for end+1 < len(sorted) {
			gap := sorted[end+1].Timestamp.Sub(sorted[end].Timestamp)
			if gap > gapThreshold {
				break // New session starts
			}
			end++
		}

		sessionGroups = append(sessionGroups, session{start: start, end: end})
		i = end + 1
	}

	// Calculate GitHub time (eventDur per event, except review events which have 0 duration)
	var githubTime time.Duration
	for _, sess := range sessionGroups {
		for idx := sess.start; idx <= sess.end; idx++ {
			event := sorted[idx]
			// Review and review_comment events have 0 duration (but count for sessions)
			if event.Kind == "review" || event.Kind == "review_comment" {
				continue
			}
			githubTime += eventDur
		}
	}

	// Calculate context switching with gap awareness
	var contextTime time.Duration

	if len(sessionGroups) == 0 {
		return 0, 0, 0
	}

	// First session: context in
	contextTime += contextIn

	// Between sessions: context out + context in, capped by gap
	for i := range len(sessionGroups) - 1 {
		lastEventOfSession := sorted[sessionGroups[i].end].Timestamp
		firstEventOfNextSession := sorted[sessionGroups[i+1].start].Timestamp
		gap := firstEventOfNextSession.Sub(lastEventOfSession)

		// Maximum context switch is contextOut + contextIn
		maxContextSwitch := contextOut + contextIn
		if gap >= maxContextSwitch {
			contextTime += maxContextSwitch
		} else {
			// Cap at gap - split proportionally based on out/in ratio
			// This maintains the asymmetry (16.55 min out vs 3 min in)
			contextTime += gap
		}
	}

	// Last session: context out
	contextTime += contextOut

	githubHours = githubTime.Hours()
	contextHours = contextTime.Hours()
	sessionCount := len(sessionGroups)

	return githubHours, contextHours, sessionCount
}
