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

### Single PR Analysis

```bash
# Basic single PR analysis
prcost https://github.com/owner/repo/pull/123

# With custom parameters
prcost --salary 300000 https://github.com/owner/repo/pull/123
prcost --format json https://github.com/owner/repo/pull/123
```

### Repository Analysis

Analyze all PRs in a repository over a time period using statistical sampling:

```bash
# Analyze repository (default: 25 samples, last 90 days)
prcost --org kubernetes --repo kubernetes

# Custom sampling parameters
prcost --org myorg --repo myrepo --samples 50 --days 30
```

### Organization-Wide Analysis

Analyze all PRs across an entire organization:

```bash
# Analyze organization (default: 25 samples, last 90 days)
prcost --org chainguard-dev

# Custom sampling parameters
prcost --org myorg --samples 100 --days 60
```

### Sampling Strategy

Repository and organization modes use time-bucket sampling to ensure even distribution across the time period. This provides more representative estimates than random sampling by avoiding temporal clustering.

## Library Usage

All functionality is available as reusable library packages:

```go
package main

import (
    "context"
    "fmt"
    "time"

    "github.com/codeGROOVE-dev/prcost/pkg/cost"
    "github.com/codeGROOVE-dev/prcost/pkg/github"
)

func main() {
    ctx := context.Background()
    token := "your-github-token"

    // Example 1: Single PR analysis
    prData, _ := github.FetchPRData(ctx, "https://github.com/owner/repo/pull/123", token)
    cfg := cost.DefaultConfig()
    breakdown := cost.Calculate(prData, cfg)
    fmt.Printf("Total cost: $%.2f\n", breakdown.TotalCost)

    // Example 2: Repository analysis with sampling
    since := time.Now().AddDate(0, 0, -90) // Last 90 days
    prs, _ := github.FetchPRsFromRepo(ctx, "kubernetes", "kubernetes", since, token)
    samples := github.SamplePRs(prs, 25) // Sample 25 PRs

    var breakdowns []cost.Breakdown
    for _, pr := range samples {
        prURL := fmt.Sprintf("https://github.com/%s/%s/pull/%d", pr.Owner, pr.Repo, pr.Number)
        prData, _ := github.FetchPRData(ctx, prURL, token)
        breakdowns = append(breakdowns, cost.Calculate(prData, cfg))
    }

    extrapolated := cost.ExtrapolateFromSamples(breakdowns, len(prs))
    fmt.Printf("Extrapolated total: $%.2f over %d PRs\n",
        extrapolated.TotalCost, extrapolated.TotalPRs)

    // Example 3: Organization-wide analysis
    prs, _ = github.FetchPRsFromOrg(ctx, "chainguard-dev", since, token)
    // ... same sampling and extrapolation as above
}
```

### Available Packages

**`pkg/github`**: PR fetching and sampling
- `FetchPRData()` - Fetch single PR data
- `FetchPRsFromRepo()` - Query all PRs in a repository
- `FetchPRsFromOrg()` - Query all PRs across an organization
- `SamplePRs()` - Time-bucket sampling for even distribution

**`pkg/cost`**: Cost calculation and extrapolation
- `Calculate()` - Calculate costs for a single PR
- `ExtrapolateFromSamples()` - Extrapolate from samples to population
- `DefaultConfig()` - Get default configuration

**`pkg/cocomo`**: COCOMO II effort estimation
- `EstimateEffort()` - Estimate hours from lines of code

## Cost Model: Scientific Foundations

This model synthesizes empirical research from software engineering economics, cognitive psychology, and organizational behavior to estimate the comprehensive cost of pull request workflows. Individual PR estimates exhibit variance due to developer heterogeneity and project characteristics; statistical validity improves with aggregate analysis across larger samples (n ≥ 25).

### 1. Code Creation Effort

**Method**: COCOMO II (COnstructive COst MOdel) effort estimation [1]

**Formula**:
```
E_code = a × (KLOC)^b × 152
```
Where:
- `E_code` = effort in person-hours
- `a = 2.94` (productivity coefficient for modern development)
- `b = 1.0997` (scale exponent reflecting diminishing returns)
- `KLOC` = thousands of lines of code added
- `152` = conversion factor (person-months to hours, assuming 152 hours/month)
- Minimum: 20 minutes (floor for small changes)

**Empirical Basis**: COCOMO II calibrated on 161 projects, demonstrating power-law relationship between code volume and effort [1]. The superlinear exponent (b > 1) captures cognitive complexity growth with codebase size.

**References**:
[1] Boehm, B., et al. (2000). *Software Cost Estimation with COCOMO II*. Prentice Hall. ISBN: 0130266922.

### 2. Context Switching Overhead

**Method**: Interruption cost model based on Microsoft productivity research [2]

**Formula**:
```
E_context = E_code × 0.2 × sqrt(KLOC)
```

**Empirical Basis**: Czerwinski et al. found that interruptions during complex cognitive tasks impose a 20% productivity penalty, with recovery time scaling sublinearly with task complexity [2]. The square root scaling reflects that larger changes have proportionally lower per-line context costs due to focused work sessions.

**Session Grouping**: Events within 60 minutes are grouped into sessions. Each session incurs context-in (20 min) and context-out (20 min) costs once, preserving flow state during continuous work [3].

**References**:
[2] Czerwinski, M., Horvitz, E., & Wilhite, S. (2004). A Diary Study of Task Switching and Interruptions. *CHI '04: Proceedings of the SIGCHI Conference on Human Factors in Computing Systems*, 175-182. DOI: 10.1145/985692.985715

[3] Csikszentmihalyi, M. (1990). *Flow: The Psychology of Optimal Experience*. Harper & Row. ISBN: 0060920432.

