# prcost

Calculate the real-world cost of GitHub pull requests using empirically-validated software engineering research. Measures at a PR, repo, or organization level.

## Installation

Local installation, which authenticates using the GitHub command-line (`gh`) or GITHUB_TOKEN:

```
go install github.com/codeGROOVE-dev/prcost/cmd/prcost@latest
```

Alternatively, you can use our hosted version: https://cost.github.codegroove.app/ - but it only has access to public repositories.

## Usage

CLI

```
% prcost --org exera-dev

Analyzing 30 sampled PRs from 91 total PRs (50 human, 41 bot) across exera-dev (last 60 days)...

  exera-dev (organization)
  Period: Last 60 days  •  Total PRs: 91 (33 human, 57 bot)  •  Authors: 10  •  Sampled: 30
  Avg Open Time: 2.4w (human: 8.7h, bot: 3.7w)

  ┌─────────────────────────────────────────────────────────────┐
  │ Average PR (sampled over 60 day period)                     │
  └─────────────────────────────────────────────────────────────┘

  Development Costs (33 PRs, 13 LOC)
  ────────────────────────────────────────
    New Development                $        344.58    2.2h    (7 LOC)
    Adaptation                     $         20.34    7.8m    (5 LOC)
    GitHub Activity                $         66.57    25.7m   (2.6 events)
    Context Switching              $         27.04    10.4m   (0.5 sessions)
    Automated Updates                            —    0.0m    (57 PRs, 5 LOC)
                                ──────────────
    Subtotal                       $        458.54    2.9h    (56.8%)

  Participant Costs
  ─────────────────
    Review Activity                $          5.02    1.9m    (0.7 reviews)
    GitHub Activity                $         37.18    14.3m   (2.4 events)
    Context Switching              $         38.88    15.0m   (0.8 sessions)
                                ──────────────
    Subtotal                       $         81.07    31.3m   (10.0%)

  Delay Costs (human PRs avg 8.7h open, bot PRs avg 3.7w)
  ───────────────────────────────────────────────────────
    Workstream blockage            $         98.09    37.8m   (33 PRs)
    Automated Updates              $        126.77    48.9m   (57 PRs)
    PR Tracking                    $         35.63    13.7m   (25 open PRs)
                                ──────────────
    Subtotal                       $        260.49    1.7h    (32.2%)

  Future Costs
  ────────────
    Review                         $          3.64    1.4m    (24 PRs)
    Merge                          $          6.92    2.7m    (24 PRs)
    Context Switching              $         27.04    10.4m   (0.8 sessions)
                                ──────────────
    Subtotal                       $         37.60    14.5m   (4.7%)

  Preventable Loss Total         $        260.49    1.7h    (32.2%)
  ════════════════════════════════════════════════════
  Average Total                $        807.86    5.2h


  ┌─────────────────────────────────────────────────────────────┐
  │ Estimated costs within a 60 day period (extrapolated)       │
  └─────────────────────────────────────────────────────────────┘

  Development Costs (33 PRs, 1.2k LOC)
  ────────────────────────────────────────
    New Development                $     31,356.75    8.4d    (712 LOC)
    Adaptation                     $      1,850.92    11.9h   (473 LOC)
    GitHub Activity                $      6,058.14    38.9h   (233 events)
    Context Switching              $      2,461.02    15.8h   (48 sessions)
    Automated Updates                            —    0.0m    (57 PRs, 485 LOC)
                                ──────────────
    Subtotal                       $     41,726.82    11.2d   (56.8%)

  Participant Costs
  ─────────────────
    Review Activity                $        456.61    2.9h    (60 reviews)
    GitHub Activity                $      3,383.11    21.7h   (215 events)
    Context Switching              $      3,537.72    22.7h   (69 sessions)
                                ──────────────
    Subtotal                       $      7,377.44    47.4h   (10.0%)

  Delay Costs (human PRs avg 8.7h open, bot PRs avg 3.7w)
  ───────────────────────────────────────────────────────
    Workstream blockage            $      8,926.47    2.4d    (33 PRs)
    Automated Updates              $     11,536.14    3.1d    (57 PRs)
    PR Tracking                    $      3,242.19    20.8h   (25 open PRs)
                                ──────────────
    Subtotal                       $     23,704.80    6.3d    (32.2%)

  Future Costs
  ────────────
    Review                         $        331.30    2.1h    (24 PRs)
    Merge                          $        629.42    4.0h    (24 PRs)
    Context Switching              $      2,461.02    15.8h   (72 sessions)
                                ──────────────
    Subtotal                       $      3,421.74    22.0h   (4.7%)

  Preventable Loss Total         $     23,704.80    6.3d    (32.2%)
  ════════════════════════════════════════════════════
  Total                        $     73,515.44    2.8w

  ┌─────────────────────────────────────────────────────────────┐
  │ DEVELOPMENT EFFICIENCY: D (67.8%) - Not good my friend.     │
  └─────────────────────────────────────────────────────────────┘
  ┌─────────────────────────────────────────────────────────────┐
  │ MERGE VELOCITY: F (2.4w) - Failing                          │
  └─────────────────────────────────────────────────────────────┘
  Weekly waste per PR author:     $        276.56    1.8h  (10 authors)
  If Sustained for 1 Year:        $    144,204.17    0.4 headcount

  ┌─────────────────────────────────────────────────────────────┐
  │ MERGE TIME MODELING                                         │
  └─────────────────────────────────────────────────────────────┘
  If you lowered your average merge time to 1.0h, you would save
  ~$126,616.08/yr in engineering overhead (+28.4% throughput).
```

