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
	// Annual salary used for calculating hourly rate (default: $250,000)
	AnnualSalary float64

	// Benefits multiplier applied to salary (default: 1.3 = 30% benefits)
	BenefitsMultiplier float64

	// Hours per year for calculating hourly rate (default: 2080)
	HoursPerYear float64

	// Time per GitHub event (default: 20 minutes)
	EventDuration time.Duration

	// Time for context switching in/out (default: 20 minutes)
	ContextSwitchDuration time.Duration

	// Session gap threshold (default: 60 minutes)
	// Events within this gap are considered part of the same session
	SessionGapThreshold time.Duration

	// Delivery delay factor as percentage of hourly rate (default: 0.15 = 15%)
	// Represents opportunity cost of blocked value delivery
	DeliveryDelayFactor float64

	// Coordination factor as percentage of hourly rate (default: 0.05 = 5%)
	// Represents mental overhead of tracking unmerged work
	CoordinationFactor float64

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

	// COCOMO configuration for estimating code writing effort
	COCOMO cocomo.Config
}

// DefaultConfig returns reasonable defaults for cost calculation.
func DefaultConfig() Config {
	return Config{
		AnnualSalary:           250000.0,
		BenefitsMultiplier:     1.3,
		HoursPerYear:           2080.0,
		EventDuration:          10 * time.Minute, // 10 minutes per GitHub event
		ContextSwitchDuration:  20 * time.Minute, // 20 minutes to context switch in/out
		SessionGapThreshold:    20 * time.Minute, // Events within 20 min are same session
		DeliveryDelayFactor:    0.15,  // 15% opportunity cost
		CoordinationFactor:     0.05,  // 5% mental overhead
		MaxDelayAfterLastEvent: 14 * 24 * time.Hour, // 14 days (2 weeks) after last event
		MaxProjectDelay:        90 * 24 * time.Hour, // 90 days absolute max
		MaxCodeDrift:           90 * 24 * time.Hour, // 90 days
		ReviewInspectionRate:   275.0, // 275 LOC/hour (average of optimal 150-400 range)
		COCOMO:                 cocomo.DefaultConfig(),
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
	// When the PR was opened
	CreatedAt time.Time
	// When the PR was closed/merged (zero if still open)
	ClosedAt time.Time
	// PR author's username
	Author string
	// All human events (reviews, comments, commits, etc.) with timestamps
	// Excludes bot events
	Events []ParticipantEvent
	// Lines of code added by the author
	LinesAdded int
}

// AuthorCostDetail breaks down the author's costs.
type AuthorCostDetail struct {
	CodeCost          float64 // COCOMO-based cost for writing code (development effort)
	GitHubCost        float64 // Cost of GitHub interactions (commits, comments, etc.)
	GitHubContextCost float64 // Cost of context switching for GitHub sessions

	// Supporting details
	LinesAdded         int     // Number of lines of code added
	Events             int     // Number of author events
	Sessions           int     // Number of GitHub work sessions
	CodeHours          float64 // Hours spent writing code (COCOMO)
	GitHubHours        float64 // Hours spent on GitHub interactions
	GitHubContextHours float64 // Hours spent context switching for GitHub
	TotalHours         float64 // Total hours (sum of above)
	TotalCost          float64 // Total author cost
}

// ParticipantCostDetail breaks down a participant's costs.
type ParticipantCostDetail struct {
	Actor             string  // Participant username
	GitHubCost        float64 // Cost of GitHub interactions
	GitHubContextCost float64 // Cost of context switching for GitHub sessions

	// Supporting details
	Events             int     // Number of participant events
	Sessions           int     // Number of GitHub work sessions
	GitHubHours        float64 // Hours spent on GitHub interactions
	GitHubContextHours float64 // Hours spent context switching for GitHub
	TotalHours         float64 // Total hours (sum of above)
	TotalCost          float64 // Total participant cost
}

// DelayCostDetail holds itemized delay costs.
type DelayCostDetail struct {
	DeliveryDelayCost float64 // Opportunity cost - blocked value delivery (15% factor)
	CoordinationCost  float64 // Mental overhead - tracking unmerged work (5% factor)
	CodeChurnCost     float64 // COCOMO cost for rework/merge conflicts

	// Future costs (estimated for open PRs) - split across 2 people
	FutureReviewCost  float64 // Cost for future review events (2 events × 20 min)
	FutureMergeCost   float64 // Cost for future merge event (1 event × 20 min)
	FutureContextCost float64 // Cost for future context switching (3 events × 40 min)

	// Supporting details
	DeliveryDelayHours float64 // Hours of delivery delay
	CoordinationHours  float64 // Hours of coordination overhead
	CodeChurnHours     float64 // Hours for code churn
	FutureReviewHours  float64 // Hours for future review events
	FutureMergeHours   float64 // Hours for future merge event
	FutureContextHours float64 // Hours for future context switching
	ReworkPercentage   float64 // Percentage of code requiring rework (1%-41%)
	TotalDelayCost     float64 // Total delay cost (sum of above)
	TotalDelayHours    float64 // Total delay hours
}

// Breakdown shows fully itemized costs for a pull request.
type Breakdown struct {
	// Participant costs (everyone except the author)
	Participants []ParticipantCostDetail

	// Author costs (person who opened the PR)
	Author AuthorCostDetail

	// Delay cost with itemized breakdown
	DelayCostDetail DelayCostDetail

	// Delay cost with itemized breakdown
	DelayCost float64

	// Supporting details for delay cost
	DelayHours         float64
	HourlyRate         float64
	AnnualSalary       float64
	BenefitsMultiplier float64

	// Total cost (sum of all components)
	TotalCost float64

	// True if project delay was capped (either by 2 weeks after last event or 90 days total)
	DelayCapped bool
}

// Calculate computes the total cost of a pull request with detailed breakdowns.
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

	// Cap Project Delay in two ways:
	// 1. Only count up to MaxDelayAfterLastEvent (default: 14 days) after the last event
	// 2. Absolute maximum of MaxProjectDelay (default: 90 days) total
	var capped bool
	var cappedHrs float64

	cappedHrs = delayHours

	// First, apply the "2 weeks after last event" cap
	maxAfterEvent := cfg.MaxDelayAfterLastEvent.Hours()
	if timeSinceLastEvent > maxAfterEvent {
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

	// Second, apply the absolute maximum cap
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
	deliveryDelayCost := hourlyRate * cappedHrs * cfg.DeliveryDelayFactor
	deliveryDelayHours := cappedHrs * cfg.DeliveryDelayFactor // Productivity-equivalent hours

	// 1b. Coordination Overhead: Mental cost of tracking unmerged work (default 5%)
	// The 5% represents the mental overhead of tracking this unmerged PR
	coordinationCost := hourlyRate * cappedHrs * cfg.CoordinationFactor
	coordinationHours := cappedHrs * cfg.CoordinationFactor // Productivity-equivalent hours

	// 2. Code Churn (Rework): Probability-based drift formula
	// Only calculated for open PRs - closed PRs won't need future updates
	//
	// Research basis:
	// - Windows Vista: 4-8% weekly code churn (Nagappan et al., Microsoft Research, 2008)
	// - Using 4% weekly baseline for active repositories
	//
	// Formula: Probability that a line becomes stale over time
	//   drift = 1 - (1 - weeklyChurn)^(weeks)
	//   drift = 1 - (0.96)^(days/7)
	//
	// This models the cumulative probability that any given line in the PR needs rework
	// due to codebase changes. Unlike compounding formulas, this accounts for the fact
	// that the same code areas often change multiple times.
	//
	// Expected drift percentages:
	// -  3 days: ~2% drift
	// -  7 days: ~4% drift (matches weekly churn)
	// - 14 days: ~8% drift
	// - 30 days: ~16% drift
	// - 60 days: ~29% drift
	// - 90 days: ~41% drift (days capped at 90)
	//
	// Reference:
	// Nagappan, N., Murphy, B., & Basili, V. (2008). The Influence of Organizational
	// Structure on Software Quality. ACM/IEEE ICSE. DOI: 10.1145/1368088.1368160

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

		// Probability-based drift: 1 - (1 - 0.04)^(days/7)
		weeks := cappedDriftDays / 7.0
		reworkPercentage = 1.0 - math.Pow(0.96, weeks)

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
		futureContextDuration := 2 * (cfg.ContextSwitchDuration + cfg.ContextSwitchDuration)
		futureContextHours = futureContextDuration.Hours()
		futureContextCost = futureContextHours * hourlyRate
	}

	// Total delay cost
	futureTotalCost := futureReviewCost + futureMergeCost + futureContextCost
	futureTotalHours := futureReviewHours + futureMergeHours + futureContextHours
	delayCost := deliveryDelayCost + coordinationCost + codeChurnCost + futureTotalCost
	totalDelayHours := deliveryDelayHours + coordinationHours + codeChurnHours + futureTotalHours

	delayCostDetail := DelayCostDetail{
		DeliveryDelayCost:  deliveryDelayCost,
		CoordinationCost:   coordinationCost,
		CodeChurnCost:      codeChurnCost,
		FutureReviewCost:   futureReviewCost,
		FutureMergeCost:    futureMergeCost,
		FutureContextCost:  futureContextCost,
		DeliveryDelayHours: deliveryDelayHours,
		CoordinationHours:  coordinationHours,
		CodeChurnHours:     codeChurnHours,
		FutureReviewHours:  futureReviewHours,
		FutureMergeHours:   futureMergeHours,
		FutureContextHours: futureContextHours,
		ReworkPercentage:   reworkPercentage * 100.0, // Store as percentage (0-100 scale, e.g., 41.0 = 41%)
		TotalDelayCost:     delayCost,
		TotalDelayHours:    totalDelayHours,
	}

	// Calculate total cost
	totalCost := authorCost.TotalCost + delayCost
	for _, pc := range participantCosts {
		totalCost += pc.TotalCost
	}

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
		TotalCost:          totalCost,
	}
}

