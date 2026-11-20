// Package dashboard renders a Grafana dashboard for a service's SLOs from
// the recording rules that package rules generates.
package dashboard

import (
	"encoding/json"
	"fmt"

	"github.com/ozy31/slo-as-code/internal/rules"
	"github.com/ozy31/slo-as-code/internal/spec"
)

type obj = map[string]any

// Render returns dashboard JSON for one spec file. Each SLO gets a row with
// remaining budget, the SLI over the compliance window, and burn rates over
// the alerting windows with the page and ticket thresholds drawn in.
func Render(f *spec.File) ([]byte, error) {
	var panels []obj
	y := 0
	for i, s := range f.SLOs {
		sel := fmt.Sprintf(`{service="%s", slo="%s"}`, f.Service, s.Name)
		win := rules.FormatDuration(s.Window.Duration)

		panels = append(panels, obj{
			"type": "row", "title": fmt.Sprintf("%s (%.4g%% over %s)", s.Name, s.Objective, win),
			"gridPos": grid(0, y, 24, 1), "id": 100*i + 1, "collapsed": false,
		})
		y++

		panels = append(panels,
			stat(100*i+2, "Error budget remaining", "slo:error_budget_remaining:ratio"+sel, grid(0, y, 6, 6),
				[]obj{{"color": "red", "value": nil}, {"color": "orange", "value": 0.25}, {"color": "green", "value": 0.5}}),
			stat(100*i+3, "SLI ("+win+")", "1 - slo:sli_error:ratio_rate"+win+sel, grid(6, y, 6, 6),
				[]obj{{"color": "red", "value": nil}, {"color": "green", "value": s.Objective / 100}}),
			burnRates(100*i+4, sel, s, grid(12, y, 12, 6)),
		)
		y += 6
	}

	d := obj{
		"title":         fmt.Sprintf("SLOs / %s", f.Service),
		"uid":           "slo-" + f.Service,
		"tags":          []string{"slo", "generated"},
		"timezone":      "utc",
		"schemaVersion": 39,
		"time":          obj{"from": "now-7d", "to": "now"},
		"refresh":       "1m",
		"templating": obj{"list": []obj{{
			"name": "datasource", "type": "datasource", "query": "prometheus", "label": "Data source",
		}}},
		"panels": panels,
	}
	return json.MarshalIndent(d, "", "  ")
}

func grid(x, y, w, h int) obj { return obj{"x": x, "y": y, "w": w, "h": h} }

func target(expr, legend string) obj {
	return obj{"expr": expr, "legendFormat": legend, "refId": legend,
		"datasource": obj{"type": "prometheus", "uid": "${datasource}"}}
}

func stat(id int, title, expr string, pos obj, steps []obj) obj {
	return obj{
		"id": id, "type": "stat", "title": title, "gridPos": pos,
		"datasource": obj{"type": "prometheus", "uid": "${datasource}"},
		"targets":    []obj{target(expr, "A")},
		"fieldConfig": obj{"defaults": obj{
			"unit": "percentunit", "decimals": 3,
			"thresholds": obj{"mode": "absolute", "steps": steps},
		}},
	}
}

func burnRates(id int, sel string, s spec.SLO, pos obj) obj {
	var targets []obj
	var thresholds []obj
	seen := map[string]bool{}
	for _, w := range rules.BurnRateWindows {
		for _, d := range []string{rules.FormatDuration(w.Long), rules.FormatDuration(w.Short)} {
			if !seen[d] {
				seen[d] = true
				targets = append(targets, target("slo:burn_rate:"+d+sel, d))
			}
		}
		thresholds = append(thresholds, obj{"color": map[string]string{"page": "red", "ticket": "orange"}[w.Class],
			"value": w.BurnRate(s.Window.Duration)})
	}
	return obj{
		"id": id, "type": "timeseries", "title": "Burn rate (1 = budget lasts exactly the window)", "gridPos": pos,
		"datasource": obj{"type": "prometheus", "uid": "${datasource}"},
		"targets":    targets,
		"fieldConfig": obj{"defaults": obj{
			"custom":     obj{"thresholdsStyle": obj{"mode": "dashed"}},
			"thresholds": obj{"mode": "absolute", "steps": append([]obj{{"color": "green", "value": nil}}, thresholds...)},
		}},
	}
}
