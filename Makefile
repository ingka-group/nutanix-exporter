GO_VERSION := $(shell cat .go-version | tr -d '[:space:]')

.PHONY: bump-go check-go build test lint tidy update

bump-go:
	@if [ -z "$(VERSION)" ]; then echo "Usage: make bump-go VERSION=x.y.z"; exit 1; fi
	@echo "Bumping Go version to $(VERSION)"
	sed -i '' 's/^go .*/go $(VERSION)/' go.mod
	sed -i '' 's|FROM golang:[^ ]*|FROM golang:$(VERSION)|' Dockerfile
	echo "$(VERSION)" > .go-version
	@echo "Done. Run 'go mod tidy' and commit the changes."

check-go:
	@EXPECTED="$(GO_VERSION)"; \
	FAILED=0; \
	ACTUAL=$$(grep '^go ' go.mod | awk '{print $$2}'); \
	if [ "$$ACTUAL" != "$$EXPECTED" ]; then \
		echo "MISMATCH: go.mod has go $$ACTUAL, expected $$EXPECTED"; \
		FAILED=1; \
	fi; \
	ACTUAL=$$(grep '^FROM golang:' Dockerfile | head -1 | sed 's/FROM golang:\([^ ]*\).*/\1/'); \
	if [ "$$ACTUAL" != "$$EXPECTED" ]; then \
		echo "MISMATCH: Dockerfile has go $$ACTUAL, expected $$EXPECTED"; \
		FAILED=1; \
	fi; \
	if [ $$FAILED -eq 1 ]; then exit 1; fi; \
	echo "go.mod and Dockerfile match .go-version ($(GO_VERSION))"

build:
	go build ./...

test:
	go test -race -cover ./...

lint:
	golangci-lint run ./...

tidy:
	go mod tidy

update:
	go get -u ./...
	go mod tidy
