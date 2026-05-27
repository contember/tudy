# Tudy

A local development proxy that uses AI to automatically route `*.localhost` domains to your running services. No config files, no port numbers to remember -- just visit `myapp.localhost` and the proxy figures out the rest.

Supports any OpenAI-compatible API (OpenRouter, Ollama, LM Studio, vLLM, etc.).

## Features

- **Dynamic hostname resolution** using any OpenAI-compatible LLM API
- **Automatic service discovery**:
  - Local processes with open ports (Linux: `ss`/`/proc`, macOS: `lsof`)
  - Docker containers (via Docker API), including listening port detection
- **Direct Docker container access** on macOS via [docker-mac-net-connect](https://github.com/chipmk/docker-mac-net-connect) -- no published ports needed
- **Cross-platform**: Works on Linux and macOS
- **On-demand TLS certificates** for `*.localhost` domains
- **Persistent mapping cache** (JSON file)
- **Debug dashboard** at `proxy.localhost`
- **Inter-service proxy** for service-to-service communication (`/_proxy/serviceName/path`)
- **REST API** for managing mappings (`/_api/mappings/`)
- **CLI** with `setup`, `status`, `start`, `stop`, `restart`, `trust`, `update`, `uninstall` commands
- **Self-update** via `tudy update`

## Installation

### Quick Install (macOS/Linux)

```bash
curl -fsSL https://raw.githubusercontent.com/contember/tudy/main/install.sh | bash
tudy setup
```

The `setup` command walks you through configuring your API key, Docker networking, trusting the HTTPS certificate, and starting the proxy.

You'll need an [OpenRouter](https://openrouter.ai/) API key (or any OpenAI-compatible API).

### Linux (Docker)

```bash
export LLM_API_KEY=your-key
docker compose up -d
```

Quick smoke test (skips cert validation):

```bash
curl -k https://myapp.localhost
```

To use Tudy from a browser without `ERR_CERT_AUTHORITY_INVALID`, trust Caddy's local root CA — see [Trusting the certificate (Linux)](#trusting-the-certificate-linux) below.

> **Note:** On macOS, Docker cannot discover local processes outside the container. Native installation is required for full process discovery.

### Alternative Installation

<details>
<summary>Manual download</summary>

Download from [Releases](../../releases), then:

```bash
tar xzf tudy-darwin-arm64.tar.gz
sudo cp cli /usr/local/bin/tudy
sudo cp caddy /usr/local/bin/tudy-bin
tudy setup
```
</details>

<details>
<summary>Build from source</summary>

```bash
# Build the Caddy binary with the plugin
go install github.com/caddyserver/xcaddy/cmd/xcaddy@latest
xcaddy build --with github.com/contember/tudy/llm_resolver=./llm_resolver

# Build the CLI
cd cmd/cli && go build -o tudy .
```
</details>

<details>
<summary>Using a local LLM (Ollama, etc.)</summary>

Set these in your env file (`/usr/local/etc/tudy/env`) or export them:

```bash
LLM_API_KEY=your-key
LLM_API_URL=http://localhost:11434/v1/chat/completions
MODEL=llama3.2
```
</details>

## Usage

Start a dev server on any port:

```bash
cd ~/projects/myapp
npm run dev  # listening on port 5173
```

Then open `https://myapp.localhost` in your browser. The proxy matches the hostname to your running process based on the project directory name, command, and port.

### Examples

| Hostname | Matches |
|---|---|
| `myapp.localhost` | Process running in `~/projects/myapp` |
| `api.myproject.localhost` | Backend service in `myproject` directory |
| `postgres-app.localhost` | Docker container named `postgres-app` |

### Query Parameters

| Parameter | Description |
|---|---|
| `?force` | Force re-resolution (bypass cache) |
| `?prompt=text` | Provide additional context to the LLM |

### Inter-Service Proxy

For frontend apps that need to reach a related backend:

```
https://myapp.localhost/_proxy/api/endpoint
```

This resolves `api` as a related service to `myapp` (e.g., a backend in the same project directory) and proxies the request.

### Dashboard

Visit `https://proxy.localhost` for a live dashboard split into three pages:

- **Activity** (default) — one row per route with hostname, target, port, a sparkline of request rate over the last 5 minutes, request/error counts, and last-seen time. The row updates live via SSE — no page refresh needed. Click a sparkline to jump to that route's logs. Active and idle routes are grouped; mapped routes can have their target or port edited inline.
- **Discovery** (`/discovery`) — current local processes (by port + working directory) and running Docker containers (image, ports, IP) that the resolver sees when picking targets.
- **Logs** (`/logs`) — per-request entries with method, host, path, status pill, and duration; resolver/tunnel events mixed in. A clock-aligned timeline at the top is **drag-selectable**: drag across a range to filter logs to that window. Host links on each row apply a host filter. Active filters appear as chips with independent clear links.

### Mappings API

| Endpoint | Method | Description |
|---|---|---|
| `/_api/mappings/` | GET | List all mappings |
| `/_api/mappings/{hostname}` | GET | Get a specific mapping |
| `/_api/mappings/{hostname}` | PUT | Set a manual mapping |
| `/_api/mappings/{hostname}` | DELETE | Delete a mapping |

```bash
# Set a manual mapping
curl -sk -X PUT https://proxy.localhost/_api/mappings/myapp.localhost \
  -H 'Content-Type: application/json' \
  -d '{"type":"process","target":"localhost","port":3000}'

# Delete a mapping
curl -sk -X DELETE https://proxy.localhost/_api/mappings/myapp.localhost
```

## CLI

```
tudy setup       # Interactive first-time setup (provider, API key, Docker, TLS, start)
tudy status      # Show proxy status with service discovery
tudy start       # Start the proxy
tudy stop        # Stop the proxy
tudy restart     # Restart the proxy
tudy doctor      # Diagnose configuration, LLM credentials, and proxy health
tudy trust       # Trust the HTTPS certificate
tudy update      # Update tudy to the latest version
tudy uninstall   # Fully remove tudy from the system
tudy logs        # Tail the proxy log file
tudy version     # Show tudy version
```

#### Setup subcommands

For quick post-install changes that don't need the full wizard:

```
tudy setup llm-api-url               # provider chooser (URL only)
tudy setup llm-api-url <url>         # set endpoint directly
tudy setup llm-model                 # prompt for model id
tudy setup llm-model <name>          # set model directly
```

Both restart the running proxy so the change takes effect immediately (Caddy
only reads `LLM_API_URL` / `MODEL` at startup).

#### tudy doctor

Walks through every layer that has to be right for routing to work:
config files, the LLM endpoint (with a real `200 OK` ping against your
key), the launchd service, TLS trust, the dashboard, and Docker
networking. Exits non-zero if anything fails, so it's scriptable. Use it
when "I changed providers and routing stopped working" or "everything
worked yesterday."

For anything else (raw Caddy commands, debug invocations), use the explicit
`caddy` pass-through. The env file is sourced first so `{$LLM_API_KEY}` etc.
resolve when Caddy reads the Caddyfile.

```bash
tudy caddy run            # Runs Caddy in foreground
tudy caddy list-modules
tudy caddy version        # Caddy's version (vs. 'tudy version' for tudy's)
```

## Docker Networking (macOS)

On macOS, Docker containers run inside a VM so their IPs aren't directly reachable. Tudy supports two modes:

- **With [`docker-mac-net-connect`](https://github.com/chipmk/docker-mac-net-connect)** (recommended): Containers are reachable by IP without publishing ports.
- **Without**: Containers need published ports (`-p 8080:8080`). Tudy uses published port mappings to route traffic.

Install it with:

```bash
brew install chipmk/tap/docker-mac-net-connect
sudo brew services start chipmk/tap/docker-mac-net-connect
```

Or run `tudy setup` which offers to install it automatically.

## Configuration

### Environment Variables

| Variable | Default | Description |
|---|---|---|
| `LLM_API_KEY` | *(required)* | API key for the LLM provider |
| `LLM_API_URL` | `https://openrouter.ai/api/v1/chat/completions` | OpenAI-compatible chat completions endpoint |
| `MODEL` | `anthropic/claude-haiku-4.5` | Model to use for routing decisions |
| `COMPOSE_PROJECT` | | Own Docker Compose project name (filtered from discovery) |

### LLM providers

Tudy speaks the OpenAI chat-completions protocol — `POST <url>` with
`Authorization: Bearer <key>`, body `{model, messages, response_format}`,
parsing `choices[0].message.content`. Any endpoint that follows that
shape works. Examples:

| Provider | `LLM_API_URL` | `MODEL` |
|---|---|---|
| OpenRouter (default) | `https://openrouter.ai/api/v1/chat/completions` | `anthropic/claude-haiku-4.5`, `openai/gpt-4.1-mini`, etc. |
| OpenAI direct | `https://api.openai.com/v1/chat/completions` | `gpt-4o-mini`, `gpt-4.1-mini` |
| Cloudflare AI Gateway | `https://gateway.ai.cloudflare.com/v1/<account>/<gateway>/compat/chat/completions` | depends on routed provider |
| Self-hosted / Groq / Together / Ollama (OAI bridge) | their `…/v1/chat/completions` | their model id |

Anthropic's native API (`api.anthropic.com/v1/messages`) is **not**
compatible — its request and response shape differ. Use Anthropic models
via OpenRouter or Cloudflare AI Gateway instead.

Pick a provider during `tudy setup`, or switch later with
`tudy setup llm-api-url`. After changing the endpoint, run
`tudy doctor` to confirm the new key actually works against it.

### Config Files

Config is stored in `/usr/local/etc/tudy/` (created by `tudy setup`):

| File | Purpose |
|---|---|
| `env` | Environment variables (`LLM_API_KEY`, etc.) |
| `Caddyfile` | Caddy configuration (auto-generated) |

### Service Management

```bash
tudy start    # Start the proxy
tudy stop     # Stop via admin API (no sudo needed)
tudy restart  # Hot reload if possible, full restart otherwise
```

Logs: `~/Library/Logs/tudy.log` (macOS)

## Trusting the certificate (Linux)

Tudy uses Caddy's internal CA to issue short-lived certs for `*.localhost`. Browsers don't trust this CA by default. To get rid of `ERR_CERT_AUTHORITY_INVALID`, add the root CA to your trust stores.

Extract the root CA from the running container:

```bash
docker compose cp tudy:/data/pki/authorities/local/root.crt /tmp/tudy-root-ca.crt
```

Install it system-wide (curl, most CLI tools, Chrome/Edge via system bundle):

```bash
sudo cp /tmp/tudy-root-ca.crt /usr/local/share/ca-certificates/tudy-root-ca.crt
sudo update-ca-certificates
```

Add it to the NSS database (Chrome/Chromium on Linux read this in addition to / instead of the system bundle):

```bash
certutil -d sql:$HOME/.pki/nssdb -A -t "CT,C,C" -n "Tudy Root CA" -i /tmp/tudy-root-ca.crt
```

(Install `libnss3-tools` on Debian/Ubuntu if `certutil` is missing.)

Firefox uses its own trust store — import `/tmp/tudy-root-ca.crt` via *Settings → Privacy & Security → Certificates → View Certificates → Authorities → Import* and check "Trust this CA to identify websites".

Fully restart your browser after importing (closing the window is not enough — kill all processes).

The root CA is persisted in the `caddy_data` volume, so it survives container restarts and rebuilds. You only need to do this once.

## Troubleshooting

First step for any "it stopped working" question: run **`tudy doctor`**. It
verifies the config files, pings the configured LLM endpoint with your
key, checks proxy / launchd / TLS state, and (on macOS) Docker
networking. Each failed check comes with a one-line fix hint.

A few common symptoms:

- **Routes that worked yesterday now return 502** — the LLM call is
  failing. `tudy doctor` will show whether it's an auth error (rotate
  the key) or rate-limit (`tudy setup llm-model` to a cheaper model).
- **`ERR_CERT_AUTHORITY_INVALID`** — run `tudy trust`. On Linux, see the
  manual steps below.
- **Container routing broken on macOS after a Docker restart** — the
  dmnc tunnel often dies after Docker upgrades. The dashboard shows a
  banner; `sudo brew services restart docker-mac-net-connect` fixes it.
- **Dashboard at `proxy.localhost` shows old data** — the page itself
  updates live via SSE every second. If it looks stale, the SSE
  connection dropped (status dot in the header goes dim); reload.

## How It Works

1. Request arrives with a hostname (e.g., `api.myproject.localhost`)
2. Module checks the mapping cache
3. If not cached, it:
   - Discovers local processes with open ports
   - Discovers running Docker containers (including listening ports via `/proc/net/tcp`)
   - Calls the LLM with hostname + service list
   - LLM returns the best matching target
   - Result is cached
4. Request is proxied to the resolved target

## Development

```bash
# Run with hot reload (requires xcaddy)
xcaddy run --config Caddyfile

# Build the Caddy binary with the plugin
xcaddy build --with github.com/contember/tudy/llm_resolver=./llm_resolver

# Build the CLI
cd cmd/cli && go build -o tudy .

# Run tests
cd llm_resolver && go test ./...

# Build Docker image
docker build -t tudy .
```

### Project Structure

```
llm_resolver/            # Caddy module (Go package)
  module.go              # Caddy module registration
  handler.go             # HTTP middleware, dashboard, API
  resolver.go            # LLM resolution logic
  cache.go               # Persistent mapping storage
  network_tunnel_darwin.go  # docker-mac-net-connect detection
  discovery/             # Service discovery
    docker.go            # Docker container discovery
    processes.go         # Local process discovery
cmd/shared/              # Shared utilities
cmd/cli/                 # CLI binary (tudy command)
```

## License

MIT
