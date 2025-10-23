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

	// Delay cost factor as percentage of hourly rate (default: 0.20 = 20%)
	// This represents the opportunity cost of having a PR open
	DelayCostFactor float64

	// Maximum time after last event to count for project delay (default: 14 days / 2 weeks)
	// Only counts delay costs up to this many days after the last event on the PR
	MaxDelayAfterLastEvent time.Duration

	// Maximum total project delay duration (default: 90 days / 3 months)
	// Absolute cap on project delay costs regardless of PR age
	MaxProjectDelay time.Duration

	// Maximum duration for code drift calculation (default: 90 days / 3 months)
	// Code drift is capped at this duration (affects rework percentage)
	MaxCodeDrift time.Duration

	// COCOMO configuration for estimating code writing effort
	COCOMO cocomo.Config
}

// DefaultConfig returns reasonable defaults for cost calculation.
func DefaultConfig() Config {
	return Config{
		AnnualSalary:           250000.0,
		BenefitsMultiplier:     1.3,
		HoursPerYear:           2080.0,
		EventDuration:          20 * time.Minute,
		ContextSwitchDuration:  20 * time.Minute,
		SessionGapThreshold:    60 * time.Minute,
		DelayCostFactor:        0.20,
		MaxDelayAfterLastEvent: 14 * 24 * time.Hour, // 14 days (2 weeks) after last event
		MaxProjectDelay:        90 * 24 * time.Hour, // 90 days absolute max
		MaxCodeDrift:           90 * 24 * time.Hour, // 90 days
		COCOMO:                 cocomo.DefaultConfig(),
	}
}

