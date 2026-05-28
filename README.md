# meeting-planner

> 🧪 **AI-driven development experiment.** Every line of this project — design, code, tests, docs — was produced by working with an AI coding agent. The session log (decisions, dead ends, time, rough cost) is in [`EXPERIMENT.md`](EXPERIMENT.md). Source material for a planned blog post.

Self-hosted, single-binary meeting planner. Share a link, others pick a time, you get a real calendar invite — without exposing your event details and without depending on a SaaS product. Coordinates across multiple of your own calendars and across other people's instances.

- One static Go binary, distroless Docker image, no database.
- Calendar backends: **Google Calendar** (OAuth), **Open-Xchange App Suite** (Hostnet, mailbox.org, … — HTTP API, no app project), **CalDAV** (Apple, Fastmail, Nextcloud, Proton via Bridge, self-hosted), **ICS file** (dev/testing).
- Output of a booking = a real calendar event; invites are dispatched by your calendar provider.

---

## Quick start (5 minutes)

Pick the path that matches your calendar:

| Your calendar | Path | Why |
|---|---|---|
| **Local file (dev/test)** | [ICS file](#path-a-local-ics-no-account) | Try the whole flow without any account |
| **Google Calendar** | [Google OAuth](#path-b-google-calendar) | Real invites, requires a Google Cloud project (free) |
| **Hostnet / OX-based mail host** | [Open-Xchange](#path-c-open-xchange-hostnet-mailboxorg-) | No project, just email + password |
| **Apple / Fastmail / Nextcloud / Proton / self-hosted** | [CalDAV](#path-d-caldav-apple-fastmail-nextcloud-) | Standard CalDAV with an app password |

### Path A — Local ICS (no account)

```
go run ./cmd/meeting-planner -config configs/config.dev.example.yaml
```

Or with the scripts wrapper:

```
scripts/run.sh dev
```

The startup log prints the **booking link** — open it in a browser to book against `.scratch/calendars/owner.ics`. Pre-seed that file with a few `VEVENT`s to give the planner something to avoid.

### Path B — Google Calendar

You need a Google Cloud project (free) and an OAuth client:

1. **Google Cloud Console** → enable the **Google Calendar API**.
2. **OAuth consent screen**: External, add yourself as a Test user.
3. **Credentials → OAuth client ID** (type: Web application). Authorized redirect URI: `http://localhost:8080/admin/calendars/personal/callback` (adjust host/path for your deployment).
4. Copy the Client ID & Secret. Export them:

   ```
   export GOOGLE_CLIENT_ID="…"
   export GOOGLE_CLIENT_SECRET="…"
   ```
5. Copy `configs/config.google.example.yaml` to `configs/config.google.yaml`, edit owner + redirect host if needed.
6. `scripts/run.sh google`
7. Open `/admin/` (link printed at startup), click "Connect with Google", finish OAuth. `/admin` self-disables once every OAuth-requiring calendar is connected.

### Path C — Open-Xchange (Hostnet, mailbox.org, …)

If your provider uses App Suite (URL like `https://appsuite.YOUR-HOSTER.example/`), no project / OAuth needed:

1. Copy `configs/config.ox.example.yaml` → `configs/config.ox.yaml`. Fill in `server_url`, `username` (your email).
2. Put your password in `.scratch/env.sh`:

   ```
   export OX_PASSWORD="…"
   ```
3. `scripts/run.sh ox`

OX dispatches invite emails for events with `attendees` automatically.

### Path D — CalDAV (Apple, Fastmail, Nextcloud, …)

Standard CalDAV with HTTP Basic auth:

1. Generate an app-specific password from your provider.
2. Copy `configs/config.caldav.example.yaml` → `configs/config.caldav.yaml`. Fill in `server_url`, `username`.
3. Put the password in `.scratch/env.sh`:

   ```
   export CALDAV_PASSWORD="…"
   ```
4. `scripts/run.sh caldav`

**Not supported via CalDAV**: Google (requires OAuth — use Path B) and Hostnet (CalDAV is gated — use Path C).

---

## What a guest sees

Open your booking link → form with their name + email + meeting duration + optional extra attendees → time picker grouped by day → pick a slot → invite arrives in their inbox from your calendar provider.

A guest can list extra attendees who *also* run meeting-planner by pasting their booking link in the "Their booking link" field. Each side's instance fetches the other's free time over a private endpoint and the proposing instance intersects them. Email-only attendees are just invited; they don't contribute to availability.

---

## Multiple calendars

The `calendars: []` list takes any number of entries of any provider type. **One** of them is named in `invite_from_calendar:` — that's where new events get created and from which invites go out. **All** of them contribute their busy time to availability calculations.

So you can mix: a Google work calendar (booking + invites) plus an OX personal calendar (just blocks slots) plus an ICS-file private agenda (just blocks slots) plus a Google personal calendar pulled via its secret iCal URL (read-only — blocks slots without any OAuth setup). See `configs/config.ics-url.example.yaml` for the read-only-feed example.

```yaml
calendars:
  - id: "work"
    provider: "google"
    google: {...}
  - id: "personal"
    provider: "ox"
    ox: {...}
  - id: "private"
    provider: "ics_file"
    ics_file: {...}
  - id: "google-personal-readonly"
    provider: "ics_url"
    ics_url:
      url: "https://calendar.google.com/calendar/ical/.../private-.../basic.ics"

invite_from_calendar: "work"   # only this one gets written to
```

Read-only providers (`ics_url`) can't be used as `invite_from_calendar` — validation rejects it at startup.

---

## Notifications

Get a self-email when someone books a slot:

```yaml
notifications:
  smtp:
    enabled: true
    host: "smtp.YOUR-HOSTER.example"
    port: 587
    username: "you@yourdomain.example"
    password_env: "SMTP_PASSWORD"   # any env var name; can reuse OX_PASSWORD etc.
    from: "you@yourdomain.example"
    to: ["you@yourdomain.example"]
    start_tls: true
```

Send is fire-and-forget — if SMTP fails, the booking still completes; the failure is logged.

---

## Configuration

Three sources, each overrides the previous:

1. Built-in defaults (in code).
2. **YAML source**, one of:
   - `-config FILE` flag
   - `MP_CONFIG_FILE=/path` env var
   - `MP_CONFIG=<inline yaml>` env var (the whole config as a string — handy for Docker without volumes)
3. **Per-field env overrides** with the prefix `MP_<UPPER_YAML_PATH>`:

   | YAML | Env var |
   |---|---|
   | `server.listen` | `MP_SERVER_LISTEN` |
   | `server.public_base_url` | `MP_SERVER_PUBLIC_BASE_URL` |
   | `server.capability_token` | `MP_SERVER_CAPABILITY_TOKEN` |
   | `owner.display_name` | `MP_OWNER_DISPLAY_NAME` |
   | `owner.timezone` | `MP_OWNER_TIMEZONE` |
   | `availability.working_hours.start` | `MP_AVAILABILITY_WORKING_HOURS_START` |
   | `availability.working_days` (CSV) | `MP_AVAILABILITY_WORKING_DAYS` |
   | …all scalar fields follow the same pattern | … |

**Calendars list** can come from YAML *or* from indexed env vars (not both):

```
MP_CALENDARS_0_ID=work
MP_CALENDARS_0_PROVIDER=google
MP_CALENDARS_0_GOOGLE_CLIENT_ID_ENV=GOOGLE_CLIENT_ID_WORK
MP_CALENDARS_0_GOOGLE_CLIENT_SECRET_ENV=GOOGLE_CLIENT_SECRET_WORK
MP_CALENDARS_0_GOOGLE_CALENDAR_ID=primary
```

**Secrets**: any config field whose key ends in `_env` is interpreted as the *name of another env var* holding the actual secret. Secrets never appear in YAML or in `MP_CONFIG`.

---

## Operations

Helper scripts:

| Script | What it does |
|---|---|
| `scripts/build.sh` | Build the binary into `.scratch/bin/` |
| `scripts/run.sh <name>` | (Re)start an instance in the background. Looks for `configs/config.<name>.yaml`, falls back to `configs/config.<name>.example.yaml`. Sources `.scratch/env.sh` first. |
| `scripts/stop.sh` | Stop all running instances |
| `scripts/logs.sh [name]` | Tail the log for that instance (or the most recently modified one) |
| `scripts/test.sh` | `go test ./...` |

CLI subcommands (run on the binary directly):

| Command | What it does |
|---|---|
| `meeting-planner serve` (default) | Start the HTTP server |
| `meeting-planner reauth <calendar-id>` | Drop OAuth tokens so `/admin` re-enables to reconnect |
| `meeting-planner rotate-capability` | Mint a new booking link (invalidates the old URL) |
| `meeting-planner print-urls` | Print the current booking link |

The running server picks up changes to `state.json` via mtime, so the CLI commands take effect without a restart.

`/admin` is enabled **only** while at least one OAuth-requiring calendar lacks tokens. It returns 404 the rest of the time — there is no admin password (the setup window is bounded by you triggering it).

---

## Stateless / ephemeral-filesystem hosting

By default the capability token (your booking link) is randomly generated on first boot and persisted to `<data_dir>/state.json`. On hosts with no persistent disk (e.g. **DigitalOcean App Platform**, Heroku, most "deploy from git" PaaS), that file is wiped on every deploy/restart, so the link would rotate constantly.

Pin it instead:

```
# generate once
openssl rand -hex 32
# then set as a SECRET env var on the platform
MP_SERVER_CAPABILITY_TOKEN=<the 64-char hex string>
```

With it set, the booking link is stable across restarts and `rotate-capability` is disabled (rotate by changing the env value). Treat the token as a secret — it grants booking + `/freebusy` access. Inject it as an encrypted/secret env var; don't commit it to YAML.

This is the only stateful concern for OX/CalDAV/ICS setups. (Google OAuth deployments also need a persistent `data_dir` for refresh tokens, so they're not a fit for fully-ephemeral hosting unless you re-`/admin` after each deploy.)

### DigitalOcean App Platform

Set these as component environment variables (mark the token **encrypted/secret**):

| Variable | Value | Notes |
|---|---|---|
| `MP_SERVER_PUBLIC_BASE_URL` | `${APP_URL}` | DO bindable — resolves to your app's real URL at runtime |
| `MP_SERVER_CAPABILITY_TOKEN` | `<openssl rand -hex 32>` | pins the booking link across deploys |
| `OX_PASSWORD` (or provider secret) | your password | as required by your calendar provider |

`${APP_URL}` is a DigitalOcean bindable variable, substituted at deploy — you don't need to know the URL in advance. (Bindable vars resolve at runtime only for Dockerfile components, which is when we read config, so this works.)

## Deploying behind a reverse proxy (subpath)

If you want the app reachable at `https://example.com/meeting` rather than at its own domain, set `server.public_base_url` to include the path:

```yaml
server:
  listen: ":8080"
  public_base_url: "https://example.com/meeting"
```

The app derives the prefix from that URL and mounts all routes under it. Configure your reverse proxy to forward `/meeting/*` to the app **without** stripping the prefix.

If you use Google OAuth, the registered redirect URI in Google Cloud Console must include the subpath too — `https://example.com/meeting/admin/calendars/<id>/callback`.

## Docker

```
docker build -t meeting-planner .
docker run -p 8080:8080 \
  -v $PWD/config.yaml:/etc/meeting-planner/config.yaml:ro \
  -v meeting-planner-data:/data \
  -e GOOGLE_CLIENT_ID="…" \
  -e GOOGLE_CLIENT_SECRET="…" \
  meeting-planner:latest -config /etc/meeting-planner/config.yaml
```

Pure-env (no mounted config file): use `MP_CONFIG=<inline yaml>`, plus the indexed `MP_CALENDARS_<N>_*` env vars.

---

## Federation between instances

Two people each running meeting-planner can let a guest book a time that suits both. Each instance exposes a private read endpoint at `/c/<token>/freebusy` returning the owner's free windows + working hours + soft preferences. When a guest lists another instance's booking link as a participant, the proposing instance fetches the other's free time and intersects.

Knowing the URL is sufficient to query — there's no separate auth. Rotate the link (`meeting-planner rotate-capability`) to revoke. See [`docs/PROTOCOL.md`](docs/PROTOCOL.md) for the exact JSON shape if you want to interoperate.

---

## Privacy boundary

- Only **busy/free time windows** ever leave the calendar provider — not titles, attendees, descriptions, or locations.
- The federation endpoint returns the **inverse** (free blocks), not your busy times directly, so even the gaps between meetings within working hours aren't enumerated in the wire data.
- Guest name + email is stored in a 90-day `HttpOnly` cookie on the guest's browser for form pre-fill on return visits — never sent to anyone but you.

---

## License

[MIT](LICENSE).
