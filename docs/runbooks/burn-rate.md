# Runbook: error budget burn alerts

Linked from every `*BudgetBurnPage` and `*BudgetBurnTicket` alert.

## What the alert means

The service is failing more requests than its SLO allows, fast enough that
the error budget for the compliance window will run out early.

| Alert | Burn rate (30d window) | Budget gone in | Response |
| --- | --- | --- | --- |
| Page, 1h/5m | 14.4x | about 2 days | Act now |
| Page, 6h/30m | 6x | about 5 days | Act now |
| Ticket, 1d/2h | 3x | about 10 days | Next working day |
| Ticket, 3d/6h | 1x | end of the window | Plan a fix |

Both windows of a pair must be over the threshold, so the alert only fires
while errors are still happening, and resolves within minutes of recovery.

## First five minutes

1. Open the service's SLO dashboard (`SLOs / <service>` in Grafana). The
   burn-rate graph shows which window crossed which threshold.
2. Check what changed: recent deploys, config or feature-flag changes,
   upstream incidents.
3. Look at the SLI query from the spec to see which requests are failing
   (status code, route, region).
4. If a deploy lines up with the start, roll it back first and investigate
   after.

## Useful queries

```promql
# Current burn rate per window
slo:burn_rate:5m{service="<service>", slo="<slo>"}
slo:burn_rate:1h{service="<service>", slo="<slo>"}

# Budget left in the compliance window (1 = untouched, below 0 = SLO missed)
slo:error_budget_remaining:ratio{service="<service>", slo="<slo>"}
```

## After the incident

- If the budget is below 25%, freeze risky changes until it recovers, as
  the team's error budget policy defines.
- If the alert fired but nobody needed to act, tune the SLO or the SLI, not
  the alert thresholds. The thresholds come from the window table and should
  stay consistent across services.
