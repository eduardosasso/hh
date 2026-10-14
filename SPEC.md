# hh — HTTPS here

One binary. Prefix any dev command. Get a stable `https://<name>.localhost` URL.

```
hh bun run dev
→ https://quip.localhost
```

## Problem

Every local app lands on `localhost:3000`, `3001`, `3002`. Ports collide. HTTPS needs
a hand-written proxy config per project. Agents guess ports and get lost. Worktrees of
the same repo share one hostname and one port.

## Goals

- Zero config per project. The folder name is the hostname.
- Real HTTPS with no browser warning.
- Any stack. If it listens on a port, it works.
- Every worktree gets its own URL.
- Agents learn one rule: prefix with `hh`, read the URL.

## Non-goals (v1)

- Raw TCP services such as Postgres.
- Sharing a URL outside the machine.
- Windows. See "Platforms".

## Install

```bash
brew install hh
hh setup
```

`hh setup` runs once per machine:

1. Trusts the internal root certificate in the OS trust store. One sudo prompt.
2. On Linux, runs `setcap cap_net_bind_service` on the binary. Same prompt.
3. Installs the daemon as a login service: launchd on macOS, systemd user unit on Linux.
4. Asks to add the agent rule to `~/AGENTS.md`. See "Agents".

Step 4 is an opt-in prompt. Everything else is silent.

## Commands

| Command | Does |
|---|---|
| `hh <cmd...>` | Run `cmd` on a managed port. Print the URL. Route until exit. |
| `hh <label> -- <cmd...>` | Same, as `<label>.<host>`. |
| `hh url` | Print the URL for the current folder. Starts nothing. |
| `hh url --json` | Same, as `{ "url", "host", "port" }`. |
| `hh ls` | List live routes: host, port, pid, uptime, folder. |
| `hh who [port]` | List listening TCP ports: port, pid, command, and host and folder for hh apps. With a port, show only it; exit 1 if nothing holds it. |
| `hh open` | Open the URL in the browser. |
| `hh init` | Bake the prefix into the project's dev script. See "Agents". |
| `hh setup` | Machine setup. See "Install". |
| `hh daemon` | Run the proxy in the foreground. Used by the service. |
| `hh logs` | Tail daemon logs. |

Flags on `hh <cmd>`:

| Flag | Does |
|---|---|
| `--name <n>` | Override the derived name. |
| `--port <p>` | Use a fixed port instead of an allocated one. |
| `--route <path>=<port>` | Send `path` to another local port. Repeatable. Covers split Vite HMR setups. |

## Naming

The host is derived. Nothing is configured. One folder, one URL.

1. `base` = basename of the git root. No git: basename of cwd.
2. Slugify: lowercase, every run of characters outside `[a-z0-9]` becomes `-`, trim `-`.
3. Main checkout: `host = <base>.localhost`. The branch does not matter. Switching
   branches never changes the URL.
4. Worktree: `base` comes from the main repo folder, the parent of
   `git rev-parse --git-common-dir`. The subdomain is the branch, slugified, `/` to `-`.
   `host = <branch>.<base>.localhost`. A worktree is detected when `--git-dir` and
   `--git-common-dir` differ.
5. A label prepends: `<label>.<host>`.

Examples:

| Folder | Branch | Host |
|---|---|---|
| `~/code/quip` | main | `quip.localhost` |
| `~/code/quip` | `feat/auth` | `quip.localhost` |
| `~/code/overdew-wt-173` (worktree of `overdew`) | `fix/recap-unread` | `fix-recap-unread.overdew.localhost` |
| `hh api -- bun run api` in `quip` | main | `api.quip.localhost` |

Every host also registers `*.<host>`, so tenant subdomains like
`acme.quip.localhost` reach the same app with no flag. Caddy matches the most
specific host, so `wt-173.overdew.localhost` wins over `*.overdew.localhost`.

Collision: two different folders that derive the same host. The second one fails
with the first one's folder in the message and a hint to use `--name`.

## Runtime

When `hh <cmd>` runs:

1. Derive the host.
2. Pick the port. Hash the host into `40000–49999`. If busy, take the next free port.
   Same host, same port, run after run. Agents and `.env` files can depend on it.
