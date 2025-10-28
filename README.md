# prcost

Calculate the real-world cost of GitHub pull requests using empirically-validated software engineering research. Provides detailed breakdowns of development effort, review costs, coordination overhead, and delay impacts.

## Hosted Web Interface

Try https://cost.github.codegroove.app/ - it only has access to public repositories, so if you need to take that account for accuracy, run prcost locally.

## CLI Example

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

## Installation

```bash
go install github.com/codeGROOVE-dev/prcost/cmd/prcost@latest
```

## Usage

```bash
# Single PR analysis
prcost https://github.com/owner/repo/pull/123
prcost --salary 300000 https://github.com/owner/repo/pull/123

# Repository analysis (samples 30 PRs from last 90 days)
prcost --org kubernetes --repo kubernetes
prcost --org myorg --repo myrepo --samples 50 --days 30

# Organization-wide analysis
prcost --org chainguard-dev --samples 50 --days 60
```

### Web Interface

```bash
go run ./cmd/server
```

### Sampling Strategy

Repository and organization modes use time-bucket sampling to ensure even distribution across the time period, avoiding temporal clustering that would bias estimates.

- **30 samples** (default): Fast analysis with ±18% confidence interval
- **50 samples**: More accurate with ±14% confidence interval (1.3× better precision)

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
