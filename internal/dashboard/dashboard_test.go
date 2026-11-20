package dashboard

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ozy31/slo-as-code/internal/spec"
)

func TestRenderCheckout(t *testing.T) {
	f, err := spec.Load("../../examples/checkout.yaml")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := Render(f)
	if err != nil {
		t.Fatal(err)
	}
	var d struct {
		UID    string `json:"uid"`
		Panels []struct {
			ID      int    `json:"id"`
			Type    string `json:"type"`
			Targets []struct {
				Expr string `json:"expr"`
			} `json:"targets"`
		} `json:"panels"`
	}
	if err := json.Unmarshal(raw, &d); err != nil {
		t.Fatal(err)
	}
	if d.UID != "slo-checkout" {
		t.Errorf("uid = %s", d.UID)
	}
	// Two SLOs x (row + 2 stats + burn-rate graph).
	if len(d.Panels) != 8 {
		t.Fatalf("got %d panels, want 8", len(d.Panels))
	}
	ids := map[int]bool{}
	for _, p := range d.Panels {
		if ids[p.ID] {
			t.Errorf("duplicate panel id %d", p.ID)
		}
		ids[p.ID] = true
		for _, tg := range p.Targets {
			if !strings.Contains(tg.Expr, `service="checkout"`) || !strings.HasPrefix(tg.Expr, "slo:") && !strings.HasPrefix(tg.Expr, "1 - slo:") {
				t.Errorf("panel %d queries something other than generated series: %s", p.ID, tg.Expr)
			}
		}
	}
}
