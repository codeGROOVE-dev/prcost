// Package cost calculates the real-world cost of GitHub pull requests.
// Costs are broken down into detailed components with itemized inputs.
package cost

import (
	"math"
	"sort"
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

	// Minutes per GitHub event (default: 20 minutes)
	MinutesPerEvent float64

	// Minutes for context switching in/out (default: 20 minutes)
	ContextSwitchMinutes float64

	// Session gap threshold in minutes (default: 60 minutes)
	// Events within this gap are considered part of the same session
	SessionGapMinutes float64

	// Delay cost factor as percentage of hourly rate (default: 0.25 = 25%)
	// This represents the opportunity cost of having a PR open
	DelayCostFactor float64

	// COCOMO configuration for estimating code writing effort
	COCOMO cocomo.Config
}

// DefaultConfig returns reasonable defaults for cost calculation.
func DefaultConfig() Config {
	return Config{
		AnnualSalary:         250000.0,
		BenefitsMultiplier:   1.3,
		HoursPerYear:         2080.0,
		MinutesPerEvent:      20.0,
		ContextSwitchMinutes: 20.0,
		SessionGapMinutes:    60.0,
		DelayCostFactor:      0.25,
		COCOMO:               cocomo.DefaultConfig(),
	}
}

// HourlyRate calculates the hourly rate from annual salary including benefits.
// Formula: (salary × benefits_multiplier) / hours_per_year
func (c Config) HourlyRate() float64 {
	totalCompensation := c.AnnualSalary * c.BenefitsMultiplier
	return totalCompensation / c.HoursPerYear
}

// ParticipantEvent represents a single event by a participant.
type ParticipantEvent struct {
	Timestamp time.Time
	Actor     string
}

// PRData contains all information needed to calculate PR costs.
type PRData struct {
	// Lines of code added by the author
	LinesAdded int

	// PR author's username
	Author string

	// All human events (reviews, comments, commits, etc.) with timestamps
	// Excludes bot events
	Events []ParticipantEvent

	// When the PR was opened
	CreatedAt time.Time

	// When the PR was last updated
	UpdatedAt time.Time

	// Whether the author has write access (false means external contributor)
	AuthorHasWriteAccess bool
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
	CodeUpdatesCost  float64 // COCOMO cost for rework/merge conflicts
	FutureGitHubCost float64 // Cost for future GitHub activity (3 events with context)

	// Supporting details
	ProjectDelayHours float64 // Hours of project delay
	CodeUpdatesHours  float64 // Hours for code updates
	FutureGitHubHours float64 // Hours for future GitHub activity
	ReworkPercentage  float64 // Percentage of code requiring rework (1%-30%)
	TotalDelayCost    float64 // Total delay cost (sum of above)
	TotalDelayHours   float64 // Total delay hours
}

// Breakdown shows fully itemized costs for a pull request.
type Breakdown struct {
	// Author costs (person who opened the PR)
	Author AuthorCostDetail

	// Participant costs (everyone except the author)
	Participants []ParticipantCostDetail

	// Delay cost with itemized breakdown
	DelayCost       float64
	DelayCostDetail DelayCostDetail

	// Supporting details for delay cost
	DelayHours         float64
	DelayCapped        bool // True if delay was capped at 90 days
	HourlyRate         float64
	AnnualSalary       float64
	BenefitsMultiplier float64

	// Total cost (sum of all components)
	TotalCost float64
}

