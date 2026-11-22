package main

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/ozy31/slo-as-code/internal/dashboard"
	"github.com/ozy31/slo-as-code/internal/rules"
	"github.com/ozy31/slo-as-code/internal/spec"
)

var update = flag.Bool("update", false, "rewrite golden files")

// TestGolden pins the exact generated output for the examples. A change in
// a rule expression, label or threshold shows up as a reviewable diff of
// testdata/*.golden instead of slipping through.
//
//	go test ./cmd/slogen -update   # after an intended change
func TestGolden(t *testing.T) {
	examples, err := filepath.Glob("../../examples/*.yaml")
	if err != nil || len(examples) == 0 {
		t.Fatalf("no examples: %v", err)
	}
	for _, ex := range examples {
		f, err := spec.Load(ex)
		if err != nil {
			t.Fatal(err)
		}
		name := filepath.Base(ex)

		rulesOut, err := marshal(rules.Compile(f), "examples/"+name)
		if err != nil {
			t.Fatal(err)
		}
		check(t, filepath.Join("testdata", f.Service+".rules.golden"), rulesOut)

		dash, err := dashboard.Render(f)
		if err != nil {
			t.Fatal(err)
		}
		check(t, filepath.Join("testdata", f.Service+".dashboard.golden"), append(dash, '\n'))
	}
}

func check(t *testing.T, path string, got []byte) {
	t.Helper()
	if *update {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run go test ./cmd/slogen -update)", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s is out of date; run go test ./cmd/slogen -update and review the diff", path)
	}
}