// calculateAuthorCost computes the author's costs broken down by type.
func calculateAuthorCost(data PRData, cfg Config, hourlyRate float64) AuthorCostDetail {
	// 1. Code Cost: COCOMO-based estimation for development effort
	// COCOMO II includes all overhead: understanding existing code, testing, integration, etc.
	codeEffort := cocomo.EstimateEffort(data.LinesAdded, cfg.COCOMO)
	codeHours := codeEffort.Hours()
	codeCost := codeHours * hourlyRate

	// 2. GitHub Cost + GitHub Context Cost: Based on author's events
	// Include all commits (even if Actor != data.Author) plus author's non-commit events
	var authorEvents []ParticipantEvent
	for _, event := range data.Events {
		// All commits go to Author, regardless of Actor
		// (commits may be attributed to full name instead of GitHub username)
		if event.Kind == "commit" {
			authorEvents = append(authorEvents, event)
		} else if event.Actor == data.Author {
			// Non-commit events only if from the author
			authorEvents = append(authorEvents, event)
		}
	}
	githubHours, githubContextHours, sessions := calculateSessionCosts(authorEvents, cfg)
	githubCost := githubHours * hourlyRate
	githubContextCost := githubContextHours * hourlyRate

	totalHours := codeHours + githubHours + githubContextHours
	totalCost := codeCost + githubCost + githubContextCost

	return AuthorCostDetail{
		CodeCost:           codeCost,
		GitHubCost:         githubCost,
		GitHubContextCost:  githubContextCost,
		LinesAdded:         data.LinesAdded,
		Events:             len(authorEvents),
		Sessions:           sessions,
		CodeHours:          codeHours,
		GitHubHours:        githubHours,
		GitHubContextHours: githubContextHours,
		TotalHours:         totalHours,
		TotalCost:          totalCost,
	}
}

