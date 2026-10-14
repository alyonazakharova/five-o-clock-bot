.PHONY: build linux test

build:
	go build -o five-o-clock-bot .

# Static binary for a typical Linux server; use GOARCH=arm64 for ARM boxes.
linux:
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/five-o-clock-bot .

test:
	go test ./...