Web interface:

```bash
go run ./cmd/server
```

## Cost Model: Scientific Foundations

This model synthesizes empirical research from software engineering economics, cognitive psychology, and organizational behavior. Individual PR estimates exhibit variance due to developer heterogeneity; statistical validity improves with aggregate analysis (n ≥ 25).

### 1. Development Effort: COCOMO II

Uses the COnstructive COst MOdel (COCOMO II) calibrated on 161 projects, demonstrating a power-law relationship between code volume and effort:

```
E_code = 2.94 × (KLOC)^1.0997 × 152 hours
```

The superlinear exponent (1.0997 > 1) captures cognitive complexity growth. The model includes overhead activities inherent to professional development: understanding existing code, interface checking, testing, and integration—not just "typing time."

**Reference**: Boehm, B., et al. (2000). *Software Cost Estimation with COCOMO II*. Prentice Hall.

### 2. Review Costs: IEEE Inspection Rates

Based on Fagan inspection methodology and IEEE standards, using an optimal inspection rate of 275 LOC/hour (midpoint of 150-400 LOC/hour range for effective defect detection).

**References**:
- Fagan, M. E. (1976). Design and Code Inspections. *IBM Systems Journal*, 15(3).
- IEEE Std 1028-2008: Standard for Software Reviews and Audits

### 3. Context Switching: Microsoft Research

Events within 60 minutes are grouped into sessions. Each session incurs context-in (20 min) and context-out (20 min) costs, based on Microsoft Research findings on working memory restoration time.

**Reference**: Czerwinski, M., et al. (2004). A Diary Study of Task Switching. *CHI '04*.

### 4. Delay Costs: Queuing Theory & Cognitive Load

Split into two components:

**Delivery Delay (15%)**: Captures opportunity cost of blocked value delivery. Based on Little's Law (L = λW) from queuing theory—cycle time directly impacts throughput.

**Coordination Overhead (5%)**: Mental burden of tracking unmerged work. Limited working memory capacity (7±2 items) makes this cost measurable.

**References**:
- Little, J. D. C. (1961). A Proof for the Queuing Formula. *Operations Research*, 9(3).
- Sweller, J., et al. (1998). Cognitive Architecture. *Educational Psychology Review*, 10(3).

### 5. Code Churn: Probabilistic Drift Model

Models the probability that code requires rework due to codebase evolution:

```
P_drift(t) = 1 - (1 - 0.04)^(t/7)
```

Calibrated on Windows Vista development data showing 4% weekly code churn. A PR open for 30 days has ~16% probability of requiring updates.

**Reference**: Nagappan, N., et al. (2008). Organizational Structure and Software Quality. *ICSE '08*.

## Model Limitations

**Individual Estimates**: High variance (CV > 1.0) due to developer and task heterogeneity.

**Aggregate Estimates**: By the Central Limit Theorem, sample means converge to population means at rate √n. For n ≥ 25, aggregate estimates achieve acceptable confidence intervals.

**Recommended Usage**:
- ✅ Portfolio-level analysis (organizational or repository-wide)
- ✅ Comparative analysis (before/after process changes)
- ⚠️ Individual PR estimates (directionally correct, high variance)
- ❌ Performance reviews (not calibrated for individual assessment)

## License

Apache 2.0
