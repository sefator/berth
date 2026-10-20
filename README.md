<p align="center"><img src="assets/banner.svg" alt="berth: a llama harbor master on a night pier moors a padlock lantern to a dock board of GPUs. On gpu0 llama-swap hands the card to strata when its reply ends, ComfyUI waits for its job to finish on gpu1, and a game holds gpu2. Zero collisions." width="100%"></p>

<p align="center">
  <a href="https://github.com/T-Crypt/berth/releases"><img src="https://img.shields.io/github/v/release/T-Crypt/berth?color=7ad4f2&amp;labelColor=0a1017" alt="latest release"></a>
  <a href="https://github.com/T-Crypt/berth/actions/workflows/ci.yml"><img src="https://img.shields.io/github/actions/workflow/status/T-Crypt/berth/ci.yml?branch=main&amp;label=ci&amp;labelColor=0a1017" alt="CI status"></a>
  <img src="https://img.shields.io/badge/go-1.27-7ad4f2?labelColor=0a1017" alt="Go 1.27">
  <img src="https://img.shields.io/badge/runs%20on-linux%20%7C%20windows%20(experimental)-e8b56f?labelColor=0a1017" alt="Linux, Windows experimental">
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-Apache--2.0-7ee8ac?labelColor=0a1017" alt="Apache-2.0"></a>
</p>

<p align="center">
  <a href="#quick-start">Quick start</a> &nbsp;·&nbsp; <a href="#how-it-works">How it works</a> &nbsp;·&nbsp; <a href="#install">Install</a> &nbsp;·&nbsp; <a href="#gateway-preview">Gateway</a> &nbsp;·&nbsp; <a href="#visibility">Harbor</a> &nbsp;·&nbsp; <a href="#roadmap">Roadmap</a>
</p>

# Berth

Berth decides who gets your GPU. Chat models, image pipelines, voice engines, games and GPU apps
lease the card instead of grabbing it, so two of them never load at once and crash each other
with an out-of-memory error. One Go binary, no cloud account, nothing else to install.

```sh
berth daemon &                                        # one broker per card
berth run --as llama -- llama-server -m model.gguf    # this engine now takes turns
berth status                                          # who holds the card, who waits, and why
```

## How it works

<p align="center"><img src="assets/how-it-works.svg" alt="How berth works. Four clients ask for the card: a chat agent through the gateway, a ComfyUI workflow, an engine under berth run, and a game. berthd queues them by priority, drains the holder when its reply or job ends, waits for the driver to show the memory free, then grants the lease. Below, a timeline of one 24 GiB card: llama-swap, strata, a game and ComfyUI take turns and never overlap; markers 1 to 4 show one handoff." width="100%"></p>

Every engine asks berthd for the card: through `berth run`, the gateway, ComfyUI's API or a game
claim. When another engine holds it, Berth:

1. queues the request by priority: `hold`, `foreground`, `interactive`, `batch`;
2. lets the holder finish its reply or image job, then unloads it;
3. waits until the driver shows the memory free;
4. grants the lease and starts the next engine, and the request that waited streams.

The numbered markers on the timeline are those four steps. A process that holds VRAM without a lease
shows up as a stowaway: Berth reports its PID and memory and leaves it running.

