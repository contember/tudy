# Tudy

A local development proxy that uses AI to automatically route `*.localhost` domains to your running services. No config files, no port numbers to remember -- just visit `myapp.localhost` and the proxy figures out the rest.

Supports any OpenAI-compatible API (OpenRouter, Ollama, LM Studio, vLLM, etc.).

## Features

- **Dynamic hostname resolution** using any OpenAI-compatible LLM API
- **Automatic service discovery**:
  - Local processes with open ports (Linux: `ss`/`/proc`, macOS: `lsof`)
  - Docker containers (via Docker API)
- **Cross-platform**: Works on Linux and macOS
- **On-demand TLS certificates** for `*.localhost` domains
- **Persistent mapping cache** (JSON file)
- **Debug dashboard** at `proxy.localhost`
- **Inter-service proxy** for service-to-service communication (`/_proxy/serviceName/path`)
- **REST API** for managing mappings (`/_api/mappings/`)
- **CLI** with `setup`, `status`, `start`, `stop`, `restart`, `trust` commands
- **macOS menu bar app** for quick access

## Installation

### macOS (Homebrew)

```bash
brew tap contember/tudy https://github.com/contember/tudy
brew install tudy
tudy setup
```

The `setup` command walks you through configuring your API key, trusting the HTTPS certificate, and starting the proxy.

You'll need an [OpenRouter](https://openrouter.ai/) API key (or any OpenAI-compatible API).

> **Note:** On macOS, Docker cannot discover local processes outside the container. Native installation is required for full process discovery.

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

### Alternative Installation

<details>
<summary>Install script (macOS/Linux)</summary>

```bash
curl -fsSL https://raw.githubusercontent.com/contember/tudy/main/install.sh | bash
```

Handles macOS Gatekeeper automatically.
</details>

<details>
<summary>Manual download</summary>

Download from [Releases](../../releases), then:

```bash
tar xzf caddy-darwin-arm64.tar.gz
sudo LLM_API_KEY=your-key ./caddy run --config Caddyfile
```
</details>

<details>
<summary>Build from source</summary>

```bash
go install github.com/caddyserver/xcaddy/cmd/xcaddy@latest
xcaddy build --with github.com/contember/tudy/llm_resolver=./llm_resolver
LLM_API_KEY=your-key ./caddy run --config Caddyfile
```
</details>

<details>
<summary>Using a local LLM (Ollama, etc.)</summary>

```bash
export LLM_API_KEY=your-key
export LLM_API_URL=http://localhost:11434/v1/chat/completions
export MODEL=llama3.2
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

Visit `https://proxy.localhost` to see all current route mappings, discovered processes, and Docker containers. You can delete stale mappings from here.

### Mappings API

| Endpoint | Method | Description |
|---|---|---|
| `/_api/mappings/` | GET | List all mappings |
| `/_api/mappings/{hostname}` | GET | Get a specific mapping |
| `/_api/mappings/{hostname}` | PUT | Set a manual mapping |
| `/_api/mappings/{hostname}` | DELETE | Delete a mapping |

```bash
# Set a manual mapping
curl -X PUT https://any.localhost/_api/mappings/myapp.localhost \
  -d '{"type":"process","target":"localhost","port":3000}'

# Delete a mapping
curl -X DELETE https://any.localhost/_api/mappings/myapp.localhost
```

## CLI

The `tudy` command handles proxy management and delegates all other commands to the underlying Caddy binary.

```
tudy setup       # Interactive first-time setup
tudy status      # Show proxy status
tudy start       # Start the proxy
tudy stop        # Stop the proxy
tudy restart     # Restart the proxy
tudy trust       # Trust the HTTPS certificate
```

All other commands (`run`, `version`, `list-modules`, etc.) are passed through to Caddy:

```bash
tudy version     # Shows Caddy version
tudy run         # Runs Caddy in foreground (env file sourced automatically)
```

## macOS Menu Bar App

On macOS, a menu bar app is installed alongside the proxy. It shows proxy status, lets you start/stop the service, configure your API key, and trust the certificate from the menu bar.

The app is installed at `$(brew --prefix)/opt/tudy/Tudy.app`. Add it to System Settings > General > Login Items to start automatically.

## Configuration

### Environment Variables

| Variable | Default | Description |
|---|---|---|
| `LLM_API_KEY` | *(required)* | API key for the LLM provider |
| `LLM_API_URL` | `https://openrouter.ai/api/v1/chat/completions` | OpenAI-compatible chat completions endpoint |
| `MODEL` | `anthropic/claude-haiku-4.5` | Model to use for routing decisions |
| `COMPOSE_PROJECT` | | Own Docker Compose project name (filtered from discovery) |

### Config Files

Homebrew installations store config in `$(brew --prefix)/etc/tudy/`:

| File | Purpose |
|---|---|
| `env` | Environment variables (`LLM_API_KEY`, etc.) |
| `Caddyfile` | Caddy configuration |

### Caddyfile Directives

```caddyfile
llm_resolver {
    api_key {env.LLM_API_KEY}
    api_url {env.LLM_API_URL}
    model anthropic/claude-haiku-4.5
    cache_file /data/mappings.json
    compose_project myproject
}
```

### Service Management

```bash
# Via brew services (recommended)
sudo brew services start tudy
sudo brew services stop tudy

# Or via the CLI
tudy start
tudy stop
```

Logs: `~/Library/Logs/Homebrew/tudy.log` (macOS)

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

## How It Works

1. Request arrives with a hostname (e.g., `api.myproject.localhost`)
2. Module checks the mapping cache
3. If not cached, it:
   - Discovers local processes with open ports
   - Discovers running Docker containers
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

# Build the menubar app (macOS only)
cd cmd/menubar && go build -o menubar .

# Run tests
go test ./...

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
  discovery/             # Service discovery
    docker.go            # Docker container discovery
    processes.go         # Local process discovery
cmd/cli/                 # CLI binary (tudy command)
cmd/menubar/             # macOS menu bar app
Formula/                 # Homebrew formula
```

## License

MIT
