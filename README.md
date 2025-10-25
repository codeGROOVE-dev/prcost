# prcost

Calculate the real-world cost of GitHub pull requests with detailed breakdowns of author effort, participant contributions, and delay costs. 

## Example

```
$ prcost https://github.com/chainguard-dev/apko/pull/1860

  https://github.com/chainguard-dev/apko/pull/1860
  Rate: $155.71/hr  •  Salary: $249,000.00  •  Benefits: 1.3x

  Author
  ──────
    Development Effort           $7,531.93    132 LOC • 2.0 days
    GitHub Activity                $156.25    2 sessions • 1.0 hrs
    GitHub Context Switching       $208.33    1.3 hrs
                              ────────────
    Subtotal                     $7,896.51    2.1 days

  Participants
  ────────────
    philroche
      Review Activity               $75.00    1 sessions • 29 min
      Context Switching            $104.17    40 min
    justinvreeland
      Review Activity               $75.00    1 sessions • 29 min
      Context Switching            $104.17    40 min
                              ────────────
    Subtotal                       $358.33    2.3 hrs

  Merge Delay
  ───────────
    Delivery                  $9,481.36    2.5 days (capped)
    Coordination              $3,160.45    20.2 hrs (capped)
                              ────────────
    Subtotal                  $12,641.81    3.4 days

  Future Costs
  ────────────
    Code Churn (18% drift)    $1,155.39    7.4 hrs
    Review                        $75.00    29 min
    Merge                         $52.08    20 min
    Context Switching            $208.33    1.3 hrs
                              ────────────
    Subtotal                   $1,490.81    9.5 hrs

  ═══════════════════════════════════════════════════════════════
  Total                         $22,387.47    6.0 days

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
prcost --org myorg --samples 50 --days 60
```

### Sampling Strategy

Repository and organization modes use time-bucket sampling to ensure even distribution across the time period. This provides more representative estimates than random sampling by avoiding temporal clustering.

**Sample Size Options:**
- **25 samples** (default): Fast analysis with ±20% confidence interval
- **50 samples**: Slower but more accurate with ±14% confidence interval (1.4× better accuracy)

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