// ParticipantEvent represents a single event by a participant.
type ParticipantEvent struct {
	Timestamp time.Time
	Actor     string
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
	CodeCost          float64 // COCOMO-based cost for writing code
	CodeContextCost   float64 // Cost of context switching while writing code (Microsoft research)
	GitHubCost        float64 // Cost of GitHub interactions (commits, comments, etc.)
	GitHubContextCost float64 // Cost of context switching for GitHub sessions

	// Supporting details
	LinesAdded         int     // Number of lines of code added
	Events             int     // Number of author events
	Sessions           int     // Number of GitHub work sessions
	CodeHours          float64 // Hours spent writing code (COCOMO)
	CodeContextHours   float64 // Hours spent context switching during coding
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
	ProjectDelayCost float64 // Opportunity cost of engineer time (20% factor)
	CodeChurnCost  float64 // COCOMO cost for rework/merge conflicts
	FutureGitHubCost float64 // Cost for future GitHub activity (3 events with context)

	// Supporting details
	ProjectDelayHours float64 // Hours of project delay
	CodeChurnHours  float64 // Hours for code churn
	FutureGitHubHours float64 // Hours for future GitHub activity
	ReworkPercentage  float64 // Percentage of code requiring rework (1%-30%)
	TotalDelayCost    float64 // Total delay cost (sum of above)
	TotalDelayHours   float64 // Total delay hours
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

	// 1. Project Delay: Configured percentage (default 20%) of engineer time
	projectDelayCost := hourlyRate * cappedHrs * cfg.DelayCostFactor
	projectDelayHours := cappedHrs

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

	if !isClosed && delayDays >= 3.0 {
		// Cap days at configured maximum for drift calculation (default: 90 days)
		maxDriftDays := cfg.MaxCodeDrift.Hours() / 24.0
		driftDays := delayDays
		if driftDays > maxDriftDays {
			driftDays = maxDriftDays
		}

		// Probability-based drift: 1 - (1 - 0.04)^(days/7)
		weeks := driftDays / 7.0
		reworkPercentage = 1.0 - math.Pow(0.96, weeks)

		reworkLOC = int(float64(data.LinesAdded) * reworkPercentage)

		// Ensure minimum of 1 LOC for PRs >= 3 days
		if reworkLOC < 1 && delayDays >= 3.0 {
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

	// 3. Future GitHub time: 3 events with full context switching
	// Only calculated for open PRs - closed PRs won't have future activity
	// Each event: context in + event time + context out
	var futureGitHubHours float64
	var futureGitHubCost float64

	if !isClosed {
		futureGitHubDuration := 3 * (cfg.ContextSwitchDuration + cfg.EventDuration + cfg.ContextSwitchDuration)
		futureGitHubHours = futureGitHubDuration.Hours()
		futureGitHubCost = futureGitHubHours * hourlyRate
	}

	// Total delay cost
	delayCost := projectDelayCost + codeChurnCost + futureGitHubCost
	totalDelayHours := projectDelayHours + codeChurnHours + futureGitHubHours

	delayCostDetail := DelayCostDetail{
		ProjectDelayCost:  projectDelayCost,
		CodeChurnCost:   codeChurnCost,
		FutureGitHubCost:  futureGitHubCost,
		ProjectDelayHours: projectDelayHours,
		CodeChurnHours:  codeChurnHours,
		FutureGitHubHours: futureGitHubHours,
		ReworkPercentage:  reworkPercentage * 100.0, // Store as percentage (0-100 scale, e.g., 41.0 = 41%)
		TotalDelayCost:    delayCost,
		TotalDelayHours:   totalDelayHours,
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
	// 1. Code Cost: COCOMO-based estimation for writing code
	codeEffort := cocomo.EstimateEffort(data.LinesAdded, cfg.COCOMO)
	codeHours := codeEffort.Hours()
	codeCost := codeHours * hourlyRate

	// 2. Code Context Switching Cost: Interruptions during code writing
	// Based on Microsoft research (Czerwinski et al., 2004):
	// Context switching overhead = COCOMO hours × 0.2 × sqrt(KLOC)
	// This captures the cognitive overhead of task switching while writing code
	kloc := float64(data.LinesAdded) / 1000.0
	codeContextFactor := 0.2 * math.Sqrt(kloc)
	codeContextHours := codeHours * codeContextFactor
	codeContextCost := codeContextHours * hourlyRate

	// 3. GitHub Cost + GitHub Context Cost: Based on author's events
	var authorEvents []ParticipantEvent
	for _, event := range data.Events {
		if event.Actor == data.Author {
			authorEvents = append(authorEvents, event)
		}
	}
	githubHours, githubContextHours, sessions := calculateSessionCosts(authorEvents, cfg)
	githubCost := githubHours * hourlyRate
	githubContextCost := githubContextHours * hourlyRate

	totalHours := codeHours + codeContextHours + githubHours + githubContextHours
	totalCost := codeCost + codeContextCost + githubCost + githubContextCost

	return AuthorCostDetail{
		CodeCost:           codeCost,
		CodeContextCost:    codeContextCost,
		GitHubCost:         githubCost,
		GitHubContextCost:  githubContextCost,
		LinesAdded:         data.LinesAdded,
		Events:             len(authorEvents),
		Sessions:           sessions,
		CodeHours:          codeHours,
		CodeContextHours:   codeContextHours,
		GitHubHours:        githubHours,
		GitHubContextHours: githubContextHours,
		TotalHours:         totalHours,
		TotalCost:          totalCost,
	}
}

// calculateParticipantCosts computes costs for all participants except the author.
func calculateParticipantCosts(data PRData, cfg Config, hourlyRate float64) []ParticipantCostDetail {
	// Group events by actor
	eventsByActor := make(map[string][]ParticipantEvent)
	for _, event := range data.Events {
		if event.Actor != data.Author {
			eventsByActor[event.Actor] = append(eventsByActor[event.Actor], event)
		}
	}

	var participantCosts []ParticipantCostDetail

	for actor, events := range eventsByActor {
		// Calculate GitHub and GitHub Context costs based on sessions
		githubHours, githubContextHours, sessions := calculateSessionCosts(events, cfg)
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
// - Events within SessionGapThreshold (default 60 min) are part of the same session
// - Events >60 min apart start a new session
//
// Time Calculation per Session:
// - First event: ContextSwitchIn + EventTime + GapToNext (or ContextSwitchOut if last)
// - Middle events: GapFromPrev + EventTime + GapToNext
// - Last event: GapFromPrev + EventTime + ContextSwitchOut
//
// Example: 3 events 5 minutes apart
// - Event 1: 20 (context in) + 20 (event) + 5 (gap) = 45 min
// - Event 2: 5 (gap) + 20 (event) + 5 (gap) = 30 min
// - Event 3: 5 (gap) + 20 (event) + 20 (context out) = 45 min
// - Total: 120 minutes (60 GitHub, 40 context, 20 gaps).
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

	var githubTime time.Duration
	var contextTime time.Duration
	count := 0

	i := 0
	for i < len(sorted) {
		// Start a new session
		count++
		start := i

		// Find the end of this session (events within SessionGapThreshold)
		end := start
		for end+1 < len(sorted) {
			gap := sorted[end+1].Timestamp.Sub(sorted[end].Timestamp)
			if gap > gapThreshold {
				break // New session starts
			}
			end++
		}

		// Calculate costs for this session
		// Context switch in at the start
		contextTime += contextSwitch

		for j := start; j <= end; j++ {
			// Each event costs EventTime
			githubTime += eventDur

			// Add gap time to next event (if within session)
			if j < end {
				gap := sorted[j+1].Timestamp.Sub(sorted[j].Timestamp)
				githubTime += gap
			}
		}

		// Context switch out at the end
		contextTime += contextSwitch

		// Move to next session
		i = end + 1
	}

	githubHours = githubTime.Hours()
	contextHours = contextTime.Hours()
	sessions = count

	return githubHours, contextHours, sessions
}