// calculateParticipantCosts computes costs for all participants except the author.
// Excludes commits (which are attributed to the author).
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
		// Calculate context switching costs based on sessions
		_, githubContextHours, sessions := calculateSessionCosts(events, cfg)

		// Calculate review time based on inspection rate (LOC / rate)
		// Defensive check: avoid division by zero
		inspectionRate := cfg.ReviewInspectionRate
		if inspectionRate <= 0 {
			inspectionRate = 275.0 // Default to average
		}
		githubHours := float64(data.LinesAdded) / inspectionRate

		githubCost := githubHours * hourlyRate
		githubContextCost := githubContextHours * hourlyRate

		totalHours := githubHours + githubContextHours
		totalCost := githubCost + githubContextCost

		participantCosts = append(participantCosts, ParticipantCostDetail{
			Actor:              actor,
			GitHubCost:         githubCost,
			GitHubContextCost:  githubContextCost,
			Events:             len(events),
			Sessions:           sessions,
			GitHubHours:        githubHours,
			GitHubContextHours: githubContextHours,
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
// Context Switching:
// - First session: ContextSwitchDuration (20 min) at start
// - Between sessions: min(2 × ContextSwitchDuration, gap) to avoid double-counting
//   - If gap >= 40 min: full 20 min out + 20 min in
//   - If gap < 40 min: split gap evenly (gap/2 out, gap/2 in)
// - Last session: ContextSwitchDuration (20 min) at end
//
// Example: 3 events in one session, then 1 event 30 min later
// - Session 1: 20 (context in) + 3×10 (events) + 20 (context out, but see gap)
// - Gap: 30 min (< 40), so context overhead = 30 min total (15 out, 15 in)
// - Session 2: (15 context in from gap) + 1×10 (event) + 20 (context out)
// - Total context: 20 + 30 + 20 = 70 min
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
	contextSwitch := cfg.ContextSwitchDuration
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

	// Calculate GitHub time (simple: eventDur per event)
	var githubTime time.Duration
	for _, sess := range sessionGroups {
		eventsInSession := sess.end - sess.start + 1
		githubTime += time.Duration(eventsInSession) * eventDur
	}

	// Calculate context switching with gap awareness
	var contextTime time.Duration

	if len(sessionGroups) == 0 {
		return 0, 0, 0
	}

	// First session: context in
	contextTime += contextSwitch

	// Between sessions: context out + context in, capped by gap
	for i := 0; i < len(sessionGroups)-1; i++ {
		lastEventOfSession := sorted[sessionGroups[i].end].Timestamp
		firstEventOfNextSession := sorted[sessionGroups[i+1].start].Timestamp
		gap := firstEventOfNextSession.Sub(lastEventOfSession)

		// Maximum context switch is 2 × contextSwitch (out + in)
		maxContextSwitch := 2 * contextSwitch
		if gap >= maxContextSwitch {
			contextTime += maxContextSwitch
		} else {
			// Cap at gap (implicitly split as gap/2 out + gap/2 in)
			contextTime += gap
		}
	}

	// Last session: context out
	contextTime += contextSwitch

	githubHours = githubTime.Hours()
	contextHours = contextTime.Hours()
	sessionCount := len(sessionGroups)

	return githubHours, contextHours, sessionCount
}
