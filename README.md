# prcost

Calculate the real-world cost of GitHub pull requests with detailed breakdowns of author effort, participant contributions, and delay costs. 

## Example

```
$ prcost https://github.com/chainguard-dev/apko/pull/1860

PULL REQUEST COST ANALYSIS
==========================

PR URL:      https://github.com/chainguard-dev/apko/pull/1860
Hourly Rate: $156.25 ($250000 salary * 1.3X total benefits multiplier)

AUTHOR COSTS
  Code Cost (COCOMO)          $   7531.93   (132 LOC, 48.20 hrs)
  Code Context Switching      $    547.30   (3.50 hrs)
  GitHub Time                 $    161.50   (3 events, 1.03 hrs)
  GitHub Context Switching    $    208.33   (2 sessions, 1.33 hrs)
  ---
  Author Subtotal             $   8449.06   (54.07 hrs total)

PARTICIPANT COSTS
  philroche
    Event Time                $     52.08   (1 events, 0.33 hrs)
    Context Switching         $    104.17   (1 sessions, 0.67 hrs)
    Subtotal                  $    156.25   (1.00 hrs total)
  justinvreeland
    Event Time                $     52.08   (1 events, 0.33 hrs)
    Context Switching         $    104.17   (1 sessions, 0.67 hrs)
    Subtotal                  $    156.25   (1.00 hrs total)
  ---
  Participants Subtotal       $    312.50   (2.00 hrs total)

DELAY COST
  Project Delay (20%)              $   2677.27   (68.54 hrs)
  Future GitHub (3 events)         $    468.75   (3.00 hrs)
  ---
  Total Delay Cost            $   3146.02

==========================
TOTAL COST                  $  11907.58
==========================
```

## Caveats

* Due to limited input, results may not be accurate for single PRs, but should be accurate on average across a larger sample size of PRs
* Real-world project delay costs depend on the revenue importance of the code involved. It attempts to include the opportunity cost, but for high-impact PRs, it will underestimate the true cost of a delay.

## Installation

```bash
go install github.com/codeGROOVE-dev/prcost/cmd/prcost@latest
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

**Project Delay (20% of an engineer's time)**: Opportunity cost of blocked engineer time: `hourly_rate × duration_hours × 0.20`. Capped at 2 weeks after the last event, with an absolute maximum of 90 days.

**Code Updates**: Rework cost from code drift. Probability-based formula: `drift = 1 - (0.96)^(days/7)`, modeling the cumulative probability that code becomes stale with 4% weekly churn. Applies to PRs open 3+ days, capped at 90 days (~41% max drift). Based on Windows Vista analysis (Nagappan et al., Microsoft Research, 2008).

**Future GitHub**: Cost for 3 future events (push, review, merge) with full context switching.

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