3. Ensure the daemon is up. If not, start it. No sudo.
4. Register `{ host, port, pid, cwd, routes }` with the daemon over a unix socket.
5. Print one line to stdout: `→ https://<host>`. Then exec `cmd` with inherited stdio.
6. Watch the child's process tree. See "Port discovery".
7. On exit or signal, stop leftover processes, then deregister. The exit code is the child's.

Environment passed to the child:

| Var | Value |
|---|---|
| `PORT` | the allocated port |
| `HH_URL` | `https://<host>` |
| `HH_HOST` | `<host>` |
| `HH_PORT` | same as `PORT` |

`$PORT` in the command line is expanded by `hh` before exec, so it works in every shell.

### Profiles

Tools that ignore `PORT` get the flag added for them. `hh` matches the first word of
the command.

| First word | Added |
|---|---|
| `vite` | `--port $PORT --strictPort` |
| `wrangler` | `--port $PORT` |
| `uvicorn` | `--port $PORT` |
| `php` (`-S`) | `localhost:$PORT` |
| `python -m http.server` | `$PORT` |

Rails, Next, Bun, Deno, Go, Django, Flask, Phoenix read `PORT` or take `$PORT`.
No profile needed. A user-supplied port flag always wins over the profile.

### Port discovery

Many dev scripts ignore `PORT` and bind a fixed port such as `8787` or `3000`.
`hh` finds the port the app really listens on.

- Every 250 ms while the child lives, `hh` reads the child's process tree
  (`ps -A -o pid=,ppid=,comm=`) and the TCP sockets those pids listen on, on loopback
  or all interfaces. macOS: `lsof -nP -iTCP -sTCP:LISTEN -a -p <pids> -F pn`.
  Linux: `/proc/net/tcp` and `/proc/net/tcp6`, inodes mapped to pids through
  `/proc/<pid>/fd`. Polling for a port stops at the first hit.
- `PORT` itself wins if the app listens on it. `--route` ports are skipped. Else the
  lowest port wins.
- Same as `PORT`: nothing to say.
- Different: the wrapper registers the route again with the same pid and the new port.
  The daemon replaces the route, keeps its start time, and reloads Caddy. One more line
  goes to stdout after the first: `→ https://<host> (app listens on 8787, ignoring PORT=47433)`.
- The wrapper records the found port as the host's usual port in `~/.hh/ports.json`.
  When the app listens on `PORT` again, the entry is dropped. The daemon never reads it.

Before the next start, if the usual port is busy, `hh` says who holds it on stderr and
still starts the app:

```
hh: quip.localhost usually listens on 3000, but 3000 is held by bun (pid 4242) running overdew.localhost from ~/moonshot/overdew
hh: free port for this run: 47433. Read $PORT in your dev script to use it.
```

"running <host> from <folder>" appears when the holder is in the process tree of a live
hh route. Otherwise the line shows the command, pid, and the holder's working folder.
`hh` prints the same two lines when the app exits non-zero within 15 s without a port.
With no usual port known, it prints a hint to run `hh who`.

After the child exits, `hh` sends SIGTERM to the processes it last saw in the child's
tree that still run the same command, waits up to 2 s, then sends SIGKILL. It never
signals other pids. The tree refreshes every 250 ms until the port is found, then every
second. When it stops something, it prints one line:
`hh: stopped 2 leftover processes (stripe, wrangler)`.

### Daemon

- Embedded Caddy, in process. The binary is the proxy.
- Listens on `:443` and `:80`. `:80` redirects to `https`.
- Control socket: `~/.hh/hh.sock`. State file: `~/.hh/routes.json`.
- `POST /routes` adds a route. The same pid posting the same host again moves it.
- Every route carries the wrapper's pid. A sweep every 5 s removes routes whose pid is gone.
  A crashed wrapper never leaves a stale route.
- Each route proxies to `127.0.0.1:<port>` with `X-Forwarded-*` headers and the
  original `Host`. WebSockets pass through.
- An unknown host returns a 503 page that lists the live routes.

### HTTPS