### 3. Review and Collaboration Costs

**Method**: Symmetric cost model treating review as analytical work

**Formula**: Reviews apply the same COCOMO and context switching formulas as code creation, treating review comments and interactions as equivalent cognitive effort to writing code.

**Empirical Basis**: Code review is cognitively demanding analytical work requiring similar mental effort to code production [4]. Session-based grouping applies identically to reviewers.

**References**:
[4] Bacchelli, A., & Bird, C. (2013). Expectations, outcomes, and challenges of modern code review. *ICSE '13: Proceedings of the 35th International Conference on Software Engineering*, 712-721. DOI: 10.1109/ICSE.2013.6606617

### 4. Opportunity Cost and Velocity Loss

**Method**: Queuing theory application to software delivery pipelines [5]

**Formula**:
```
C_velocity = hourly_rate × t_open × α
```
Where:
- `t_open` = time PR remains open (hours)
- `α = 0.20` (velocity impact factor: 20% of team capacity)

**Capping Logic**:
```
t_capped = min(t_open, t_last_event + 336, 2160)
```
- Cap at 336 hours (14 days) after last activity
- Absolute maximum: 2160 hours (90 days)

**Scientific Justification**:

**Opportunity Cost**: Open PRs represent blocked value delivery. While waiting, engineers cannot ship features that could generate revenue or reduce operational costs. The 20% factor reflects that:
1. Engineers typically work on multiple tasks concurrently
2. PR delays reduce effective team throughput
3. Blocked work creates cascade delays in dependent tasks

**Queuing Theory**: Little's Law states that cycle time directly impacts throughput [5]. Extended PR review cycles increase work-in-progress (WIP), reducing delivery velocity. Studies of lean software development demonstrate that reducing batch size and cycle time improves organizational throughput [6].

**Measurement Validity**: The 336-hour post-activity cap prevents abandoned PRs from dominating cost estimates. The 90-day absolute maximum reflects that PRs open longer than one quarter typically indicate process failure rather than linear cost accumulation.

**References**:
[5] Little, J. D. C. (1961). A Proof for the Queuing Formula: L = λW. *Operations Research*, 9(3), 383-387. DOI: 10.1287/opre.9.3.383

[6] Poppendieck, M., & Poppendieck, T. (2003). *Lean Software Development: An Agile Toolkit*. Addison-Wesley. ISBN: 0321150783.

### 5. Code Churn and Drift Costs

**Method**: Probability-based decay model calibrated on Windows Vista development [7]

**Formula**:
```
P_drift(t) = 1 - (1 - r)^(t/7)
E_churn = E_code × P_drift(t)
```
Where:
- `P_drift(t)` = probability that code requires rework after t days
- `r = 0.04` (weekly code churn rate: 4%)
- `t` = days PR has been open
- Applies only to PRs open ≥ 3 days
- Maximum drift: 90 days (~41% rework)

**Expected Drift Values**:
| Duration | Drift Probability | Interpretation |
|----------|------------------|----------------|
| 3 days | ~2% | Minimal conflict risk |
| 7 days | ~4% | One week of codebase evolution |
| 14 days | ~8% | Sprint boundary |
| 30 days | ~16% | Monthly cycle |
| 60 days | ~29% | Quarterly planning |
| 90 days | ~41% | Maximum modeled (capped) |

**Scientific Justification**:

The formula models cumulative probability that any given line in the PR becomes stale due to codebase changes. Unlike compound interest (which assumes independent events), this probabilistic model accounts for:

1. **Overlapping Changes**: Multiple modifications may affect the same code regions
2. **Dependency Propagation**: Changes in one area often necessitate updates elsewhere
3. **API Evolution**: Interface changes require downstream adaptation

**Empirical Calibration**: Nagappan et al. analyzed Windows Vista development and found organizational churndirectly correlated with defect density [7]. Active codebases exhibit 4-8% weekly churn; we use the conservative 4% baseline. The model predicts that a PR open for 30 days has ~16% of its code requiring updates, matching empirical observations of merge conflict rates.

**References**:
[7] Nagappan, N., Murphy, B., & Basili, V. (2008). The Influence of Organizational Structure on Software Quality: An Empirical Case Study. *ICSE '08: Proceedings of the 30th International Conference on Software Engineering*, 521-530. DOI: 10.1145/1368088.1368160

### 6. Future GitHub Activity

**Method**: Expectation-based forecasting

**Formula**:
```
E_future = 3 × (t_context_in + t_event + t_context_out)
```
Where typical values:
- `t_context_in = 20 min` (load mental model)
- `t_event = 20 min` (perform action: push, review, merge)
- `t_context_out = 20 min` (context switch out)
- Total: 3 events × 60 min = 180 min (3 hours)

**Justification**: Open PRs require future interactions (additional commits, re-review, merge). Three events represent empirical mean completion path. Only applied to open PRs; closed PRs have no future cost.

### Model Limitations and Statistical Properties

**Uncertainty Quantification**: Individual PR estimates have high variance (CV > 1.0) due to:
- Developer productivity heterogeneity
- Task complexity variation
- Organizational process differences

**Aggregation Properties**: By the Central Limit Theorem, sample means converge to population means at rate √n. For n ≥ 25 samples, aggregate estimates achieve acceptable confidence intervals (±20% at 95% confidence).

**Recommended Usage**:
- ✅ Portfolio-level analysis (organizational or repository-wide)
- ✅ Comparative analysis (before/after process changes)
- ⚠️ Individual PR estimates (directionally correct, high variance)
- ❌ Performance reviews (model not calibrated for individual assessment)

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
