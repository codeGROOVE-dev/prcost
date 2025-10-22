# prcost

Calculate the real-world cost of GitHub pull requests with detailed breakdowns of author effort, participant contributions, and delay costs.

## Installation

```bash
go install github.com/codeGROOVE-dev/prcost/cmd/prcost@latest
```

Or build from source:

```bash
git clone https://github.com/codeGROOVE-dev/prcost
cd prcost
go build -o prcost ./cmd/prcost
```

## Usage

```bash
prcost https://github.com/owner/repo/pull/123
prcost --format json https://github.com/owner/repo/pull/123
prcost --salary 300000 https://github.com/owner/repo/pull/123
```

## Cost Model

This model is based on early research combining COCOMO II effort estimation with session-based time tracking. Individual PR estimates may be under or over-estimated, particularly for delay costs. The recommendation is to use this model across a large pool of PRs and rely on the law of averages for meaningful insights.

### Author Costs

**Code Cost**: Writing effort estimated using COCOMO II: `Effort = 2.94 × (KLOC)^1.0997`. Minimum 20 minutes.

**Code Context Switching**: Cognitive overhead during code writing: `COCOMO hours × 0.2 × sqrt(KLOC)`. Based on Microsoft interruption research (Czerwinski et al., 2004).

**GitHub Time**: Session-based calculation for GitHub interactions (commits, comments).

**GitHub Context Switching**: 20 minutes to context in, 20 minutes to context out per session.

### Participant Costs

**Review Cost**: COCOMO II estimation applied to review comments.

**GitHub Time**: Session-based calculation for GitHub interactions.

**GitHub Context Switching**: Same 20-minute in/out costs per session.

### Delay Costs

**Project Delay (20%)**: Opportunity cost of blocked engineer time: `hourly_rate × duration_hours × 0.20`.

**Code Updates**: Rework cost from code drift. Power-law formula: `driftMultiplier = 1 + (0.03 × days^0.7)`, calibrated to 4% weekly churn. Applies to PRs open 3+ days, capped at 90 days. Based on Windows Vista analysis (Nagappan et al., Microsoft Research, 2008).

**Future GitHub**: Cost for 3 future events (push, review, merge) with full context switching.

External contributors (no write access) receive 50% delay cost reduction.

## Session-Based Time Tracking

Events within 60 minutes are grouped into sessions to model real work patterns. This preserves flow state during continuous work and applies context switching costs only when work is interrupted.

**Example (three events 5 min apart, one session)**:
- Event 1: 20 (in) + 20 (event) + 5 (gap) = 45 min
- Event 2: 5 (gap) + 20 (event) + 5 (gap) = 30 min
- Event 3: 5 (gap) + 20 (event) + 20 (out) = 45 min
- Total: 120 minutes

**Example (two events 90 min apart, two sessions)**:
- Event 1: 20 + 20 + 20 = 60 min
- Event 2: 20 + 20 + 20 = 60 min
- Total: 120 minutes

## License

Apache 2.0
