.PHONY: dev test

dev:
	go run github.com/air-verse/air@latest -c .air.toml

test:
	go test ./...
