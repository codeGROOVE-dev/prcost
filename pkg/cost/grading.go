package cost

// EfficiencyGrade returns a letter grade and message based on efficiency percentage (MIT scale).
// Efficiency is the percentage of total cost that goes to productive work (author + participant)
// vs overhead/delays.
func EfficiencyGrade(efficiencyPct float64) (grade, message string) {
	switch {
	case efficiencyPct >= 97:
		return "A+", "Impeccable"
	case efficiencyPct >= 93:
		return "A", "Excellent"
	case efficiencyPct >= 90:
		return "A-", "Nearly excellent"
	case efficiencyPct >= 87:
		return "B+", "Acceptable+"
	case efficiencyPct >= 83:
		return "B", "Acceptable"
	case efficiencyPct >= 80:
		return "B-", "Nearly acceptable"
	case efficiencyPct >= 70:
		return "C", "Average"
	case efficiencyPct >= 60:
		return "D", "Not good my friend."
	default:
		return "F", "Failing"
	}
}

// MergeVelocityGrade returns a grade based on average PR open time in hours.
// Faster merge times indicate better team velocity and lower coordination overhead.
func MergeVelocityGrade(avgOpenHours float64) (grade, message string) {
	switch {
	case avgOpenHours <= 4: // 4 hours
		return "A+", "World-class velocity"
	case avgOpenHours <= 24: // 1 day
		return "A", "High-performing team"
	case avgOpenHours <= 84: // 3.5 days
		return "B", "Room for improvement"
	case avgOpenHours <= 132: // 5.5 days
		return "C", "Sluggish"
	case avgOpenHours <= 168: // 7 days (1 week)
		return "D", "Slow"
	default:
		return "F", "Failing"
	}
}

// MergeRateGrade returns a grade based on the percentage of PRs successfully merged.
// Higher merge rates indicate less wasted effort on abandoned work.
func MergeRateGrade(mergeRatePct float64) (grade, message string) {
	switch {
	case mergeRatePct > 90:
		return "A", "Excellent"
	case mergeRatePct > 80:
		return "B", "Good"
	case mergeRatePct > 70:
		return "C", "Acceptable"
	case mergeRatePct > 60:
		return "D", "Low"
	default:
		return "F", "Poor"
	}
}
