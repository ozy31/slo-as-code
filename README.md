# slo-as-code

Service level objectives written as YAML and compiled by `slogen` into:

- Prometheus recording rules for error ratios, burn rates and remaining error budget
- multi-window, multi-burn-rate page and ticket alerts (Google SRE Workbook)
- a `PrometheusRule` custom resource for the Prometheus Operator
- a Grafana dashboard per service

promtool tests check that the alerts **fire** on a fast burn, **open a
ticket but stay quiet** on a slow burn, and **resolve** soon after recovery.

## Example

```yaml
service: checkout
labels:
  team: payments
slos:
  - name: availability
    objective: 99.9
    window: 30d
    sli:
      errors: sum(rate(http_requests_total{job="checkout", code=~"5.."}[$window]))
      total: sum(rate(http_requests_total{job="checkout"}[$window]))
    alerting:
      runbook: https://github.com/ozy31/slo-as-code/blob/main/docs/runbooks/burn-rate.md
      page:
        labels: { severity: critical }
      ticket:
        labels: { severity: warning }
```

```bash
go run ./cmd/slogen validate examples/*.yaml
go run ./cmd/slogen generate -o out/rules examples/*.yaml
go run ./cmd/slogen generate -o out/k8s -format prometheusrule examples/*.yaml
go run ./cmd/slogen dashboard -o out/dashboards examples/*.yaml
go run ./cmd/slogen explain examples/checkout.yaml
go run ./cmd/slogen version
```

Latency SLOs on a Prometheus histogram do not need hand-written queries:

```yaml
  - name: latency
    objective: 99
    sli:
      latency:
        metric: http_request_duration_seconds   # without _bucket / _count
        threshold: "0.3"                         # an existing le bucket
        selector: job="checkout"
```

`slogen explain` shows what the numbers mean before anything ships:

```
checkout / availability: 99.9% over 30d
  error budget: 0.1% of events, or 43m12s of full downtime per window

  class   windows  burn rate  error ratio >  budget gone in
  page    1h/5m    14.4x      1.44%          2.1d
  page    6h/30m   6x         0.6%           5.0d
  ticket  1d/2h    3x         0.3%           10.0d
  ticket  3d/6h    1x         0.1%           30.0d
```

`$window` is replaced with each evaluation window. The generated page alert
for this SLO:

```yaml
- alert: CheckoutAvailabilityBudgetBurnPage
  expr: |-
    (
      slo:sli_error:ratio_rate1h{service="checkout", slo="availability"} > 0.0144
      and
      slo:sli_error:ratio_rate5m{service="checkout", slo="availability"} > 0.0144
    )
    or
    (
      slo:sli_error:ratio_rate6h{service="checkout", slo="availability"} > 0.006
      and
      slo:sli_error:ratio_rate30m{service="checkout", slo="availability"} > 0.006
    )
  for: 2m
```

## How the thresholds are derived

The error budget is `1 - objective` (0.1% for 99.9%). A burn rate of 1 spends
exactly the whole budget over the compliance window. Each alert is defined
by how much of the budget may be spent within its long window:

| Class | Long / short window | Budget spent | Burn rate (30d) | Burn rate (28d) |
| --- | --- | --- | --- | --- |
| page | 1h / 5m | 2% | 14.4 | 13.44 |
| page | 6h / 30m | 5% | 6 | 5.6 |
| ticket | 1d / 2h | 10% | 3 | 2.8 |
| ticket | 3d / 6h | 10% | 1 | 0.93 |

`burn rate = budget spent × window ÷ long window`, and the alert threshold
is `burn rate × error budget`. Deriving the burn rate from the budget fraction
means a 28-day SLO gets correctly scaled thresholds instead of reusing
the 30-day numbers.

The long window proves the problem is big enough to matter. The short
window proves it is still happening, so a page clears within minutes of a
fix rather than staying open for an hour.

## What the tests prove

`test/promtool/checkout_test.yaml` drives the generated rules with
synthetic traffic of 1000 requests per minute:

| Scenario | Error rate | Expected |
| --- | --- | --- |
| Healthy | 0.05% (0.5x) | no alerts, burn rate 0.5 |
| Fast burn | 2% (20x) | quiet at 1 min, page by 10 min |
| Slow burn | 0.4% (4x) | ticket, no page |
| Recovery | 2% for 30 min, then 0 | page while failing; resolved 30 min later although the 6h ratio is still above its threshold |
| Slow responses | 5% over 300 ms (5x on the 99% latency SLO) | latency ticket, no page |
| Budget exhausted | 2% for an hour | no alert at 10 min, `ErrorBudgetExhausted` after its 15 min hold |

Go unit tests cover spec validation (every error is reported, unknown keys
are rejected), the latency shorthand, the burn-rate table, window
substitution and the dashboard. Golden files in `cmd/slogen/testdata` pin
the exact generated output, so any change to an expression or threshold
shows up as a diff in review (`go test ./cmd/slogen -update` after an
intended change).

## Validation

`slogen validate` rejects:

- objectives outside (0, 100), windows shorter than a day
- SLI queries that do not use `$window`
- duplicate SLO names, and service or SLO names that are not valid labels
- unknown keys, so `windw: 30d` fails instead of silently using the default
- two files for the same service, because output files are named after the
  service and the second would silently overwrite the first
- `service`, `slo` and `slo_alert` in any label set (slogen sets them, and
  overriding them would break the generated selectors), label names that
  Prometheus would reject, and annotations that replace the generated
  `summary`, `description` or `runbook_url`

## More alerting options

```yaml
    alerting:
      runbook: https://example.com/runbooks/burn-rate
      annotations:
        dashboard_url: https://grafana.example.com/d/slo-checkout
      page:
        labels: { severity: critical }
      ticket:
        labels: { severity: warning }
      exhausted:            # the SLO is already missed for this window
        labels: { severity: warning }
```

`exhausted` adds `<Service><Slo>ErrorBudgetExhausted`, which fires when
`slo:error_budget_remaining:ratio` stays below zero for 15 minutes. It is
the signal for an error budget policy (freeze risky releases), not a page.

For the Prometheus Operator, `-namespace` sets `metadata.namespace` on the
generated `PrometheusRule`:

```bash
slogen generate -format prometheusrule -namespace monitoring -o out/k8s specs/*.yaml
```

## Committing generated rules

If the rule files are committed next to the specs (for GitOps, or so a
reviewer sees the PromQL), add this to CI:

```bash
slogen generate -check -o deploy/rules specs/*.yaml
```

It writes nothing and fails with the list of files that are missing or no
longer match their spec, so a spec change cannot be merged without the
regenerated rules, and a hand edit to a generated file is caught.

## Generated series

| Series | Meaning |
| --- | --- |
| `slo:sli_error:ratio_rate<w>` | bad / total events over window `w` |
| `slo:burn_rate:<w>` | error ratio ÷ error budget |
| `slo:error_budget_remaining:ratio` | 1 = untouched, 0 = spent, below 0 = SLO missed |
| `slo:objective:ratio`, `slo:error_budget:ratio` | constants, for dashboards and joins |

All series carry `service`, `slo` and the file's `labels`. The rule files
use the standard Prometheus format, which vmalert (VictoriaMetrics) also reads.

## Development

```bash
make test        # go vet, go test, promtool check, promtool test
make dashboards  # out/dashboards/*.json
```

Requirements: Go and promtool (from the Prometheus release). Tagged
versions publish `slogen` binaries for Linux and macOS with checksums.

## License

MIT