- Caddy's internal CA. Root lives in `~/.hh/pki`.
- Leaf certs are issued per host on first request, in memory, no files to manage.
- `hh setup` trusts the root. After that, every new host is green with no prompt.

### DNS

`.localhost` only. macOS and systemd-resolved Linux resolve `*.localhost` to loopback
with no setup. Nested names like `a.b.c.localhost` work. No DNS server, no resolver file.

## Platforms

| OS | DNS | Port 443 | Service |
|---|---|---|---|
| macOS | native | user can bind | launchd |
| Linux + systemd-resolved | native | `setcap` once | systemd user unit |
| Linux without systemd-resolved | hosts file per name, admin prompt | `setcap` once | systemd user unit |
| Windows | later: built-in DNS server | user can bind | later |

## Agents

`hh` is tied to no agent harness. It writes only to `AGENTS.md`, the open convention
that Claude Code, Codex, Cursor, Copilot, and Gemini CLI all read. It never writes to a
vendor folder such as `~/.claude`. Two layers, and each one works alone.

1. **Global rule.** `hh setup` offers to append one line to `~/AGENTS.md`:
   *"Start every dev server with the `hh` prefix. Use the URL it prints, never
   `localhost:<port>`."*
2. **Project.** `hh init` rewrites the `dev` script in `package.json` or `Procfile` to
   start with `hh`, and adds the same line to the project's `AGENTS.md`. Then
   `bun run dev` already does the right thing for every agent and every human, with no
   knowledge of `hh` at all. A `Makefile` gets a printed hint, not an edit.

`hh url --json` gives an agent the address before anything starts.

A harness-specific adapter, such as a Claude Code hook, is never part of `setup`. If one
is ever wanted, it ships as a separate optional command.

## Failure modes

| Case | Behavior |
|---|---|
| Daemon not running | Started on demand. |
| Root cert not trusted | URL still prints. One warning line: run `hh setup`. |
| Host collision from another folder | Exit 1, show the other folder, suggest `--name`. |
| Same folder, second `hh` run | Exit 1, show the running pid. |
| Allocated port busy | Next free port. Stable port is a preference, not a promise. |
| Child exits | Route removed within 5 s even on SIGKILL. |
| Command not found | Same error and exit code the shell gives. |
| App ignores `PORT` | Route moves to the port the app listens on. One extra `→` line names both ports. |
| Usual port busy | Stderr names the holder (hh host and folder, or command, pid, and folder), gives the free port, and the app still starts. |
| App exits early without a port | Same holder lines if the usual port is busy. Else a hint to run `hh who`. |
| Child leaves processes behind | SIGTERM, then SIGKILL after 2 s. One stderr line lists them. |
| `lsof` missing on macOS | Port discovery stops with an error line. The app keeps running. |

## Tech

- Go. Single static binary. Caddy imported as a library.
- Release with goreleaser: Homebrew tap, curl install script, GitHub releases.
- Tests: unit tests for naming and port hashing. Integration test that starts the daemon,
  runs `hh python -m http.server`, and fetches the URL with a trusted client.
- No config file format. Flags and env only.

## Acceptance

- [ ] Fresh Mac: `brew install hh && hh setup`, one sudo prompt, done.
- [ ] `hh bun run dev` in `quip` prints `→ https://quip.localhost` and the page loads green in Safari, Chrome, and `curl`.
- [ ] Same command in a worktree on branch `wt-173` prints `→ https://wt-173.overdew.localhost`. Both run at the same time.
- [ ] `git switch feat/auth` in the main checkout. `hh url` still prints `https://quip.localhost`.
- [ ] `acme.quip.localhost` reaches the `quip` app with no flag.
- [ ] `hh vite` works with HMR through the proxy.
- [ ] Kill the wrapper with `kill -9`. `hh ls` is clean within 5 s.
- [ ] `hh url --json` works with no daemon and no app running.
- [ ] Rails, Go, Node, Deno, Bun, Python examples from the README all work unchanged.
- [ ] Linux: same flow with one `setcap` prompt.

## Later

- Windows with a built-in DNS responder.
- `hh share` to expose a URL through a tunnel.
- TCP routes through Caddy's layer4 module.
