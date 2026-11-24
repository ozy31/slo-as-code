package rules

import (
	"fmt"
	"maps"
	"time"

	"github.com/ozy31/slo-as-code/internal/spec"
)

// BurnRateWindow is one row of the multi-window, multi-burn-rate table from
// the Google SRE Workbook ("Alerting on SLOs"). An alert fires when both the
// long and the short window burn faster than the threshold: the long window
// proves the problem is significant, the short window proves it is still
// happening, so the alert resets within minutes of recovery.
type BurnRateWindow struct {
	Class          string        // "page" or "ticket"
	Long, Short    time.Duration // evaluation windows
	BudgetFraction float64       // share of the whole budget spent in Long
	For            time.Duration // extra hold time against flapping
}

// BurnRateWindows reproduces the workbook's recommended settings.
var BurnRateWindows = []BurnRateWindow{
	{Class: "page", Long: time.Hour, Short: 5 * time.Minute, BudgetFraction: 0.02, For: 2 * time.Minute},
	{Class: "page", Long: 6 * time.Hour, Short: 30 * time.Minute, BudgetFraction: 0.05, For: 15 * time.Minute},
	{Class: "ticket", Long: 24 * time.Hour, Short: 2 * time.Hour, BudgetFraction: 0.10, For: time.Hour},
	{Class: "ticket", Long: 3 * 24 * time.Hour, Short: 6 * time.Hour, BudgetFraction: 0.10, For: 3 * time.Hour},
}

// BurnRate converts a budget fraction into a burn-rate multiplier for a
// given compliance window. For 30 days: 2% in 1h -> 14.4, 5% in 6h -> 6,
// 10% in 1d -> 3, 10% in 3d -> 1. The table scales to 28d or any window.
func (b BurnRateWindow) BurnRate(window time.Duration) float64 {
	return b.BudgetFraction * float64(window) / float64(b.Long)
}

func alertingGroup(f *spec.File, s spec.SLO, labels map[string]string) (Group, bool) {
	if s.Alerting.Disabled {
		return Group{}, false
	}
	g := Group{Name: fmt.Sprintf("slo:%s:%s:alerts", f.Service, s.Name)}
	for _, class := range []string{"page", "ticket"} {
		sev := s.Alerting.Page
		if class == "ticket" {
			sev = s.Alerting.Ticket
		}
		// A class is generated only when it is configured, so a batch job
		// can opt out of paging while still getting tickets.
		if sev.Labels == nil {
			continue
		}
		var conds []string
		var desc string
		for _, w := range BurnRateWindows {
			if w.Class != class {
				continue
			}
			rate := w.BurnRate(s.Window.Duration)
			threshold := formatFloat(rate * s.ErrorBudget())
			conds = append(conds, fmt.Sprintf(
				"(\n  %s%s > %s\n  and\n  %s%s > %s\n)",
				ratioMetric(w.Long), selector(labels), threshold,
				ratioMetric(w.Short), selector(labels), threshold,
			))
			desc += fmt.Sprintf("%.4gx over %s/%s, ", rate, FormatDuration(w.Long), FormatDuration(w.Short))
		}
		alertLabels := maps.Clone(labels)
		maps.Copy(alertLabels, s.Alerting.Labels)
		maps.Copy(alertLabels, sev.Labels)
		alertLabels["slo_alert"] = class

		ann := map[string]string{
			"summary": fmt.Sprintf("%s %s SLO is burning its error budget too fast", f.Service, s.Name),
			"description": fmt.Sprintf(
				"Objective %.4g%% over %s. Fires on burn rates of %s"+
					"error ratio over the long window: {{ $value | humanizePercentage }}.",
				s.Objective, FormatDuration(s.Window.Duration), desc),
		}
		if s.Alerting.Runbook != "" {
			ann["runbook_url"] = s.Alerting.Runbook
		}
		g.Rules = append(g.Rules, Rule{
			Alert:       alertName(f.Service, s.Name, class),
			Expr:        joinOr(conds),
			For:         FormatDuration(shortestFor(class)),
			Labels:      alertLabels,
			Annotations: ann,
		})
	}
	if sev := s.Alerting.Exhausted; sev.Labels != nil {
		alertLabels := maps.Clone(labels)
		maps.Copy(alertLabels, s.Alerting.Labels)
		maps.Copy(alertLabels, sev.Labels)
		alertLabels["slo_alert"] = "exhausted"
		ann := map[string]string{
			"summary": fmt.Sprintf("%s %s SLO has spent its whole error budget", f.Service, s.Name),
			"description": fmt.Sprintf(
				"Objective %.4g%% over %s is missed: {{ $value | humanizePercentage }} of the budget remains. "+
					"Prioritise reliability work over new releases until it recovers.",
				s.Objective, FormatDuration(s.Window.Duration)),
		}
		if s.Alerting.Runbook != "" {
			ann["runbook_url"] = s.Alerting.Runbook
		}
		g.Rules = append(g.Rules, Rule{
			Alert: camel(f.Service) + camel(s.Name) + "ErrorBudgetExhausted",
			Expr:  fmt.Sprintf("slo:error_budget_remaining:ratio%s < 0", selector(labels)),
			// Long enough that a budget hovering around zero does not flap.
			For:         "15m",
			Labels:      alertLabels,
			Annotations: ann,
		})
	}
	return g, len(g.Rules) > 0
}

func shortestFor(class string) time.Duration {
	var d time.Duration
	for _, w := range BurnRateWindows {
		if w.Class == class && (d == 0 || w.For < d) {
			d = w.For
		}
	}
	return d
}

func joinOr(conds []string) string {
	out := ""
	for i, c := range conds {
		if i > 0 {
			out += "\nor\n"
		}
		out += c
	}
	return out
}

// alertName builds e.g. CheckoutAvailabilityBudgetBurnPage.
func alertName(service, slo, class string) string {
	return camel(service) + camel(slo) + "BudgetBurn" + camel(class)
}

func camel(s string) string {
	out := []rune{}
	upper := true
	for _, r := range s {
		if r == '-' || r == '_' {
			upper = true
			continue
		}
		if upper && r >= 'a' && r <= 'z' {
			r -= 'a' - 'A'
		}
		upper = false
		out = append(out, r)
	}
	return string(out)
}
