SPECS := $(wildcard examples/*.yaml)

.PHONY: build test unit rules dashboards check promtool fmt clean

build:
	go build -o slogen ./cmd/slogen

test: unit check promtool ## everything CI runs

unit:
	go vet ./...
	go test ./...

rules:
	go run ./cmd/slogen generate -o out/rules $(SPECS)
	go run ./cmd/slogen generate -o out/prometheusrule -format prometheusrule $(SPECS)

dashboards:
	go run ./cmd/slogen dashboard -o out/dashboards $(SPECS)

check: rules ## promtool syntax and lint checks on generated rules
	promtool check rules out/rules/*.yaml

promtool: rules ## behavioural tests: alerts fire and resolve when they should
	promtool test rules test/promtool/*.yaml

fmt:
	gofmt -w .

clean:
	rm -rf out slogen
