# bicameral

**Frontier models do the logic. Open workers implement it.**

bicameral runs Claude Code with two kinds of models working together:

- **Claude** (the frontier model) reads the code, works out what to change,
  and checks the result.
- **An open model on your machine** (the worker) makes the edits, runs the
  tests and does the busywork.

The worker's tokens are free, and the files it reads stay on your machine.
Claude sees only the brief it writes and the worker's report.

The name comes from Julian Jaynes's *bicameral mind*: one half of the brain
gives instructions, the other carries them out.

## How it works

```
claude ──► bicameral ──┬─ worker requests ──► open model on your machine
                       └─ everything else ──► Claude (api.anthropic.com)
```

1. `bicameral` starts a small proxy and launches `claude` behind it.
2. It adds a **`local-worker`** subagent and a **`delegate-local`** skill to
   the session. The skill tells Claude which jobs to hand to the worker and
   how to brief it.
3. When Claude delegates, the proxy sends the worker's requests to your local
   model instead of the cloud. It strips your Anthropic credentials from
   those requests first.
4. When you quit Claude, the proxy stops too.

You don't need to change your projects or your Claude Code settings.

## What you need

- **[Claude Code](https://claude.com/claude-code)**, installed and logged in.
- **A local model server that speaks the Anthropic API** (`/v1/messages`).
  bicameral finds these by itself:

  | Engine    | Default address          |
  |-----------|--------------------------|
  | Splash    | `http://127.0.0.1:8010`  |
  | Ollama    | `http://127.0.0.1:11434` |
  | LM Studio | `http://127.0.0.1:1234`  |

  Other servers work too if you set `BICAMERAL_LOCAL_URL`. Servers that only
  speak the OpenAI API (`/v1/chat/completions`) won't work.

Pick a model that is good at tool use and coding. A small model will make
more mistakes; Claude checks the worker's results either way.

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/tudalex/bicameral/master/install.sh | sh
```

This installs the right binary for your OS and CPU (macOS or Linux, amd64 or
arm64) to `~/.local/bin`, after checking its checksum. Set
`BICAMERAL_VERSION=v0.1.0` to pin a version, or `BICAMERAL_INSTALL_DIR` to
install somewhere else.

<details>
<summary>Other ways to install</summary>

**By hand**, from the [releases page](https://github.com/tudalex/bicameral/releases):

```sh
# pick darwin_arm64, darwin_amd64, linux_amd64 or linux_arm64
curl -L https://github.com/tudalex/bicameral/releases/latest/download/bicameral_darwin_arm64.tar.gz | tar xz
mv bicameral_darwin_arm64/bicameral ~/.local/bin/
```

The macOS binaries aren't signed. If macOS blocks one you downloaded in a
browser, run `xattr -d com.apple.quarantine ~/.local/bin/bicameral`.

**From source** (Go 1.27+):

```sh
git clone https://github.com/tudalex/bicameral
cd bicameral
go install .        # puts it in $(go env GOPATH)/bin
```

The plugin is built into the binary, so there's nothing else to install.

</details>

## Use it

Start your local model server, then run `bicameral` wherever you would run
`claude`. Any arguments are passed straight through:

```sh
cd ~/some/project
bicameral                                  # same as `claude`
bicameral --continue
bicameral -p "rename foo to bar in pkg/"   # headless works too
```

Claude delegates to the worker on its own when a job is mechanical. You can
also ask directly: *"have the local worker run the tests and fix the
failures"*.

**Good jobs for the worker:** edits you can describe exactly, renames,
boilerplate, run-the-tests-and-fix loops, collecting files or command output.

**Claude keeps:** design decisions, unclear bugs, security-sensitive changes,
and checking the worker's diff before reporting success.

### Choosing the model

On start, bicameral looks for Splash, Ollama and LM Studio. If it finds more
than one, or an engine with several models, it asks you to pick:

```
bicameral: Local engine:
   1) Splash     http://127.0.0.1:8010  (1 model)
   2) Ollama     http://127.0.0.1:11434  (15 models)
