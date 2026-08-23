# ─── Omni-BugTracker ───────────────────────────────────────────────
# Single Go module builds two entrypoints (server + worker) from one binary.

.DEFAULT_GOAL := help
BIN           := ./bin
PKG           := github.com/omni/bugtracker
DATABASE_URL  ?= postgres://omni:omni@localhost:5432/omni_bugtracker?sslmode=disable

## help: list targets
help:
	@grep -E '^##' $(MAKEFILE_LIST) | sed 's/## //'

## tools: install codegen tooling (sqlc, oapi-codegen, goose, river CLI)
tools:
	go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
	go install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@latest
	go install github.com/pressly/goose/v3/cmd/goose@latest
	go install github.com/riverqueue/river/cmd/river@latest
	cd web && npm install

## generate: run the codegen pipelines. NOTE: nothing in the build imports their
## output today — see "A note on code generation" in docs/ARCHITECTURE.md.
generate:
	sqlc generate
	oapi-codegen -config api/oapi-codegen.yaml api/openapi.yaml
	cd web && npm run gen:api

## migrate: apply goose schema migrations + River queue tables (single binary)
migrate:
	OMNI_BT_DATABASE__DSN="$(DATABASE_URL)" go run ./cmd/migrate up

## build: compile server + worker + migrate + obt binaries
build:
	go build -o $(BIN)/server ./cmd/server
	go build -o $(BIN)/worker ./cmd/worker
	go build -o $(BIN)/migrate ./cmd/migrate
	go build -o $(BIN)/obt ./cmd/obt

## obt: build just the CLI
obt:
	go build -o $(BIN)/obt ./cmd/obt

## dev: run API with live config (requires postgres + redis up)
dev:
	go run ./cmd/server

## worker: run background workers
worker:
	go run ./cmd/worker

## test: run unit tests
test:
	go test ./... -race -count=1

## lint: vet + staticcheck
lint:
	go vet ./...

## web: run the frontend dev server
web:
	cd web && npm run dev

## up: full self-hosted stack via docker compose
up:
	docker compose -f deploy/docker-compose.yml up --build

.PHONY: help tools generate migrate build obt dev worker test lint web up
