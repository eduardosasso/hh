# hh — HTTPS here

Prefix any dev command. Get a stable `https://<name>.localhost` URL.

```
$ hh bun run dev
→ https://quip.localhost
```

No config. The folder name is the hostname. Every git worktree gets its own URL.

## Install

```bash
go install github.com/eduardosasso/hh/cmd/hh@latest
hh setup
```

`hh setup` runs once per machine. It asks for sudo one time and then:

1. On Linux, lets the binary bind ports 80 and 443 (`setcap`).
2. Installs the proxy daemon as a login service (launchd on macOS, systemd user unit on Linux).
3. Trusts the local root certificate in the OS trust store.
4. Offers to add one line to `~/AGENTS.md` so every agent starts dev servers with `hh`.
   Use `--agents` to say yes without a prompt, or `--no-agents` to skip it.

`hh init` does the same for one project: it adds the line to the project's `AGENTS.md`.
`hh` never writes to vendor folders such as `~/.claude`.

You can skip setup. `hh` starts the daemon on demand, and the URL still works with a
certificate warning.

## Use

| Command | Does |
|---|---|
| `hh <cmd...>` | Run `cmd` on a managed port. Print the URL. Route until exit. |
| `hh <label> -- <cmd...>` | Same, as `<label>.<host>`. |
| `hh url [--json]` | Print the URL for this folder. Starts nothing. |
| `hh ls` | List live routes. |
| `hh open` | Open the URL in the browser. |
| `hh logs` | Tail the daemon log. |
| `hh daemon` | Run the proxy in the foreground. |

Flags, before the command: `--name <n>`, `--port <p>`, `--route <path>=<port>` (repeatable).

The child gets `PORT`, `HH_PORT`, `HH_URL`, and `HH_HOST`. `hh` expands `$PORT` in the
command itself, so quote it (`'$PORT'`) and it works in every shell.

## Names

| Folder | Branch | URL |
|---|---|---|
| `~/code/quip` | any | `https://quip.localhost` |
| `~/code/overdew-wt-173`, worktree of `overdew` | `fix/recap-unread` | `https://fix-recap-unread.overdew.localhost` |
| `hh api -- bun run api` in `quip` | any | `https://api.quip.localhost` |

Every host also answers on `*.<host>`, so `acme.quip.localhost` reaches the `quip` app.
The port is a hash of the host in `40000–49999`. It stays the same run after run.

## Stacks

```bash
# Rails (puma reads PORT)
hh bin/rails server

# Go (read os.Getenv("PORT"))
hh go run .

# Node / Next.js (read process.env.PORT)
hh node server.js
hh npm run dev

# Vite (hh adds --port $PORT --strictPort)
hh vite

# Bun (Bun.serve reads PORT)
hh bun run dev

# Deno
hh deno serve --port '$PORT' main.ts

# Django
hh python manage.py runserver '$PORT'

# FastAPI (hh adds --port $PORT)
hh uvicorn main:app --reload

# Cloudflare Wrangler (hh adds --port $PORT)
hh wrangler dev

# Docker
hh docker run --rm -p '$PORT':80 nginx

# Static files (hh adds the port)
hh python3 -m http.server
```

To make it the default for a project, put the prefix in the dev script:

```json
{ "scripts": { "dev": "hh vite" } }
```

Vite with HMR on a separate port:

```bash
hh --route /hmr=24678 vite
```

## How it works

`hh daemon` embeds [Caddy](https://caddyserver.com). It listens on `:443` and `:80`
(which redirects to HTTPS), issues certificates from a local CA on first request, and
proxies each host to `localhost:<port>` with `X-Forwarded-*` headers. WebSockets pass
through. An unknown host gets a 503 page that lists live routes.

The wrapper registers `{host, port, pid, folder}` over `~/.hh/hh.sock`. The daemon drops
any route whose pid is gone within 5 seconds, so even `kill -9` leaves nothing behind.

State lives in `~/.hh`: `routes.json`, `daemon.log`, and the CA under `caddy/pki`.