Choose [1-2, default 1]:
```

Press Enter for the first option. To skip the questions, set the engine and
model yourself:

```sh
BICAMERAL_LOCAL_URL=http://127.0.0.1:11434 BICAMERAL_LOCAL_MODEL=devstral:24b bicameral
```

If no engine is running, bicameral warns you and starts anyway. Only the
worker fails until a model is up.

### Seeing what went where

Claude's interface takes over the terminal, so the proxy writes its log to a
file. Watch it from another terminal:

```sh
tail -f ~/Library/Caches/bicameral/proxy.log     # macOS
tail -f ~/.cache/bicameral/proxy.log             # Linux
```

```
local 200   41.3s POST /v1/messages model=claude-local-qwen class=subagent
cloud 200    3.2s POST /v1/messages model=claude-opus-5-5 class=main
```

`local` lines are the worker, `cloud` lines are Claude.

## Configuration

| Variable                | Default                       | What it does |
|-------------------------|-------------------------------|--------------|
| `BICAMERAL_LOCAL_URL`   | auto-detected                 | Local model server. Setting it skips detection |
| `BICAMERAL_LOCAL_MODEL` | asked on start                | Model name to use on that server |
| `BICAMERAL_PREFIX`      | `claude-local`                | Model names starting with this go to the local server |
| `BICAMERAL_LOG`         | `<cache dir>/bicameral/proxy.log` | Where the proxy log goes |
| `ANTHROPIC_BASE_URL`    | `https://api.anthropic.com`   | Where Claude's requests go. If you already use a gateway, bicameral sits in front of it |

### Running only the proxy

`bicameral serve` runs the proxy without launching Claude, for a long-lived
proxy or another Anthropic client. It listens on `127.0.0.1:8787` and logs to
the terminal:

```sh
bicameral serve [-listen ADDR] [-local URL] [-local-model NAME] [-prefix P] [-cloud URL]
ANTHROPIC_BASE_URL=http://127.0.0.1:8787 claude --plugin-dir ./plugin
```

In this mode `BICAMERAL_LOCAL_URL` defaults to Splash and
`BICAMERAL_LOCAL_MODEL` to `incoai/Qwen3.8-27B-Splash`; the flags override
both.

## The plugin

The `plugin/` folder is built into the binary and loaded for each session:

- **`agents/local-worker.md`**: the worker. It has `Read`, `Edit`, `Write`,
  `Bash`, `Glob` and `Grep`, and is told to stay on task, check its changes,
  and stop after two failed attempts.
- **`skills/delegate-local/SKILL.md`**: tells Claude what to delegate, how to
  write the brief, and to verify the result.

To send any other agent to the local model, give it a `model:` that starts
with `claude-local` (for example `claude-local-reviewer`).

## Limitations

- **No Remote Control.** Claude Code turns off Remote Control (`claude rc`,
  `--remote-control`) when `ANTHROPIC_BASE_URL` points anywhere but
  api.anthropic.com, so bicameral sessions can't be driven from the phone or
  claude.ai.
- **Unofficial.** Anthropic doesn't support routing Claude Code to
  non-Claude models. A Claude Code update can break this. Claude Code also
  prints a harmless `unrecognized_model` warning for the worker.
- **The first worker call is slow.** The local model has to read Claude
  Code's instructions and tool list before its first answer. Later calls
  reuse its cache and are much faster.

## Troubleshooting

- **`exec: "claude": executable file not found`**: Claude Code isn't on your
  `PATH`.
- **`warning: no local engine found`**: nothing answered on the default
  ports. Check with `curl http://127.0.0.1:11434/v1/models` (or your
  engine's port), or set `BICAMERAL_LOCAL_URL`.
- **The worker returns 404 or errors**: the server doesn't support
  `/v1/messages`, or doesn't know the model name. Look at the `local` lines
  in the proxy log.

## License

[MIT](LICENSE)
