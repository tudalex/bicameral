# bicameral

Run Claude Code with two minds: Claude does the thinking, and a local model
does the mechanical work.

bicameral is a small HTTP proxy that sits between Claude Code and the API. It
sends requests for opted-in subagents (any model starting with `claude-local`)
to a local Anthropic-compatible engine. Everything else goes to the cloud
unchanged. It ships with a Claude Code plugin that adds a `local-worker`
subagent and a `delegate-local` skill, which teaches Claude when to hand work
to that subagent and how to brief it.

```
claude ──► bicameral ──┬─ model=claude-local* ──► local engine (e.g. Splash)
                       └─ everything else ──────► api.anthropic.com
```

Local requests have their `model` rewritten to `BICAMERAL_LOCAL_MODEL`, and
the proxy strips `Authorization`, `X-Api-Key` and `Cookie` before they leave,
so your Anthropic credentials never reach the local engine.

## Requirements

- **Go 1.27 or newer**, to build it (`go version` to check).
- **[Claude Code](https://claude.com/claude-code)**, with `claude` on your
  `PATH` and already logged in. bicameral launches it; it doesn't install it.
- **At least one local model server that speaks the Anthropic Messages API**
  (`POST /v1/messages` and `GET /v1/models`). bicameral finds these on their
  default ports:

  | Engine    | Address                  |
  |-----------|--------------------------|
  | Splash    | `http://127.0.0.1:8010`  |
  | Ollama    | `http://127.0.0.1:11434` |
  | LM Studio | `http://127.0.0.1:1234`  |

  Any other Anthropic-compatible server works too if you set
  `BICAMERAL_LOCAL_URL` (see [Configuration](#configuration)). An
  OpenAI-only server (`/v1/chat/completions`) won't work.

## Installation

### 1. Build the binary

```sh
git clone <this repo> bicameral
cd bicameral
go build -o bicameral .
```

This gives you a single `bicameral` executable in the repo. The plugin is
embedded in it, so you don't need to install the plugin separately.

### 2. Put it on your `PATH` (optional)

Either install it into Go's bin directory:

```sh
go install .                     # installs to $(go env GOPATH)/bin/bicameral
```

and make sure that directory is on your `PATH`
(`export PATH="$(go env GOPATH)/bin:$PATH"`), or copy the binary somewhere
already on it:

```sh
cp bicameral ~/.local/bin/
```

### 3. Start your local model server

Start Splash, Ollama or LM Studio (or several) and load or pull at least
one chat model. For LM Studio, turn on the local server (Developer tab, or
`lms server start`). Check that each one answers:

```sh
curl -s http://127.0.0.1:8010/v1/models    # Splash
curl -s http://127.0.0.1:11434/v1/models   # Ollama
curl -s http://127.0.0.1:1234/v1/models    # LM Studio
```

### 4. Check that routing works

Run the proxy on its own and send it one request for a local model:

```sh
bicameral serve -listen 127.0.0.1:8787 &

curl -s http://127.0.0.1:8787/v1/messages \
  -H 'content-type: application/json' \
  -H 'anthropic-version: 2023-06-01' \
  -d '{"model":"claude-local-test","max_tokens":20,
       "messages":[{"role":"user","content":"Say hi"}]}'

kill %1
```

The response should name the local model in its `model` field, e.g.
`"model":"incoai/Qwen3.8-27B-Splash"`, and the proxy should log a `local 200`
line. No API key is needed for this test, because local requests never go to
the cloud.

## Running

### Wrap Claude Code (the usual way)

Run `bicameral` wherever you'd run `claude`, with the same arguments:

```sh
cd ~/some/project
bicameral                        # same as `claude`
bicameral --continue             # any claude flags pass straight through
bicameral -p "rename foo to bar in pkg/" # including headless mode
```

bicameral then:

1. starts the proxy on a random `127.0.0.1` port;
2. extracts the plugin to a temp dir;
3. runs `claude --plugin-dir <dir> [your args...]` with `ANTHROPIC_BASE_URL`
   pointing at the proxy;
4. when Claude exits, stops the proxy, deletes the temp dir and exits with
   Claude's exit code.

In the session you'll see the `local-worker` agent and the
`bicameral:delegate-local` skill. Claude uses them by itself for mechanical
work, or you can ask it directly ("have the local worker run the tests and
fix the failures").

### Choosing the engine and model

Unless `BICAMERAL_LOCAL_URL` is set, bicameral checks Splash, Ollama and
LM Studio on their default ports before launching Claude:

- If more than one is running, it asks which to use:

  ```
  bicameral: Local engine:
     1) Splash     http://127.0.0.1:8010  (1 model)
     2) Ollama     http://127.0.0.1:11434  (15 models)
     3) LM Studio  http://127.0.0.1:1234  (5 models)
  Choose [1-3, default 1]:
  ```

  If only one is running, it uses that one and prints its name.
- Then it asks which of that engine's models to use. Embedding models are
  left out. Set `BICAMERAL_LOCAL_MODEL` to skip this question.
- Press Enter to accept the default (1). If stdin isn't a terminal (e.g. it's
  piped), bicameral takes the first engine and model in the order above and
  prints what it chose.
- To skip detection completely, set `BICAMERAL_LOCAL_URL` (and usually
  `BICAMERAL_LOCAL_MODEL`):

  ```sh
  BICAMERAL_LOCAL_URL=http://127.0.0.1:11434 BICAMERAL_LOCAL_MODEL=devstral:24b bicameral
  ```

If no engine is running, bicameral prints a warning and starts anyway. Only
`local-worker` calls fail until an engine is up.

### Watching the logs

The terminal belongs to Claude's TUI, so proxy logs go to a file:
`~/Library/Caches/bicameral/proxy.log` on macOS
(`$XDG_CACHE_HOME/bicameral/proxy.log`, usually `~/.cache/...`, on Linux),
or `BICAMERAL_LOG` if it's set. Follow it from another terminal to see what
went where:

```sh
tail -f ~/Library/Caches/bicameral/proxy.log
```

```
local 200   41.3s POST /v1/messages model=claude-local-qwen class=...
cloud 200    3.2s POST /v1/messages model=claude-opus-5-5 class=...
```

Each line shows the route, status, latency, method, path, model and request
class.

### Run just the proxy

To keep a long-running proxy, or to use it with another Anthropic client:

```sh
bicameral serve [-listen 127.0.0.1:8787] [-cloud URL] [-local URL] \
                [-prefix claude-local] [-local-model NAME]
```

In this mode logs go to stderr. Point Claude Code at the proxy yourself and
load the plugin from the repo:

```sh
ANTHROPIC_BASE_URL=http://127.0.0.1:8787 claude --plugin-dir ./plugin
```

### Troubleshooting

- **`starting claude: exec: "claude": executable file not found`**: Claude
  Code isn't on your `PATH`.
- **`warning: no local engine found`**: none of Splash, Ollama or LM Studio
  answered on its default port with at least one model. Run the `curl`
  commands from step 3. If your engine uses a different port, set
  `BICAMERAL_LOCAL_URL`.
- **`warning: local model not reachable`**: you set `BICAMERAL_LOCAL_URL`,
  but nothing answers there, or `GET /v1/models` returns a 5xx.
- **`local-worker` returns 404 or errors**: the engine probably doesn't
  implement `/v1/messages`, or doesn't recognise the name in
  `BICAMERAL_LOCAL_MODEL`. Check the `local` lines in the proxy log.
- **Running bicameral from inside a bicameral session** (for example from a
  Bash tool call): the inner proxy picks up the outer `ANTHROPIC_BASE_URL` as
  its cloud upstream. That works, because the proxies chain, but the
  `rest ->` address in the log will be a localhost port.

## Configuration

| Variable                | Default                        | Meaning                                      |
|-------------------------|--------------------------------|----------------------------------------------|
| `BICAMERAL_LOCAL_URL`   | auto-detected (wrap) / `http://127.0.0.1:8010` (serve) | Local Anthropic-compatible upstream. Setting it turns off detection |
| `BICAMERAL_LOCAL_MODEL` | picked from the engine (wrap) / `incoai/Qwen3.8-27B-Splash` (serve) | Model name sent to the local upstream |
| `BICAMERAL_PREFIX`      | `claude-local`                 | Model prefix routed to the local upstream    |
| `BICAMERAL_LOG`         | `<user cache dir>/bicameral/proxy.log` | Proxy log file (wrap mode only)      |
| `ANTHROPIC_BASE_URL`    | `https://api.anthropic.com`    | Cloud upstream. If you already use a gateway, bicameral chains in front of it |

In `serve` mode, the flags override the matching environment variables.

## The plugin

`plugin/` is embedded at build time and loaded automatically in wrap mode.

- **`agents/local-worker.md`**: a subagent with `model: claude-local-qwen`, so
  the proxy routes it to the local engine. It gets `Read`, `Edit`, `Write`,
  `Bash`, `Glob` and `Grep`, and is told to stay in scope, verify its changes
  and stop after two failed attempts.
- **`skills/delegate-local/SKILL.md`**: guidance for the main Claude session.
  Delegate fully specified, mechanical work (targeted edits, renames,
  test-fix loops, gathering output). Keep design, unclear bugs and
  security-sensitive work for Claude. Always verify the worker's diff.

To route another agent to the local model, give it a `model:` that starts
with the prefix (e.g. `claude-local-anything`).
