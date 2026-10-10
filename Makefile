VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

.PHONY: build web test lab lab-down bench swarm image

## build: web UI + single binary into ./bin/roarr
build: web
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=$(VERSION)" -o bin/roarr ./cmd/roarr

web:
	cd web && npm ci --no-audit --no-fund && npm run build
	touch internal/webui/dist/.keep

test:
	go vet ./... && go test ./...
	cd web && npx tsc -b --noEmit && npx vitest run

image:
	docker build --build-arg VERSION=$(VERSION) -t roarr:$(VERSION) .

## lab: real Sonarr/Radarr/Prowlarr/qBittorrent + fake indexers (see lab/README.md)
lab:
	sh lab/scripts/seed-config.sh
	docker compose -f lab/docker-compose.yml up -d --build
	python3 lab/scripts/bootstrap.py

lab-down:
	docker compose -f lab/docker-compose.yml --profile swarm --profile full down

bench:
	go run ./lab/bench -n 5

swarm:
	docker compose -f lab/docker-compose.yml --profile swarm up -d
	python3 lab/swarm/experiment.py 2