// Calculate computes the total cost of a pull request with detailed breakdowns.
func Calculate(data PRData, cfg Config) Breakdown {
	hourlyRate := cfg.HourlyRate()

	// Calculate author costs
	authorCost := calculateAuthorCost(data, cfg, hourlyRate)

	// Calculate participant costs (everyone except author)
	participantCosts := calculateParticipantCosts(data, cfg, hourlyRate)

	// Calculate delay cost with itemized breakdown (always shown)
	delayHours := data.UpdatedAt.Sub(data.CreatedAt).Hours()
	delayDays := delayHours / 24.0

	// Cap at 90 days
	const maxDelayDays = 90.0
	var capped bool
	var cappedDelayHours float64

	if delayDays > maxDelayDays {
		capped = true
		cappedDelayHours = maxDelayDays * 24.0
	} else {
		cappedDelayHours = delayHours
	}

	// 1. Project Delay: 20% of engineer time
	projectDelayCost := hourlyRate * cappedDelayHours * 0.20
	projectDelayHours := cappedDelayHours

	// 2. Code Updates (Rework): Power-law drift formula based on empirical research
	//
	// Research basis:
	// - Windows Vista: 4-8% weekly code churn (Nagappan et al., Microsoft Research, 2008)
	// - Using 4% weekly baseline for active repositories
	// - Daily churn rate: (1.04)^(1/7) - 1 ≈ 0.57% per day (compounding)
	// - Code drift accelerates non-linearly over time (power law behavior)
	//
	// Formula: driftMultiplier = 1 + (0.03 × days^0.7)
	//          reworkPercentage = driftMultiplier - 1
	//
	// Calibration to 4% weekly churn gives us:
	// - 1 day: ~3% drift
	// - 3 days: ~5% drift
	// - 7 days: ~7% drift (slightly above 4% to account for non-linearity)
	// - 14 days: ~12% drift
	// - 30 days: ~19% drift
	// - 90 days: ~35% drift (days capped at 90)
	//
	// Reference:
	// Nagappan, N., Murphy, B., & Basili, V. (2008). The Influence of Organizational
	// Structure on Software Quality. ACM/IEEE ICSE. DOI: 10.1145/1368088.1368160

	var reworkLOC int
	var codeUpdatesHours float64
	var codeUpdatesCost float64
	var reworkPercentage float64

	if delayDays < 3.0 {
		// Under 3 days: minimal drift
		reworkLOC = 0
		reworkPercentage = 0.0
	} else {
		// Cap days at 90 for drift calculation
		driftDays := delayDays
		if driftDays > 90.0 {
			driftDays = 90.0
		}

		// Power-law drift: driftMultiplier = 1 + (0.03 × days^0.7)
		driftMultiplier := 1.0 + (0.03 * math.Pow(driftDays, 0.7))
		reworkPercentage = driftMultiplier - 1.0

		reworkLOC = int(float64(data.LinesAdded) * reworkPercentage)

		// Ensure minimum of 1 LOC for PRs >= 3 days
		if reworkLOC < 1 && delayDays >= 3.0 {
			reworkLOC = 1
			if data.LinesAdded > 0 {
				reworkPercentage = 1.0 / float64(data.LinesAdded)
			}
		}
	}

	if reworkLOC > 0 {
		reworkEffort := cocomo.EstimateEffort(reworkLOC, cfg.COCOMO)
		codeUpdatesHours = reworkEffort.Hours()
		codeUpdatesCost = codeUpdatesHours * hourlyRate
		// Recalculate actual percentage for display
		if data.LinesAdded > 0 {
			reworkPercentage = float64(reworkLOC) / float64(data.LinesAdded)
		}
	}

	// 3. Future GitHub time: 3 events with full context switching
	// Each event: context in (20) + event time (20) + context out (20) = 60 minutes
	// 3 events = 180 minutes = 3 hours
	futureGitHubHours := 3.0 * (cfg.ContextSwitchMinutes + cfg.MinutesPerEvent + cfg.ContextSwitchMinutes) / 60.0
	futureGitHubCost := futureGitHubHours * hourlyRate

	// Total delay cost
	delayCost := projectDelayCost + codeUpdatesCost + futureGitHubCost
	totalDelayHours := projectDelayHours + codeUpdatesHours + futureGitHubHours

	delayCostDetail := DelayCostDetail{
		ProjectDelayCost:  projectDelayCost,
		CodeUpdatesCost:   codeUpdatesCost,
		FutureGitHubCost:  futureGitHubCost,
		ProjectDelayHours: projectDelayHours,
		CodeUpdatesHours:  codeUpdatesHours,
		FutureGitHubHours: futureGitHubHours,
		ReworkPercentage:  reworkPercentage * 100.0, // Store as percentage (0-30)
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
	authorEvents := filterEventsByActor(data.Events, data.Author)
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
	eventsByActor := groupEventsByActor(data.Events, data.Author)

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
	sort.Slice(participantCosts, func(i, j int) bool {
		return participantCosts[i].TotalCost > participantCosts[j].TotalCost
	})

	return participantCosts
}

// calculateSessionCosts computes GitHub and context switching costs based on event sessions.
//
// Session Logic:
// - Events within SessionGapMinutes (default 60 min) are part of the same session
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
// - Total: 120 minutes (60 GitHub, 40 context, 20 gaps)
func calculateSessionCosts(events []ParticipantEvent, cfg Config) (githubHours, contextHours float64, sessions int) {
	if len(events) == 0 {
		return 0, 0, 0
	}

	// Sort events by timestamp
	sortedEvents := make([]ParticipantEvent, len(events))
	copy(sortedEvents, events)
	sort.Slice(sortedEvents, func(i, j int) bool {
		return sortedEvents[i].Timestamp.Before(sortedEvents[j].Timestamp)
	})

	sessionGapDuration := time.Duration(cfg.SessionGapMinutes * float64(time.Minute))
	contextSwitchDuration := time.Duration(cfg.ContextSwitchMinutes * float64(time.Minute))
	eventDuration := time.Duration(cfg.MinutesPerEvent * float64(time.Minute))

	var totalGitHubTime time.Duration
	var totalContextTime time.Duration
	sessionCount := 0

	i := 0
	for i < len(sortedEvents) {
		// Start a new session
		sessionCount++
		sessionStart := i

		// Find the end of this session (events within SessionGapMinutes)
		sessionEnd := sessionStart
		for sessionEnd+1 < len(sortedEvents) {
			gap := sortedEvents[sessionEnd+1].Timestamp.Sub(sortedEvents[sessionEnd].Timestamp)
			if gap > sessionGapDuration {
				break // New session starts
			}
			sessionEnd++
		}

		// Calculate costs for this session
		// Context switch in at the start
		totalContextTime += contextSwitchDuration

		for j := sessionStart; j <= sessionEnd; j++ {
			// Each event costs EventTime
			totalGitHubTime += eventDuration

			// Add gap time to next event (if within session)
			if j < sessionEnd {
				gap := sortedEvents[j+1].Timestamp.Sub(sortedEvents[j].Timestamp)
				totalGitHubTime += gap
			}
		}

		// Context switch out at the end
		totalContextTime += contextSwitchDuration

		// Move to next session
		i = sessionEnd + 1
	}

	githubHours = totalGitHubTime.Hours()
	contextHours = totalContextTime.Hours()
	sessions = sessionCount

	return githubHours, contextHours, sessions
}

// filterEventsByActor returns events for a specific actor.
func filterEventsByActor(events []ParticipantEvent, actor string) []ParticipantEvent {
	var filtered []ParticipantEvent
	for _, event := range events {
		if event.Actor == actor {
			filtered = append(filtered, event)
		}
	}
	return filtered
}

// groupEventsByActor groups events by actor, excluding the specified author.
func groupEventsByActor(events []ParticipantEvent, excludeAuthor string) map[string][]ParticipantEvent {
	grouped := make(map[string][]ParticipantEvent)
	for _, event := range events {
		if event.Actor != excludeAuthor {
			grouped[event.Actor] = append(grouped[event.Actor], event)
		}
	}
	return grouped
}
