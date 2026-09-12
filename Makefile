SHELL := /usr/bin/env bash

GO ?= go
NPM ?= npm
VERSION ?= dev
COMMIT ?= unknown
BUILD_DATE ?= 1970-01-01T00:00:00Z
LDFLAGS := -s -w -buildid= -X forms/internal/version.Version=$(VERSION) -X forms/internal/version.Commit=$(COMMIT) -X forms/internal/version.BuildDate=$(BUILD_DATE)

.PHONY: deps frontend-check frontend-test frontend-build bundle-check public-budget-check go-check systemd-check systemd-runtime-check cross-browser-check test integration build release

deps:
	cd web/admin && $(NPM) ci

frontend-check:
	cd web/admin && $(NPM) run check

frontend-test:
	cd web/admin && $(NPM) run test

frontend-build:
	cd web/admin && $(NPM) run build

bundle-check:
	./scripts/check-bundle.sh

public-budget-check:
	./scripts/check-public-budget.sh

go-check:
	@test -z "$$(gofmt -l $$(find cmd internal migrations -name '*.go' -type f))" || { echo "فایل Go فرمت‌نشده وجود دارد"; exit 1; }
	$(GO) vet ./...

systemd-check:
	./scripts/verify-systemd-units.sh

systemd-runtime-check:
	./scripts/verify-systemd-runtime.sh

cross-browser-check:
	./scripts/verify-cross-browser.sh

test: frontend-check frontend-test public-budget-check go-check
	$(GO) test ./...

integration:
	@test -n "$(FORMS_TEST_DATABASE_URL)" || { echo "FORMS_TEST_DATABASE_URL الزامی است"; exit 1; }
	$(GO) test -count=1 -run 'TestStage(One|Two|Three|Four)' -v ./internal/web

build: frontend-build bundle-check public-budget-check
	mkdir -p bin
	CGO_ENABLED=0 $(GO) build -mod=readonly -trimpath -buildvcs=false -ldflags "$(LDFLAGS)" -o bin/forms ./cmd/forms

release:
	@test "$(VERSION)" != "dev" || { echo "VERSION برای release الزامی است"; exit 1; }
	./scripts/build-release.sh "$(VERSION)" "$(COMMIT)"