> **Status: alpha (v0.2).** Runs daily on an RTX 4090 workstation beside llama-swap, NInfer,
> Strata, ComfyUI and games. [Capabilities](#capabilities) lists what works and has tests; the
> [Roadmap](#roadmap) lists the phases and what is still missing.

## What Berth does

**Shares one card between everything**
- **Engines take turns.** You start each one through `berth run`. When another needs the
  card, Berth waits for the holder's current reply to end, unloads it (its own unload, then
  SIGTERM, then SIGKILL) and lets the next one in only after the driver shows the memory free.
- **Priorities.** `foreground`, `interactive`, `batch`, and `hold` above them all. A lower
  request waits and gets a reason naming the holder, never an OOM crash.
- **No ping-pong.** When two clients ask for different engines at once, a ranking picks one
  and the other waits, so the card stays put.
- **ComfyUI at a job boundary.** Berth waits for the running image job to finish before the handoff.
- **Survives restarts.** Upgrade or restart the daemon and your running engines stay up and
  re-attach.

**Games and other apps**
- **Games take the card while you play.** Launch a Steam, Lutris or gamescope game on Linux,
  or a Steam, Epic, GOG, EA, Ubisoft, Xbox or Battle.net game on Windows, and Berth drains your
  AI engines until you quit.
- **Creative apps keep their memory.** Berth names what Blender, DaVinci Resolve, OBS,
  Kdenlive, Premiere and After Effects hold and counts it against the card, without a
  stowaway report.
- **Your own rules.** `[[claim]]` in `berth.toml` covers anything else: a training run, a
  render farm job, a cracking session, by name, command line or launcher.
- **`berth hold`** frees the card for a call or a game you start yourself. Ctrl-C gives it back.
- **`berth run --pinned`** for an app that must never be stopped mid-work.

**One API for your engines** *(preview)*
- **`berth gateway`**: one OpenAI-compatible port. Name a model and Berth starts its engine,
  unloads whatever holds the card, waits for the load and streams the answer.
- It proxies ComfyUI's workflow API too, so an image request queues behind the same lease.
- **Wake pages** (`--gui-listen`): a bookmark to a stopped engine's web UI shows who holds the
  card and a Start button, says what the start waits for, and opens the UI once it answers.
- Bearer key on every route, loopback only, browser origins refused.

**Models**
- **`berth pull hf:org/repo:Q4_K_M`** downloads from Hugging Face, resumes where it stopped,
  and checks every file against the size and sha256 the Hub publishes before using it.
- **`berth pull civitai:VERSION_ID`** does the same from CivitAI, with the hash the site
  publishes, and **`berth models`** shelves what it finds as chat, image or audio so a
  machine reading a shared root knows what a folder is.
- **`berth storage`** shows each model root's filesystem, free space against its floor, and
  the other operating systems it is shared with.
- **`berth verify`** re-checks pulled models offline and names any file that changed.
- **`berth models`** lists every model on the machine (GGUF, safetensors, the Hugging Face
  cache, LM Studio and Ollama's stores) with quant, size, parameters, context and when each
  was last used, so you can see what earns its disk space.
- **`berth models export ollama`** turns a GGUF you already have into an Ollama model.

**Sees everything on the card**
- **Finds your engines.** Berth reads llama.cpp, llama-swap, Ollama, vLLM, SGLang, ComfyUI,
  Kokoro, LM Studio, KoboldCpp, TabbyAPI, LocalAI and more on their default ports, plus any
  OpenAI-compatible or custom server you name.
- **Stowaways.** Berth reports any process holding VRAM without a lease, with its PID, name
  and memory, and leaves it running.
- **Built-in telemetry.** Per-card memory, load, temperature, power and clocks; tokens per
  second, queue, latency percentiles, KV cache use, a token ledger and a per-model
  leaderboard; 30 days of history on disk.
- **The harbor web UI** (`berth harbor`): a departures board of leases, a collisions-avoided
  counter, charts and the event log. Read-only, loopback by default.
- **Alerts** to the desktop or a webhook, `/metrics` for Prometheus, and `berth export` for
  CSV or JSON.

**Built for agents and scripts**
- **`berth skill install`** teaches Claude Code, Codex, OpenCode and other agents to ask Berth
  for the card instead of killing GPU processes.
- Every command takes `--format json|yaml`, keeps results on stdout and diagnostics on stderr,
  and exits 0, 1 or 2 by one rule.

**Runs on** Linux with an NVIDIA GPU (supported), Linux with an AMD GPU through amdgpu
(sysfs + fdinfo) and Windows 10/11 with an NVIDIA GPU
(experimental). Apache-2.0.

## Quick start

```sh
go install github.com/T-Crypt/berth/cmd/berth@latest      # or a release binary, see Install
berth daemon &                                            # the broker for this card
berth status                                              # the card, leases, engines found
berth run --as llama --priority foreground -- llama-server -m model.gguf --port 8080
berth harbor                                              # the web UI on http://127.0.0.1:9798/
```

## Preview

The harbor UI (basic mode, served from the built-in demo fixture - every number on the screen
comes from `demo.json`). The tide shows the card's memory by holder over six hours; the
hatched stretch is a stowaway the fixture replays: a model that ran without a lease after a
daemon restart, reported and left alone.

<p align="center"><img src="assets/preview-harbor.png" alt="Berth harbor: the CARD tab with ticket counter, request KPIs, the tide of card memory by holder, and the model timeline" width="900"></p>

The Engines tab (advanced mode): decode throughput, the token ledger, the per-model
leaderboard, storage and residency.

<p align="center"><img src="assets/preview-requests.png" alt="Berth harbor Engines tab, advanced mode: decode throughput per engine, the token ledger, the per-model leaderboard, storage and residency" width="900"></p>

## Install

Two ways: hand it to your coding agent, or do it yourself. Both end in the same place: the
daemon running as a user service, your engines wrapped in `berth run`, and the harbor UI on
<http://127.0.0.1:9798/>.

### Option 1: have your agent install it

Paste this into Claude Code, Codex, Cursor, OpenCode or any coding agent that can run shell
commands on the machine with the GPU:

```text
Install Berth (https://github.com/T-Crypt/berth), a VRAM lease broker for NVIDIA GPUs, on this
Linux machine, following its README and contrib/INTEGRATION.md. Steps:

1. Check prerequisites: Linux, an NVIDIA driver with `nvidia-smi` working, systemd user
   services. Stop and tell me if any is missing.
2. Download the latest release binary for this CPU (berth-linux-amd64 or berth-linux-arm64)
   and its SHA256SUMS from https://github.com/T-Crypt/berth/releases/latest, verify the
   checksum, and install it as ~/.local/bin/berth. (If Go 1.27+ is installed you may instead
   run: go install github.com/T-Crypt/berth/cmd/berth@latest.) Run `berth version`.
3. Install contrib/berthd.service and contrib/berth-harbor.service as systemd user units and
   start them. Confirm with `berth status`.
4. Find the inference engines on this machine (llama.cpp, llama-swap, Ollama, vLLM, SGLang,
   ComfyUI, LM Studio, KoboldCpp, TabbyAPI, LocalAI and so on): what runs them (systemd unit,
   llama-swap config, script) and on which port. Show me the list before changing anything.
5. For each engine I approve, wrap its start command in `berth run` as contrib/INTEGRATION.md
   shows (ComfyUI: contrib/comfyui.service with --drain-comfyui; Ollama: a systemd override of
   `ollama serve`). For llama-swap, wire the whole roster in one pass instead of one model at a
   time: `berth wire llama-swap --config <llama-swap config> --write` keeps a backup beside the
   file, keeps each model's original command, and `--check` exits 1 when a model is unwired.
   Ask me which engine wins the card (--priority foreground) and which waits (interactive or
   batch). Back up every file before you edit it and tell me how to undo each change.
6. Engines on non-default ports or other machines: add [[engine]] entries or
   [engines] discover_ports / discover_hosts to ~/.config/berth/berth.toml, then run
   `berth config check` and `berth engine check`.
7. Restart what you changed, then show me `berth status` with each engine leased and read,
   and the harbor at http://127.0.0.1:9798/. Do not expose the harbor beyond loopback unless
   I ask; it has no authentication.
```

The agent asks before it wraps anything, and every change it makes has an undo.

### Option 2: manual

**1. The binary.** Download `berth-linux-amd64` (or `-arm64`) and `SHA256SUMS` from the
[latest release](https://github.com/T-Crypt/berth/releases/latest):

```sh
cd /tmp
curl -fLO https://github.com/T-Crypt/berth/releases/latest/download/berth-linux-amd64
curl -fLO https://github.com/T-Crypt/berth/releases/latest/download/SHA256SUMS
sha256sum --check --ignore-missing SHA256SUMS
install -Dm755 berth-linux-amd64 ~/.local/bin/berth
berth version
```

Or build it: `go install github.com/T-Crypt/berth/cmd/berth@latest` (Go 1.27+, no C toolchain).

**2. The daemon and the UI** (one per machine, systemd user units from `contrib/`):

```sh
mkdir -p ~/.config/systemd/user
cp contrib/berthd.service contrib/berth-harbor.service ~/.config/systemd/user/
systemctl --user daemon-reload
systemctl --user enable --now berthd berth-harbor
berth status            # the card, leases, stowaways, engines found
```

Open <http://127.0.0.1:9798/>. Engines on their default ports appear on their own.

**3. Wrap your engines** so they lease the card instead of grabbing it. The pattern:

```sh
berth run --as <name> --priority <batch|interactive|foreground> -- <the engine's command>
```

Per-engine recipes (llama-swap, llama.cpp, Ollama, ComfyUI, vLLM, SGLang, custom) are in
[contrib/INTEGRATION.md](contrib/INTEGRATION.md).

**4. Optional configuration** in `~/.config/berth/berth.toml`; validate with
`berth config check`:

```toml
[metrics]
listen = "127.0.0.1:9797"      # Prometheus text, loopback only

[engines]
# discover = true              # default: probe the well-known engine ports every minute
# discover_ports = [9123]      # extra ports to probe
# discover_hosts = ["192.168.1.40"]

[[engine]]                     # anything discovery does not find, on any port
name = "llama-swap"
type = "llama-swap"
url  = "http://127.0.0.1:9090"

[alerts]
[alerts.queue]
warn = 4
crit = 10

[models]
# dirs = ["/mnt/d/models"]   # the roots berth models scans and pull writes into

[library]
# free_floor = 0.10          # keep this fraction of every root free; 0 turns it off

[[library.root]]             # what a root holds and shares
path   = "/mnt/d/models"
shelf  = "image"             # chat | image | audio; beats what the files say
shared = "windows"           # the other side of a shared root, shown by berth storage
```

**Uninstall:** `systemctl --user disable --now berthd berth-harbor`, remove the two unit
files and `~/.local/bin/berth`, and take `berth run ... --` off any engine command you
wrapped. History lives in `~/.local/state/berth/`.

---

## Engines

| Engine | Reader (`type`) | Found on its own | What Berth reads |
|---|---|---|---|
| llama.cpp `llama-server` | `llamacpp` | :8080 | tokens, rates, queue, context window, KV slots |
| llama-swap | `llama-swap` | :8080 | every running model's upstream, request log, per-model window |
| Ollama | `ollama` | :11434 | loaded models and their VRAM |
| vLLM | `vllm` | :8000 | models, window; tokens and queue from `/metrics` |
| SGLang | `sglang` | :30000 | models, window; tokens and queue with `--enable-metrics` |
| ComfyUI | `comfyui` | :8188 | jobs running and queued, version; job-boundary handoff |
| Kokoro-FastAPI (text to speech) | `kokoro` | :8880 | health, models, voice count, default voice |
| LM Studio | `lmstudio` | :1234 | loaded models, context length |
| KoboldCpp | `koboldcpp` | :5001 | model, context length |
| TabbyAPI (ExLlamaV3) | `tabbyapi` | :5000 | models, context length |
| LocalAI | `localai` | :8080 | models, tokens |
| Unsloth Studio | `unsloth` | :8888 | health; models with an API key |
| FreeToken | `freetoken` | :1919 | tokens, queue |
| [NInfer](https://github.com/T-Crypt/ninfer-4090) | `ninfer` | as llama.cpp | as llama.cpp, plus draft acceptance |
| [Strata](https://github.com/T-Crypt/strata) | `strata` | :8080 | model, window, tokens, queue |
| Any OpenAI-compatible server | `openai` | :8080, :8000 | models |
| Anything else | `custom` | never | your health, models, metrics and slots paths; `kind = "tts"`, `"stt"`, `"image"` or `"other"` tracks a voice or image server as one |

Every reader takes any URL, so any port and any host works. Engines that need a key name the
environment variable that holds it (`api_key_env`); the key never goes in the file. An
engine's figures are its own claims and are shown apart from what the driver measures.

### ComfyUI as a suite

`berth engine install|show|update|rollback|remove` manages ComfyUI as a suite: the core,
ComfyUI-Manager and every git-based node pack are separate components, each pinned to a git
commit with one previous commit kept as the rollback snapshot. Several installs run side by
side on separate ports; each install's derived engine appears in `berth engine check`,
`berth status` and the gateway's wake page, and its start line is the exact `berth run` shape
an operator's unit already uses (the only alignment needed is `--as <install-name>`).

- `install NAME --adopt PATH` records an existing checkout in place; `--path` clones a
  fresh one (PyTorch is your step: the install is registered either way, and ComfyUI's own
  start error names the missing piece).
- `update NAME` pins the core and each pack to a newer commit; `rollback NAME --component C`
  swaps back. A dirty component, or an install with jobs running, is refused: the update
  happens at the job boundary.
- `remove NAME` drops the state entry; `--delete --yes` also removes the directory.

The state lives in `engines.toml` beside `berth.toml`. Berth writes each install an
`extra_model_paths.yaml` naming the node's model roots from `berth.toml`, so an install's
models come from the same library the rest of the node uses; the daemon rewrites the file at
startup, so a change to the roots applies on the next restart.

---

## Capabilities

### Leasing (the core)
- **Explicit leases.** Engines start through `berth run`, which waits for a berth, starts
  the engine in its own process group, and gives the berth back when the engine exits.
- **Priorities.** `foreground` > `interactive` > `batch`. An equal or higher request
  asks the holder to drain; a lower one waits and is refused with a reason naming the
  holder (exit 75), never an out-of-memory crash.
- **Handoff in steps.** A drain tries the engine's own unload first (for llama-swap, its
  unload URL), then SIGTERM to the process group, then SIGKILL, each after a grace period.
- **Measured release.** Berth grants the next engine only after the driver shows the old
  holder's processes gone, since an exited process can still hold memory.
- **Stuck releases are caught.** A released lease whose memory never frees turns
  *aground* and keeps blocking, with a reason naming the process and how much it holds.
- **Process identity is PID plus start time**, so a reused PID never inherits a lease.
- **Whole-card or sized leases.** `--needs-mib N` lets engines that fit share a card.
- **Fail-open, never silent.** If the daemon is down, `berth run` still starts the engine
  but records it as an *unprotected run* that status shows (`--fail-closed` refuses).
- **Survives a daemon restart.** A running engine stays up when berthd restarts or is
  upgraded; `berth run` dials the new daemon and re-attaches by PID plus start time. For
  15 s after a start the daemon grants nothing new while such engines are still due back,
  so nothing is loaded onto a card an engine already holds.
- **ComfyUI at a job boundary.** `--drain-comfyui` waits until ComfyUI's queue is empty
  before stopping it; a job in progress is never cut off.
- **No juggling.** Two engines at the same priority drain each other: the last one asked for
  wins, which is what you want when you switch models. When two clients want different
  engines at once, that flips the card back and forth and every request fails. Berth calls it
  a juggle when an engine is drained within `juggle_window` (15 s) of getting the card while it
  is still busy (a request in flight, a ComfyUI job queued, a model loading). The engine ranking
  decides a juggle: the winner keeps or takes the card, and the loser waits for it instead of
  draining it back. An idle holder hands over at once, so chat, then an image, then chat again
  is three clean handoffs.

  ```toml
  [priority]
  order = ["llama-swap", "strata", "comfyui"]  # first is best; omit to rank by use
  juggle_window = "15s"                        # a negative value turns it off
  ```

  Without an `order`, the engine with the most hours on the card through Berth over the last
  7 days ranks first (kept in `~/.local/state/berth/usage.json`); a tie leaves the holder in
  place. `berth run --group NAME` files a lease under an engine family: llama-swap runs each
  model under its own name, so give each `--group llama-swap`. `berth status` shows the ranking
  and a count of juggles stopped; the harbor log shows each one with the reason.

### Games, apps and holds
- **Claims cover work Berth does not start.** berthd scans the process table every 2 s.
  A *hold* claim keeps the card while its process runs: engines are drained and wait, with a
  15 s linger so a launcher restarting the game does not bounce the card back. A *track*
  claim names an app's card memory so it counts against the card without being reported as a
  stowaway; nothing waits on it.
- **Built-in pack, on by default.** Holds: Steam games (anything under Steam's `reaper`),
  Lutris and gamescope on Linux; Steam libraries (`steamapps\common`), Epic, GOG, EA,
  Ubisoft, Xbox and Battle.net games on Windows, with launchers, Wallpaper Engine and
  Lossless Scaling left out. Tracks: Blender, DaVinci Resolve, OBS, Kdenlive, Premiere,
  After Effects. `berth claims` shows every rule and what it matches now.
- **Your own rules:**

  ```toml
  [[claim]]
  name = "training"
  cmdline = 'train\.py'      # regex; on Windows it sees the exe's full path
  action = "hold"            # or "track"
  linger = "30s"

  [claims]
  builtin = false            # turn the built-in pack off
  ```

  A rule can also match `exe` names (globs, `.exe` optional), a launcher `parent`, and
  exclude with `not_cmdline`.
- **`berth hold [--for 2h] [--reason game]`** takes the card for you. It is a pinned lease at
  the top priority, so nothing drains it, and it ends on Ctrl-C, after `--for`, or when the
  process dies.
- **`berth run --pinned`** gives an app a lease that is never drained (it keeps the card until
  it exits).
- A hold cannot free memory held by something outside Berth: that memory is still reported,
  as contested.

### Models
- **`berth pull hf:ORG/REPO[:QUANT|FILE|GLOB][@REV]`** resolves the revision to one commit,
  picks the GGUF for the quant (every part of a split model, and the vision projector when
  there is one), and downloads to `~/models/<org>/<repo>/`.
- **`berth pull civitai:VERSION_ID`** resolves a model version (or
  `civitai:model:MODEL_ID` for its latest, or the site's own URLs), downloads the files it
  lists to `~/models/civitai/<model>/`, and writes the same manifest. Public models need
  no key; gated files read `$CIVITAI_API_KEY`.
- **`berth civitai search QUERY`** and **`berth civitai versions MODEL_ID`** find the ids
  a pull takes: models by name and asset kind, then a model's versions newest first.
- **Integrity first.** Each file lands as `.part` and is moved into place only after its size
  and sha256 match the source's published hash. A mismatch deletes it and fails.
- **Resumes.** Run the same command again after an interruption and it continues with a
  range request. A file already present and correct is skipped.
- **The floor.** Before it downloads, a pull measures the destination root and refuses when
  its files would leave the root under `[library] free_floor` (a tenth by default, `0`
  turns it off). A root whose space this platform cannot measure says the floor was not
  checked, and goes on: a guard that cannot measure does not claim to guard.
- **Shelves.** `berth models` reads each model for which shelf it is: chat, image, audio,
  or unshelved when the file states nothing. A root declared in `[library]` (say
  `shelf = "image"`) declares what it holds, and that beats a file's header. The SHELF
  column shows it, `--shelf image` lists one shelf, and the totals under the listing say
  how much of the library each shelf holds.
- **The floor's view: `berth storage`.** Each root's filesystem (an ntfs3 root shared with
  Windows reads as ntfs3, on both sides of the share), the space it has left against its
  floor, what it can share (hard links, symlinks, reflinks), the other operating systems
  it is shared with, the shelves it holds, and when a berth last scanned it. A root under
  its floor fails the check.
- **The in-root catalog.** A scan leaves `<root>/.berth/catalog.json`: what it resolved,
  from which machine, and when. On a root two operating systems share, the second boot
  reads the first's resolutions instead of re-guessing them. The scan always recomputes;
  the file is a snapshot for the other side, never a cache this side trusts.
- **`berth verify`** re-hashes pulled models against the manifest the pull wrote
  (`.berth-manifest.json`: source, name, ref, every hash), offline, and exits 1 on any change.
- Gated and private models read `$HF_TOKEN` (Hugging Face) or `$CIVITAI_API_KEY` (CivitAI).
- **`berth models`** reads every GGUF header (never the weights) in the tools' usual places
  and any `--dir` or `[models] dirs`. Split models list as one; vision projectors and MTP or
  draft heads list with the model they belong to (`--all` shows them on their own).
  `--usage 30d` adds requests and tokens per file from the harbor's request log, matched
  through llama-swap's config when that is how the model is served.
- **`berth models show NAME`** prints one model's header: architecture, context, tensor
  types, chat template.
- **`berth models export ollama NAME [--dry-run]`** writes a Modelfile and runs `ollama
  create`; a split model needs `llama-gguf-split --merge` first.

### Gateway (preview)
- **One port for every engine.** `berth gateway` proxies OpenAI-style requests to the engine
  that serves the requested model, streaming.
- **Cold starts under a lease.** An engine with `start` and `serves` in `berth.toml` starts on
  the first request that names one of its models, through the `berth run` path: lease, drain
  of the holder, measured release, then the start. One start per engine, however many
  requests arrive while it loads.
- **Last switch wins.** Give gateway-started engines the priority of the engine they replace
  (`priority = "foreground"` next to a foreground llama-swap) and each side drains the other on
  demand. A lower priority waits behind the holder instead.
- **Clear failures.** Unknown model: 404. A start the broker refuses or an engine that exits
  before it answers: 503 with the reason, the engine's last log line, a `Retry-After` and an
  error `code` a client can branch on: `outranked` (lost a juggle), `drained_while_starting`,
  `no_berth`, `not_ready`, `start_failed` or `no_start`. A model listed under
  `serves` by two engines fails `berth config check`.
- **API key.** Bearer token on every route; `berth key` prints it (created on first use, mode
  600). Loopback only; browser origins and non-loopback Host names are refused.

#### Switch engines by naming a model

Two engines that each want the whole card can't run together, and most harnesses can't start an engine
before they call it. `berth gateway` starts it for them. Give an engine a `start` command and the model names it
`serves` in `berth.toml`:

```toml
[[engine]]
name     = "strata"
type     = "llamacpp"
url      = "http://127.0.0.1:8080"
start    = ["/path/to/strata/.venv/bin/python", "serve/server.py", "--engine", "strata",
            "--config", "strata-262k.json", "--port", "8080"]
serves   = ["router-strata", "qwen3.8-flash-next-iq2_xs"]
priority = "foreground"   # equal to the engine it replaces, so the last model you ask for wins
```

Run `berth gateway` (loopback `127.0.0.1:9799`; a user unit is in `contrib/berth-gateway.service`) and send
your requests there with the node's API key as the bearer token. The gateway creates the key on first start
(`~/.config/berth/api.key`, mode 600) and `berth key` prints it, so a client that runs commands for its key
never has the secret written into its config:

```json
{ "baseUrl": "http://127.0.0.1:9799/v1", "apiKey": "!berth key" }
```

For each request the gateway:

1. reads the model name from the body;
2. hands the request straight to an engine that is up and lists that model (llama-swap and Ollama load
   their own models under their own leases);
3. otherwise starts the engine that `serves` it through the same path as `berth run`: it takes a lease,
   Berth drains the holder (llama-swap through its unload URL once its slots are idle, then SIGTERM, then SIGKILL), and the
   gateway waits until the new engine answers `/v1/models`;
4. proxies the request and streams tokens as the engine sends them.

Requests that arrive during a start wait on that one start. A model no engine serves gets a 404, and a
start the broker refuses comes back as a 503 with the broker's reason. A start waits for the card no longer
than `--ready` (10 minutes); if the holder has not left by then, the request fails and the start is
dropped, so it cannot take the card hours later. Asking llama-swap for one of its
models later drains the gateway's engine the same way, so either side can take the card back.

Measured on the reference RTX 4090 with an agent harness switching models between NInfer (through
llama-swap) and Strata (262K context):

| Switch | Drain of the holder | Request answered after |
|---|---|---|
| Strata on an empty card | none | 106 s (Strata's own cold load) |
| Strata to NInfer | Strata released in 2 s | 41 s |
| NInfer to Strata | NInfer released in 0.6 s | 110 s |

At no point did the two engines hold the card at the same time.

**Long waits and idle clients.** Cold starts take 41-110 s (table above), and `forward` writes zero
bytes while `ensure` drains the holder and starts the engine: both proxies stream once the engine sends
bytes (`FlushInterval = -1`) but send no keep-alive during the wait, and flushing a 200 early is unsafe
for non-streamed bodies. If a client with an idle-connection timeout (OpenWebUI's is ~40 s) drops the
wait first, raise its read timeout and prefer streamed requests. A stream-aware early flush needs its
own ticket. The gateway serves `/v1/chat/completions`,
`/v1/completions`, `/v1/embeddings` and a merged `/v1/models`, plus the ComfyUI surface: `POST /prompt`
queues a workflow (starting ComfyUI under a lease when it is down, holding the request until its `/queue`
answers), while `/ws`, `/queue`, `/history/`, `/view` and `/upload/` proxy to a running ComfyUI. Every route
needs the key. The gateway listens
on loopback only, refuses a Host that is not a loopback name (DNS rebinding) and refuses any request that
carries a browser `Origin`, so a web page cannot start an engine. ComfyUI already hands off at a job boundary under a lease (`--drain-comfyui`); a drain first unloads its models via `POST /free`, and `--keep-comfyui-up` leaves the server on its port for OpenWebUI instead of stopping it.

#### Open a stopped engine's web UI

A bookmark to ComfyUI or Strata's chat page is a dead link while that engine is stopped. Give the engine a
`gui` URL (where a browser reaches its web UI) next to its `start` command, and run the gateway with
`--gui-listen 127.0.0.1:9796`:

```toml
[[engine]]
name  = "comfyui"
type  = "comfyui"
url   = "http://127.0.0.1:8188"
start = ["comfy", "--workspace", "/path/to/comfy", "launch", "--", "--listen", "127.0.0.1", "--port", "8188"]
gui   = "https://box.tailnet.ts.net:8188/"
```

Point the bookmark (or a home-page tile) at `http://127.0.0.1:9796/gui/comfyui` instead:

- **ComfyUI answers:** the browser goes straight to its UI.
- **ComfyUI is stopped:** the page says so and lists what holds the card (`llama-swap (NInfer-27B)  active`), with a
  **Start** button. Opening the page takes nothing; only Start asks for the card.
- **After Start:** the page says what the start waits for, in the broker's terms: "Waiting for strata release:
  finishing its current request", "Waiting for llama-swap (NInfer-27B) release: freeing its memory", then
  "loading comfyui". It opens the UI once ComfyUI answers. A failed start shows its reason code and a retry button.
- `/gui/` lists every engine with a wake page.

A start never runs two engines on the card at once. Berth drains the holder only at its boundary: llama-swap once its
slots are idle (`drain_post`), ComfyUI once its queue is empty (`drain_comfyui`), and a server with no unload URL
once its `/slots` show idle (`drain_slots`, below). A pinned holder (a game, a render, `berth hold`) is never stopped;
the page says so and the start waits until it ends.

The wake page needs no API key: it can only start an engine you gave both `start` and `gui`. It listens on
loopback, accepts loopback Host names plus the gateway's `--allow-host` names (for `tailscale serve` in
front), and takes a start only as a POST from its own page, so another site cannot make a visitor's
browser drain the card.

#### Drain a server at its request boundary

An engine with `/slots` but no unload URL (Strata, a bare llama-server) would otherwise get SIGTERM mid-request when
another engine asks for the card. `drain_slots` (or `berth run --drain-slots URL`) names its base URL: a drain waits
until no slot reports `is_processing`, then stops it.

```toml
[[engine]]
name        = "strata"
type        = "llamacpp"
url         = "http://127.0.0.1:8080"
drain_slots = "http://127.0.0.1:8080"
```

### Visibility
- **Stowaways:** every process holding VRAM without a lease, with memory, identity and
  first-seen time; optional desktop notification and webhook. Report-only by design.
- **Contested grants:** a grant made while stowaways hold memory is flagged, not hidden.
- **Event log:** every request, wait, drain, release, free, stuck lease, stowaway and
  probe change, with gap-free sequence numbers; `berth events -f` follows it live.
- **The collisions board:** collisions avoided, VRAM handed off, drains, stuck releases
  caught, stowaways caught, contested grants, refusals. Each count links back to a
  recorded event. berthd keeps the board in `~/.local/state/berth/counters.json`
  (`--counters`), so a restart or a reboot carries the counts on. The harbor stores a
  reading a minute and its ticket counter shows the last 5 min, 15 min, hour, 24 h or
  7 days next to the all-time board (`GET /v1/counters?range=5m|15m|1h|24h|7d`).
- **Alerts:** one threshold table in `berth.toml` (`[alerts]`): warn and crit per key
  (`queue`, `ttft`, `tpot`, `p95`, `cache`, `reingest`, `vram`, `temp`, `power`, `kv`).
  The node evaluates it on its 5 s sample cycle, fires a desktop notification and webhook
  on entering a level, and logs `alert.raised` / `alert.cleared` events (`berth events
  -f`, the harbor log); a level stays until the value stays clear for `clear_polls`
  samples (default 3). GPU probe failure and aground leases are built-in danger alerts,
  always on. Default posture: no `[alerts]` table means no KPI alerts, and the
  notification channels are separately opt-in (`--notify`, `--webhook`). The harbor
  colors the same state the node notifies on: KPI tiles and GPU cells, a strip of the
  currently-breaching keys, and an overall LIVE / DEGRADED / OFFLINE state beside the
  connection pill.

### Telemetry (built in, no other tool needed)
- **GPU:** memory, utilisation, temperature, power against its limit, fan, SM and memory
  clocks, PCIe link, for **every card** in the machine with its real limits.
- **Host:** CPU, RAM, swap, load.
- **Engines:** every engine in the table above, found on its default port or configured on
  any other. Loaded models, decode and prefill tok/s, token totals,
  queue, draft acceptance, the context window per model (llama.cpp/NInfer `/props`, Strata
  `/health`, llama-swap per model proxy), and the live KV slot pool (`/slots`: per-slot
  context tokens against the window). An engine without those endpoints degrades to
  "no window / no slot data", never an error. An engine that was up and stops
  answering is shown as **unresponsive** (latched), distinct from one that never ran.
- **Requests:** a 24-hour request log from llama-swap; requests per minute, p50 and p95
  latency, prefill time, time per output token, cache and re-ingest share, a token ledger
  (1 h, 24 h, since start), a per-model leaderboard, and the most recent requests.
- **History:** the node keeps the last hour at 5-second resolution in memory. The harbor
  stores it on disk (SQLite, no C toolchain needed): 1-minute rows for 30 days, per engine
  the KV pressure (peak slot fill against the window) and the resident models' windows,
  hourly rows for 400 days, and the request log for 30 days, de-duplicated by request id.
  Databases written before the KV column is added are migrated in place on the first
  open; nothing is ever rewritten.
- **Model timeline:** when each model was loaded and unloaded on each engine, kept across
  restarts. A gap in the readings over 3 minutes is never drawn as loaded.
- The UI labels engine-reported figures as the engine's own, apart from what the driver
  measures.

### Interfaces
- **CLI:** one command tree with one output contract. `berth status`, `berth events -f`,
  `berth run`, `berth hold`, `berth claims`, `berth pull`, `berth verify`, `berth models`, `berth gateway`,
  `berth wire llama-swap`, `berth engine check`, `berth config check`, `berth config show`,
  `berth export`, `berth key`, `berth skill install`, `berth version`. Berth runs fully
  headless.

  ```sh
  berth wire llama-swap --config cfg.yaml          # plan: which models lack a wrapper
  berth wire llama-swap --config cfg.yaml --write  # apply; a backup stays beside the file
  berth wire llama-swap --config cfg.yaml --check  # exit 1 when a model is unwired
  berth config check --strict          # a warning fails the check, exit 1
  berth config show --format toml      # the evaluated config, ready to save
  berth engine check -v                # what each engine reports, right now
  berth -q status; echo $?             # exit codes only, for CI
  berth status --format json | jq .leases
  ```

  Results go to stdout, diagnostics go to stderr. `--format` is `human` (the default), `json` or
  `yaml`, and `berth config show` also takes `toml`, the config's own file format. `-v` adds
  detail to the human view, and `-q` suppresses stdout and leaves the exit code as the answer.
  Exit codes are 0 (done), 1 (validation or logic: an invalid config, a warning under `--strict`,
  an engine that did not answer, a lease the broker refused) and 2 (berth could not do its job:
  unknown flag, missing file, berthd unreachable). `berth run` passes the engine's own status
  through and exits 75 when no berth was granted. Every read-only command changes nothing.
- **Harbor web UI** (`berth harbor`): a departures board of every lease, a boat ticket
  per lease, the collisions counter, berths with measured memory, stowaways, the event
  log, and engine metrics. The tide (card memory by holder) and the model timeline share a
  1H / 6H / 24H / 7D / 30D picker; the Engines tab adds a queue KPI (the berth's own
  queue; each engine's own queue stays on its card), the KV pool (the live slots of each
  engine against their model windows, stored ranges the per-minute peak slot fill), context
  residency (the last requests' token figures against the model's window, plus the live
  KV sessions), a throughput chart, a 24H / 7D / 30D leaderboard and, in advanced mode, a
  storage panel. The top bar shows the overall state (LIVE / DEGRADED / OFFLINE) and a
  strip of the alerts currently breaching, colored from the node's own threshold state.
  Basic and advanced modes.
  Read-only, strict content security policy. It listens on loopback by default
  (`127.0.0.1:9798`); passing a non-loopback `--listen` address (e.g. `0.0.0.0:9798`;
  a specific interface IP works too) is the explicit opt-in to expose it on your
  network: the startup logs a warning and the page shows a banner on every screen while
  exposed. There is no authentication in this release: expose it only on a network you
  trust; phones on that network get the full read-only dashboard. To reach it from elsewhere
  without exposing it, keep the loopback listener and put a same-machine reverse proxy in
  front (`tailscale serve`, caddy), passing the proxy's hostname with `--allow-host`. A systemd user unit is
  in `contrib/berth-harbor.service`.
- **Prometheus** `/metrics` (loopback only) for Grafana and friends.
- **Footprint:** the node uses about 0.2% of one CPU core and ~21 MB of memory on the
  reference box; the harbor about 0.4% and ~23 MB, writing to disk once a minute.

---

## Support matrix

| Platform | Status |
|---|---|
| Linux + NVIDIA | Supported |
| Windows 10/11 + NVIDIA | Experimental: leases, process attribution, job-object drains and game claims work; no installer yet, and the driver reports no per-process memory under WDDM, so use whole-card leases |
| Linux + AMD (amdgpu) | Supported: sysfs for the card, /proc/<pid>/fdinfo for per-process VRAM |
| Intel, Apple | Not supported yet (the telemetry layer is an interface for later backends) |
| Multiple GPUs | Inventoried with real limits; leases schedule one card per daemon today |

### On an AMD card (amdgpu)

Berth reads the card through sysfs and attributes memory to processes through
`/proc/<pid>/fdinfo`, so it needs no `rocm-smi`, no ROCm runtime and no C
toolchain — the same single binary as on NVIDIA.

- `--gpu N` is the Nth **AMD** GPU (0 = the first), the ordinal `nvidia-smi`
  uses. The kernel's `cardN` also numbers cards that are not GPUs, so it is not
  exposed.
- `nvidia-smi` lists only **compute** processes; fdinfo lists every DRM client,
  so the compositor, browsers and editors show up too. The built-in pack tracks
  the desktop (`linux-desktop`: Hyprland, Xwayland, GNOME, KDE, sway, ...); add
  `[[claim]] action = "track"` for anything else you keep open, or it is
  reported as a stowaway.
- Fan speed is converted from RPM with the card's own maximum; telemetry the
  driver does not publish reads -1, never 0.

---

## Roadmap

| Phase | What it delivers | Status |
|---|---|---|
| P0 | Safety core: identity, measured release, stuck and contested detection, unprotected runs | **Done** |
| P1 | Node API: event log, collisions board, config, engine readers, `/metrics`, light probe | **Done** |
| P2 | Harbor web UI; native telemetry, request KPIs, ledger, leaderboard, stored history, measured against Speculum parity | **Done** |
| P3 | `berth hold` and claims for games and apps (**done**); drain and hold from the harbor, behind authentication (planned) | Partly done |
| P3.5 | Node gateway: one OpenAI-compatible port; engines start on demand under a lease and drain each other | **Preview** |
| P4 | Restart reconciliation: engines re-attach after a daemon restart, nothing left unprotected | **Done** |
| P5 | Fleet: `berth install --harbor <addr> --join <token>` on every machine; nodes push metrics into the harbor's store (already built for one box in P2) | Planned |
| P5.5 | Harbor router: one URL serves models from any node in the fleet | Planned |
| P6 | ComfyUI job-boundary handoff on Linux (**done**); ComfyUI started on demand through the gateway (**done**); Windows node (**experimental**) | Partly done |
| P7 | Offers: a waiting request can offer for an idle card on another machine; the holder decides | Planned |
| P8 | Multi-GPU scheduling: a lease table per card, "any card that fits" | Planned |
| P9 | Organisations: single sign-on, role-based access, audit log, Postgres, regional federation; a mobile app after | Planned |
| P10 | Opt-in auto-yield: a card's owner can pre-approve offers for their own card | Later |

Next, in this order: library v1 (shelves, roots with storage visibility, the in-root catalog,
the CivitAI pull), journal and undo under every mutating command, engine install and update
from upstream beyond the ComfyUI suite, Berth swapping models itself the way llama-swap does,
the gateway's full surface (Ollama-style and voice, on top of the preview's OpenAI-compatible
port and ComfyUI), the update watch and the migration manager, then the fleet, offers and
multi-GPU scheduling.

## What Berth never does

- Kill a process it did not start, or any process by default.
- Grant a card because a process exited, before the driver shows the memory free.
- Claim memory is reserved when it is only estimated to be free.
- Take a card from another user's machine without their consent (on one machine, only the
  priorities you set decide).
- Require a cloud account, telemetry upload, or any other software to work.

## Contributing

Issues and pull requests are welcome. [AGENTS.md](AGENTS.md) has the one command that proves a
change (build, format, vet, tests, lint, as CI runs them) and the invariants a change must
keep.

## License

[Apache License 2.0](LICENSE). The Berth name, the Pilot mascot and the artwork in `assets/`
are not covered by the license; see [NOTICE](NOTICE). Third-party components:
[THIRD_PARTY_NOTICES](THIRD_PARTY_NOTICES).
