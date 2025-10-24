// Package cocomo implements COCOMO II effort estimation for software projects.
// COCOMO (Constructive Cost Model) estimates development effort based on lines of code.
package cocomo

import (
	"math"
	"time"
)

// Config holds parameters for COCOMO II effort estimation.
// These defaults are based on the COCOMO II model for organic projects.
type Config struct {
	// Multiplier is the base effort coefficient (default: 2.94)
	Multiplier float64

	// Exponent is the scale factor (default: 1.0997)
	Exponent float64

	// MinimumEffort is the minimum effort in minutes (default: 20)
	MinimumEffort time.Duration
}

// DefaultConfig returns COCOMO II configuration with standard values.
func DefaultConfig() Config {
	return Config{
		Multiplier:    2.94,
		Exponent:      1.0997,
		MinimumEffort: 20 * time.Minute,
	}
}

// EstimateEffort calculates development effort based on lines of code.
//
// The formula used is: Effort = Multiplier × (KLOC)^Exponent
// where KLOC is thousands of lines of code.
//
// The result is in person-months, which we convert to hours by multiplying by 152
// (a standard industry conversion: 1 person-month = 152 hours).
//
// Parameters:
//   - linesOfCode: The number of lines of code written
//   - cfg: COCOMO configuration parameters
//
// Returns:
//   - Effort in hours (never less than config.MinimumEffort)
func EstimateEffort(linesOfCode int, cfg Config) time.Duration {
	// No effort for 0 lines of code (skip minimum)
	if linesOfCode == 0 {
		return 0
	}

	// Convert lines of code to thousands of lines (KLOC)
	kloc := float64(linesOfCode) / 1000.0

	// Apply COCOMO II formula: Effort = Multiplier × (KLOC)^Exponent
	// Result is in person-months
	personMonths := cfg.Multiplier * math.Pow(kloc, cfg.Exponent)

	// Convert person-months to hours (1 person-month = 152 hours)
	const hoursPerPersonMonth = 152.0
	hours := personMonths * hoursPerPersonMonth

	// Convert to duration
	effort := time.Duration(hours * float64(time.Hour))

	// Apply minimum effort floor (only for non-zero LOC)
	if effort < cfg.MinimumEffort {
		return cfg.MinimumEffort
	}

	return effort
}
