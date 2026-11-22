package rules

import (
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	"github.com/ozy31/slo-as-code/internal/spec"
)

// Explain writes a human-readable summary of what an SLO allows and when
// its alerts fire, so a team can sanity-check the numbers before shipping.
func Explain(w io.Writer, f *spec.File) error {
	for _, s := range f.SLOs {
		budget := s.ErrorBudget()
		allowed := time.Duration(float64(s.Window.Duration) * budget).Round(time.Second)
		fmt.Fprintf(w, "%s / %s: %.4g%% over %s\n", f.Service, s.Name, s.Objective, FormatDuration(s.Window.Duration))
		fmt.Fprintf(w, "  error budget: %.4g%% of events, or %s of full downtime per window\n\n", budget*100, allowed)

		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "  class\twindows\tburn rate\terror ratio >\tbudget gone in")
		for _, b := range BurnRateWindows {
			rate := b.BurnRate(s.Window.Duration)
			exhaust := time.Duration(float64(s.Window.Duration) / rate).Round(time.Hour)
			configured := (b.Class == "page" && s.Alerting.Page.Labels != nil) ||
				(b.Class == "ticket" && s.Alerting.Ticket.Labels != nil)
			class := b.Class
			if s.Alerting.Disabled || !configured {
				class += " (off)"
			}
			fmt.Fprintf(tw, "  %s\t%s/%s\t%.4gx\t%.4g%%\t%s\n", class,
				FormatDuration(b.Long), FormatDuration(b.Short), rate, rate*budget*100, humanDays(exhaust))
		}
		if err := tw.Flush(); err != nil {
			return err
		}
		fmt.Fprintln(w)
	}
	return nil
}

func humanDays(d time.Duration) string {
	days := d.Hours() / 24
	if days < 2 {
		return fmt.Sprintf("%.0fh", d.Hours())
	}
	return fmt.Sprintf("%.1fd", days)
}