**Default Salary**: The model uses $249,000 as the default annual salary, based on the 2025 average for Staff Software Engineers per [Glassdoor](https://www.glassdoor.com/Salaries/staff-software-engineer-salary-SRCH_KO0,23.htm). This can be customized via the `--salary` flag or API configuration.

### 1. Development Effort

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

**Small PR Behavior**: COCOMO II's Post-Architecture Model was designed for projects with established architectures, with a recommended minimum of 2,000 SLOC (2 KSLOC). For small modifications (< 2 KSLOC), the model generates disproportionately large cost estimates relative to "typing time" because it includes overhead activities:

1. **Understanding existing code**: Comprehending the surrounding codebase and architecture
2. **Interface checking**: Validating compatibility with existing interfaces and contracts
3. **Testing**: Writing and running tests to ensure correctness
4. **Integration**: Ensuring the change integrates properly with the broader system
5. **Assessment overhead**: Baseline ~5% cost for assimilation and design checking

For example, a 9-line PR may show 2.5 hours of effort—not because typing 9 lines takes 2.5 hours, but because the complete software development activity (understanding context, writing tests, reviewing, fixing CI) requires this time. This overhead is inherent to professional software development and is intentionally captured by COCOMO II's model.

**References**:
[1] Boehm, B., et al. (2000). *Software Cost Estimation with COCOMO II*. Prentice Hall. ISBN: 0130266922.

### 2. Review and Collaboration Costs

**Method**: Inspection-rate based model using IEEE/Fagan standards

**Formula**:
```
t_review = LOC / inspection_rate
```
Where:
- `LOC` = lines of code added in the PR
- `inspection_rate` = 275 LOC/hour (default, configurable)

**Empirical Basis**: Code review inspection rates have been extensively studied and show optimal defect detection at 150-400 LOC/hour [2, 3]. The default 275 LOC/hour represents the average of this optimal range.

**Session Grouping**: Review events within 60 minutes are grouped into sessions. Each session incurs context-in (20 min) and context-out (20 min) costs once, preserving flow state during continuous work [4].

**References**:
[2] Fagan, M. E. (1976). Design and Code Inspections to Reduce Errors in Program Development. *IBM Systems Journal*, 15(3), 182-211.

[3] IEEE Std 1028-2008: IEEE Standard for Software Reviews and Audits

[4] Czerwinski, M., Horvitz, E., & Wilhite, S. (2004). A Diary Study of Task Switching and Interruptions. *CHI '04: Proceedings of the SIGCHI Conference on Human Factors in Computing Systems*, 175-182. DOI: 10.1145/985692.985715

### 3. Delay Costs: Opportunity Cost and Coordination Overhead

**Method**: Split-component model separating tangible opportunity cost from intangible cognitive overhead

**Formula**:
```
C_delay = C_delivery + C_coordination + C_churn + C_future
```

**3a. Project Delivery Delay (15%)**

Captures the opportunity cost of blocked value delivery.

**Formula**:
```
C_delivery = hourly_rate × t_capped × 0.15
```
Where:
- `t_capped` = capped PR duration (see capping logic below)
- `0.15` = 15% delivery impact factor

**Scientific Justification**:

**Opportunity Cost**: Open PRs represent blocked value that cannot generate revenue or reduce operational costs. The 15% factor reflects that:
1. Engineers work on multiple tasks concurrently, so a single PR doesn't block 100% of capacity
2. PR delays reduce effective team throughput via queuing effects
3. Blocked work creates cascade delays in dependent features

**Queuing Theory**: Little's Law (L = λW) states that cycle time directly impacts throughput [5]. Extended PR review cycles increase work-in-progress (WIP), reducing delivery velocity. Lean software development research demonstrates that reducing batch size and cycle time improves organizational throughput [6].

**References**:
[5] Little, J. D. C. (1961). A Proof for the Queuing Formula: L = λW. *Operations Research*, 9(3), 383-387. DOI: 10.1287/opre.9.3.383

[6] Poppendieck, M., & Poppendieck, T. (2003). *Lean Software Development: An Agile Toolkit*. Addison-Wesley. ISBN: 0321150783.

**3b. Coordination Overhead (5%)**

Captures the mental and cognitive load of tracking unmerged work.

**Formula**:
```
C_coordination = hourly_rate × t_capped × 0.05
```
Where:
- `t_capped` = capped PR duration (see capping logic below)
- `0.05` = 5% coordination overhead factor

**Scientific Justification**:

**Working Memory Burden**: Each unmerged PR consumes limited working memory capacity. Developers must mentally track the PR's current state, potential merge conflicts, dependencies, and communication with reviewers. Cognitive Load Theory shows human working memory is limited to 7±2 items [7], making this tracking overhead measurable.

**Context Retention Costs**: Weinberg's research on programmer productivity shows 20-25% productivity loss from maintaining multiple mental contexts [8]. The 5% coordination factor captures the cost of "keeping tabs" on pending work rather than the cost of full context switches (which are accounted separately).

**Team Communication Overhead**: Brooks' Law demonstrates that communication overhead grows with project complexity [9]. Unmerged PRs increase coordination burden as team members must track dependencies and potential conflicts across the team.

**References**:
[7] Sweller, J., van Merriënboer, J. J., & Paas, F. (1998). Cognitive Architecture and Instructional Design. *Educational Psychology Review*, 10(3), 251-296. DOI: 10.1023/A:1022193728205

[8] Weinberg, G. M. (1992). *Quality Software Management: Vol. 1, Systems Thinking*. Dorset House. ISBN: 0932633226.

[9] Brooks, F. P. (1975). *The Mythical Man-Month: Essays on Software Engineering*. Addison-Wesley. ISBN: 0201835959.

**Capping Logic** (applies to both components):
```
t_capped = min(t_open, t_last_event + 336, 2160)
```
- Cap at 336 hours (14 days) after last activity
- Absolute maximum: 2160 hours (90 days)

**Measurement Validity**: The 336-hour post-activity cap prevents abandoned PRs from dominating cost estimates. The 90-day absolute maximum reflects that PRs open longer than one quarter typically indicate process failure rather than linear cost accumulation.

### 4. Code Churn and Drift Costs

**Method**: Probability-based decay model calibrated on Windows Vista development [10]

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

**Empirical Calibration**: Nagappan et al. analyzed Windows Vista development and found organizational churn directly correlated with defect density [10]. Active codebases exhibit 4-8% weekly churn; we use the conservative 4% baseline. The model predicts that a PR open for 30 days has ~16% of its code requiring updates, matching empirical observations of merge conflict rates.

**References**:
[10] Nagappan, N., Murphy, B., & Basili, V. (2008). The Influence of Organizational Structure on Software Quality: An Empirical Case Study. *ICSE '08: Proceedings of the 30th International Conference on Software Engineering*, 521-530. DOI: 10.1145/1368088.1368160

### 5. Future GitHub Activity

**Method**: Research-based forecasting using IEEE/Fagan inspection rates

For open PRs, future costs are estimated across three components:

**5a. Future Review Time**

Based on empirical code review inspection rates from IEEE standards and Fagan inspection methodology.

**Formula**:
```
t_review = LOC / inspection_rate
```
Where:
- `LOC` = lines of code added in the PR
- `inspection_rate` = 275 LOC/hour (default, configurable)

**Empirical Basis**:

Code review inspection rates have been extensively studied:
- **Fagan inspection** (thorough): ~22 LOC/hour [11]
- **Industry standard**: 150-200 LOC/hour [12]
- **Fast/lightweight**: up to 400 LOC/hour [12]
- **Average**: 275 LOC/hour (midpoint of 150-400 optimal range)

Research shows that inspecting more than 400 LOC/hour results in significantly reduced defect detection effectiveness. The default 275 LOC/hour represents the average of the optimal range.

**Application**: This inspection rate is used consistently for both past and future reviews, ensuring size-appropriate estimates regardless of PR complexity.

**Example**:
- 649 LOC PR at 275 LOC/hour = 2.4 hours review time
- 9 LOC PR at 275 LOC/hour = 2 minutes review time

**References**:
[11] Fagan, M. E. (1976). Design and Code Inspections to Reduce Errors in Program Development. *IBM Systems Journal*, 15(3), 182-211.

[12] IEEE Std 1028-2008: IEEE Standard for Software Reviews and Audits

**5b. Future Merge Time**

**Formula**:
```
t_merge = 1 × t_event
```
Where `t_event = 20 min` (default)

**Justification**: Merging requires reviewing final state, resolving any conflicts, and executing the merge operation.

**5c. Future Context Switching**

**Formula**:
```
t_context = 2 × (t_context_in + t_context_out)
```
Where:
- `t_context_in = 20 min` (load mental model)
- `t_context_out = 20 min` (save mental model)
- 2 sessions: 1 for reviewer, 1 for author merge
- Total: 2 × 40 min = 80 min (1.33 hours)

**Justification**: Based on Microsoft Research findings that context switches require ~20 minutes to restore working memory [4].

**Total Future Cost Example** (649 LOC PR):
- Review: 2.4 hours (size-dependent, 649 / 275)
- Merge: 0.33 hours (fixed)
- Context: 1.33 hours (fixed)
- **Total: 4.1 hours**

**Note**: Only applied to open PRs; closed PRs have no future cost.

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
