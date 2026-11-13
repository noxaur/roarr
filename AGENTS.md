# roarr

roarr is one fast, quiet UI on top of Sonarr, Radarr, Prowlarr, Seerr and Jellyfin.

## What makes roarr special

We can't compromise on these. They make us what we are today and tomorrow.

- Fast: results show within half a second, and a slow or dead indexer never blocks the others. Speed is the reason roarr exists, so a slower search is a bug. Measure it in the lab, and do not trade it for convenience.
- One question, one button: the user says what they want, and roarr gets exactly that. The usual case is a single press of **Get**, and every choice beyond it stays out of the way until the user asks for it.
- Small and simple to run: one Docker image, one port, one volume. Each upstream is optional except Prowlarr, and roarr works out what it can from the ones it has.

## General principles

- Channel your inner DRY, YAGNI, KISS and SOLID.
- Build on the shared layer each area already has, such as the theme tokens in `web/src/styles.css`, the styled ui-sdk parts in `web/src/components/ui`, or the clients in `internal/{arr,seerr,jellyfin,qbit}`. If it can't do what you need, extend it there.
- Write real fixes, not bypasses.
- Keep code in one function unless a part is reused or composable.
- Before adding complexity for a speculative or vanishingly unlikely race or security edge case, explain the concrete failure mode, likelihood and complexity cost to the user and get their buy-in.
- If a short request has two possible meanings, ask one question before you start.
- Call a bug fixed only when a test, log or recording shows it.

## How it works

roarr is one Go binary that embeds the React UI. Searching fans out to Prowlarr one indexer at a time, in parallel, through a priority queue per indexer, so the query for what the user clicked jumps ahead of background prefetch. Each answer streams to the browser over SSE as it arrives and is parsed, scored and mapped to the episodes it covers. The planner picks the cheapest set of releases that covers the selection, and that plan is what **Get** grabs. `app.Backend` decides where a grab goes. The default `arrBackend` pushes releases to Sonarr/Radarr so their clients, history, blocklist and import apply. `IMPORT_MODE=native` makes roarr the download client and importer itself, with `/config/library.json` as its library. Packs sent to qBittorrent download watch-first, in episode order, and each finished episode is imported right away. After a download, `verify` checks the files for fakes and `transcode` can convert them to AV1. Settings, connections included, are changed under **Admin** and saved in `/config`; one set by an environment variable is locked there.

## Docs

- Before you change an area, read its doc in `docs/`. Read `lab/README.md` before you change the lab or a benchmark.
- The theme tokens and the rules for state colours are in the header comment of `web/src/styles.css`. Read it before you change UI.
- Every interactive primitive in the UI is ui-sdk. `web/src/components/ui` only puts roarr's classes and defaults on ui-sdk parts, a part that needs neither is imported from `@ui-sdk/react/<name>` directly, and app-level composition lives in `web/src/components`. Do not add another primitive library such as Radix or shadcn.
- Before you use ui-sdk, follow the `ui-sdk` skill (`.agents/skills/ui-sdk/SKILL.md`) and read `web/node_modules/@ui-sdk/react/docs/llms.txt`. To build a new component, also search `web/node_modules/@ui-sdk/react/docs/core.md` by heading, and read `rendering.md` next to it.
- When a large feature adds or reshapes an area, write or update its doc in `docs/` in the same change. The code shows what it does, so the doc records why: each architectural decision, the alternative you rejected and the constraint that decided it. Leave out how-to steps, code tours, test results and dated verification runs. User-facing setup belongs in `README.md`. Recipes belong in a skill.
- Keep docs tight. State each fact once, in the doc that owns it, and delete what has gone stale in the same change that made it stale.

## Checks

- Build: run `make build`. It builds the UI into `internal/webui/dist` and the binary into `bin/roarr`.
- Test: run `make test`. It runs `go vet`, the Go tests, the TypeScript check and Vitest. Test the real implementation instead of copying its logic into the test.
- UI: `cd web && node mock/server.mjs` serves a fake API. When you change an API the UI uses, update the mock in the same change.
- Search speed: run `make lab`, then `make bench`, for a change that can affect how fast results arrive.

## Branches and pull requests

- Use a short branch name of at most three hyphenated words, such as `native-delete` or `fix-watch-packs`.
- Make a PR only when asked. Before you create one, load the `pr` skill (`.agents/skills/pr/SKILL.md`) and follow it. It is mandatory.

## Code and residue

- Agents read code well, so add comments only where the code can't speak for itself.
- Keep plans, research notes and scratch files outside the worktree.
