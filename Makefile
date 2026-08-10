.PHONY: fast check verify

fast:
	go test ./internal/agentextension ./internal/catalog ./internal/gorelease ./internal/distributionrelease ./internal/cli

check:
	go test ./...

verify:
	go run ./internal/qualitygate
