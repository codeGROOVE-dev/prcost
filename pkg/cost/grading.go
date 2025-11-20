package cost

// EfficiencyGrade returns a letter grade and message based on efficiency percentage (MIT scale).
// Efficiency is the percentage of total cost that goes to productive work (author + participant)
// vs overhead/delays.
func EfficiencyGrade(efficiencyPct float64) (grade, message string) {
	switch {
	case efficiencyPct >= 97:
		return "A+", "Outstanding efficiency"
	case efficiencyPct >= 93:
		return "A", "Excellent efficiency"
	case efficiencyPct >= 90:
		return "A-", "Very good efficiency"
	case efficiencyPct >= 87:
		return "B+", "Above average"
	case efficiencyPct >= 83:
		return "B", "Good efficiency"
	case efficiencyPct >= 80:
		return "B-", "Acceptable efficiency"
	case efficiencyPct >= 70:
		return "C", "Average efficiency"
	case efficiencyPct >= 60:
		return "D", "Below average"
	default:
		return "F", "Needs improvement"
	}
}

// MergeVelocityGrade returns a grade based on average PR open time in hours.
// Faster merge times indicate better team velocity and lower coordination overhead.
func MergeVelocityGrade(avgOpenHours float64) (grade, message string) {
	switch {
	case avgOpenHours <= 4: // 4 hours
		return "A+", "Exceptional velocity"
	case avgOpenHours <= 24: // 1 day
		return "A", "Excellent velocity"
	case avgOpenHours <= 84: // 3.5 days
		return "B", "Good velocity"
	case avgOpenHours <= 132: // 5.5 days
		return "C", "Average velocity"
	case avgOpenHours <= 168: // 7 days (1 week)
		return "D", "Below average"
	default:
		return "F", "Needs improvement"
	}
}

// MergeRateGrade returns a grade based on the percentage of PRs successfully merged.
// Higher merge rates indicate less wasted effort on abandoned work.
func MergeRateGrade(mergeRatePct float64) (grade, message string) {
	switch {
	case mergeRatePct > 90:
		return "A", "Excellent merge rate"
	case mergeRatePct > 80:
		return "B", "Good merge rate"
	case mergeRatePct > 70:
		return "C", "Average merge rate"
	case mergeRatePct > 60:
		return "D", "Below average"
	default:
		return "F", "Needs improvement"
	}
}
