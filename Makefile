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

PACK := Severance.S02.1080p.WEB-DL.DDP5.1.H.264-LABPACK
PAYLOAD := lab/state/swarm/payload/$(PACK)

## swarm: watch-first download-order experiment on the running lab (see lab/README.md)
swarm: lab/state/torrents/$(PACK).torrent
	docker compose -f lab/docker-compose.yml --profile swarm up -d
	python3 lab/swarm/experiment.py 2

lab/state/torrents/$(PACK).torrent: | $(PAYLOAD)
	uv run --with torf python lab/swarm/maketorrent.py

# Ten 11-minute episodes, long enough that Sonarr does not treat them as samples.
$(PAYLOAD):
	mkdir -p $@.tmp
	for i in 01 02 03 04 05 06 07 08 09 10; do \
	  docker run --rm -v "$(CURDIR)/$@.tmp":/out lscr.io/linuxserver/ffmpeg -loglevel error \
	    -f lavfi -i testsrc=size=320x180:rate=25 -f lavfi -i sine=frequency=440:sample_rate=22050 \
	    -t 660 -c:v libx264 -preset ultrafast -b:v 330k -c:a aac -b:a 32k \
	    /out/Severance.S02E$$i.1080p.WEB-DL.DDP5.1.H.264-LABPACK.mkv || exit 1; \
	done
	mv $@.tmp $@
