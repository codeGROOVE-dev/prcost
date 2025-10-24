package cocomo

import (
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Multiplier != 2.94 {
		t.Errorf("Expected multiplier 2.94, got %f", cfg.Multiplier)
	}

	if cfg.Exponent != 1.0997 {
		t.Errorf("Expected exponent 1.0997, got %f", cfg.Exponent)
	}

	if cfg.MinimumEffort != 20*time.Minute {
		t.Errorf("Expected minimum effort 20min, got %v", cfg.MinimumEffort)
	}
}

func TestEstimateEffort(t *testing.T) {
	cfg := DefaultConfig()

	tests := []struct {
		name          string
		linesOfCode   int
		expectedHours float64
		tolerance     float64
	}{
		{
			name:          "zero LOC should return zero (no minimum)",
			linesOfCode:   0,
			expectedHours: 0.0, // No effort for 0 LOC
			tolerance:     0.01,
		},
		{
			name:          "1 LOC should return minimum",
			linesOfCode:   1,
			expectedHours: 0.333, // 20 minutes
			tolerance:     0.01,
		},
		{
			name:          "3 LOC from real PR",
			linesOfCode:   3,
			expectedHours: 0.75,
			tolerance:     0.01,
		},
		{
			name:          "26 LOC from PR 1891",
			linesOfCode:   26,
			expectedHours: 8.07,
			tolerance:     0.1,
		},
		{
			name:          "100 LOC typical small change",
			linesOfCode:   100,
			expectedHours: 35.52,
			tolerance:     0.5,
		},
		{
			name:          "1000 LOC significant feature",
			linesOfCode:   1000,
			expectedHours: 446.88,
			tolerance:     1.0,
		},
		{
			name:          "10000 LOC major project",
			linesOfCode:   10000,
			expectedHours: 5622.0,
			tolerance:     10.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			effort := EstimateEffort(tt.linesOfCode, cfg)
			hours := effort.Hours()

			if hours < tt.expectedHours-tt.tolerance || hours > tt.expectedHours+tt.tolerance {
				t.Errorf("EstimateEffort(%d) = %.2f hours, expected %.2f ± %.2f hours",
					tt.linesOfCode, hours, tt.expectedHours, tt.tolerance)
			}
		})
	}
}

func TestEstimateEffortCustomConfig(t *testing.T) {
	cfg := Config{
		Multiplier:    3.5,
		Exponent:      1.1,
		MinimumEffort: 30 * time.Minute,
	}

	effort := EstimateEffort(100, cfg)
	hours := effort.Hours()

	// With higher multiplier and exponent, should be more than default
	defaultEffort := EstimateEffort(100, DefaultConfig())
	if hours <= defaultEffort.Hours() {
		t.Errorf("Custom config with higher parameters should result in more effort")
	}
}

func TestEstimateEffortMinimumFloor(t *testing.T) {
	cfg := Config{
		Multiplier:    2.94,
		Exponent:      1.0997,
		MinimumEffort: 2 * time.Hour, // High minimum
	}

	// Small LOC counts should hit the minimum
	effort := EstimateEffort(5, cfg)
	if effort != 2*time.Hour {
		t.Errorf("Expected minimum effort of 2 hours, got %v", effort)
	}
}

func TestEstimateEffortFormula(t *testing.T) {
	cfg := DefaultConfig()

	// Manually calculate for 100 LOC to verify formula
	// KLOC = 0.1
	// PersonMonths = 2.94 * (0.1)^1.0997 = 2.94 * 0.093 ≈ 0.273
	// Hours = 0.273 * 152 ≈ 41.5 hours
	effort := EstimateEffort(100, cfg)
	hours := effort.Hours()

	// Should be around 35.5 hours for 100 LOC
	if hours < 35.0 || hours > 36.0 {
		t.Errorf("100 LOC should yield ~35.5 hours, got %.2f hours", hours)
	}
}
