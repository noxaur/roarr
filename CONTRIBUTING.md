# Contributing to roarr

Thank you for your help. This page tells you how to set up, check and submit a change. It also tells how to run the lab, measure search speed and ship an image.

## Set up

- Install Go 1.25 or later, Node 22 or later and Docker.
- Clone the repository and run `make build`. This installs the UI packages, builds the UI and writes the binary to `bin/roarr`.

## Build and run

- `make build` builds the UI into `internal/webui/dist` and embeds it in `bin/roarr`. The Go build needs that folder, so build the UI before you run `go build` by hand.
- `bin/roarr` starts the server on port 3847. It reads the same environment variables as the Docker image. See the configuration table in `README.md`.
- `cd web && ROARR_API=http://localhost:3847 npm run dev` gives hot reload against a running roarr.
- `cd web && node mock/server.mjs` serves a fake API without a roarr or an upstream. It has a small library, a release list that exercises the language and subtitle columns, and a queue with stuck items.
- `make image` builds the Docker image. Add `--build-arg FFMPEG=false` to `docker build` to leave out ffmpeg.

## The lab

`make lab` starts a real Sonarr, Radarr, Prowlarr and qBittorrent in Docker, with seven fake Torznab indexers. No real tracker is contacted. `make lab-down` stops it. `lab/README.md` has the URLs, the credentials and the behaviour of each fake indexer.

## Check your change

- Run `make test`. It runs `go vet`, the Go tests, the TypeScript check and Vitest. The `image` GitHub Actions workflow runs it on each pull request and on each push to `main` or a version tag.
- For a change that can affect search speed, run `make lab`, then `make bench`, and compare the result with `main`. [docs/experiments.md](docs/experiments.md) tells what each column measures.
- For a change to watch-first downloads, run `make lab`, then `make swarm`. `lab/README.md` tells what it needs.

## Where to read first

- `AGENTS.md` has the architecture in one paragraph and the project rules.
- The header comment of `web/src/styles.css` tells how the UI must look: the colours, the state hues and the spacing. Read it before you change UI.
- `docs/` holds the architecture of each area and the reasons and constraints behind it. For example, [native-import.md](docs/native-import.md) tells how roarr downloads and imports without Sonarr and Radarr.

## Submit a change

- Write commit messages in English: `type(scope): description`. The scope is optional.
- Write the pull request title like a commit message: `type(scope): description`. Example: `feat(transcode): pause AV1 encoding and limit it to a weekly schedule`.
- Write the pull request body in this order: the problem in 1 to 3 sentences, a `## Changes` heading with an unordered list, and a `## Verification` heading that says how you verified the change.
- For a UI change, attach a before image and an after image. For motion, attach a video. Upload them to the pull request. Do not commit them.
- Verify a bug before you fix it. Fix only bugs that you verified.

## Releasing

The `image` workflow builds the Docker image for `linux/amd64` and `linux/arm64` and pushes it to `ghcr.io/noxaur/roarr`.

- Each push to `main` publishes `latest` and a short-SHA tag.
- A tag such as `v1.2.3` publishes `1.2.3` and `1.2`. The tag name is also the version that the binary reports.

```sh
git tag v1.2.3
git push origin v1.2.3
```
