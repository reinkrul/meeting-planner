# AI-driven development experiment — meeting-planner

Project built from an empty directory with Claude Code (model: Opus 4.7, 1M context). Tracking is for a future blog post.

## How these numbers are sourced

- **Wall clock**: timestamps in my conversation (server start logs, file mtimes) plus rough boundaries between phases. Reasonably accurate to ±10 minutes.
- **User active interaction**: estimated from message count + length, since I can't see the user's actual reading/typing time. Rough.
- **Tokens**: **estimates only** — the runtime does not expose per-turn token counts to me from inside a session. Use the Anthropic console / billing dashboard for exact numbers.
- **Cost**: derived from token estimates × Opus 4.7 published pricing (≈$15/M input, $75/M output; prompt-cache hits much cheaper). Wide error bars.

If you have access to actual billing for this session, please overwrite the token/cost rows — they're the most uncertain numbers here.

---

## Session 1 — 2026-05-27

| Phase | Wall clock (approx) | User turns | Notes |
|---|---|---|---|
| Requirements gathering | ~09:50 – 10:10 (20 min) | 6 | Capability URLs picked over shared-secret federation after user pushback; "MVP, KISS, no encryption" set early. |
| Plan mode + revisions | ~10:10 – 10:35 (25 min) | 5 | 4 plan revisions inside plan mode (ICS provider added, peer preferences shared via federation, /admin auto-enable rule, env-var configurability). |
| Implementation | ~10:35 – 11:00 (25 min) | 0 (autonomous) | 13 tasks completed sequentially, ~3,300 lines of Go + templates. No user input between plan approval and tests-green. |
| Smoke test (ICS path) | ~11:00 – 11:08 (8 min) | 1 | Found and fixed bug: rotate-capability via CLI did not invalidate the running server's cached token. Fix: mtime-based reload in state store. |
| UI polish | ~11:10 – 11:15 (5 min) | 1 | User feedback: "no penalties" sounded weird; "capability" was jargon. Renamed to "Booking link", hid penalty annotation on clean slots. |
| Google OAuth setup | aborted | 4 | User hit Google Cloud's "verify payment info" wall during project creation. Pushed back: are there alternatives that don't require Google Cloud? Pivoted to CalDAV. |
| CalDAV provider | ~5 min | 1 | Added `caldav` provider using `github.com/emersion/go-webdav` + `go-ical`. ~150 lines of Go + config/validation/wiring + example config + README section. Builds + tests green. |
| CalDAV + Google: AI was wrong | ~5 min | 4 | AI confidently said "Google CalDAV works with App Password". Live test against the user's account returned **401 Unauthorized**. Reality: Google deprecated basic-auth CalDAV for personal accounts; the public CalDAV endpoint requires OAuth 2.0. App Passwords work for IMAP/SMTP, not CalDAV. CalDAV provider is still valid for Apple iCloud / Fastmail / Nextcloud / Proton (via Bridge) — just not for Google. |
| Scripts wrapper | ~3 min | 1 | User asked for `scripts/{build,run,stop,logs,test}.sh` wrappers and an allow-list entry so iteration stops triggering permission prompts. Saved as feedback memory for future sessions. |
| Hostnet CalDAV detour | ~10 min | ~5 | Pivoted to user's own Hostnet (Open-Xchange) calendar. Tested live: DAV root authenticates (207), but `/caldav/*` returns 401 specifically. Diagnosis (initial): user's Hostnet plan doesn't include the Calendar feature. |
| Hostnet deep dive | ~25 min | 1 ("look deeper") | Probed harder: principal namespace works, `current-user-principal` returns `/principals/users/3`, `calendar-home-set` resolves, principal lists email + display name. CalDAV is gated *despite* the user having full calendar metadata. Tried various URL shapes, user-agent sniffing (Apple/DAVx5/Thunderbird), session-cookie auth — all 401 on `/caldav/*`. Conclusion: Hostnet has CalDAV explicitly disabled in their OX config. But the **OX HTTP API (`chronos`) works fine** — session login succeeded, listed the user's "Agenda" folder (id 31), `freeBusy` returned correct busy blocks with full event details. |
| OX HTTP API provider | ~30 min | 0 (autonomous) | New `ox` provider (~270 lines): session-cookie auth with auto re-login on 401, FreeBusy via `chronos?action=freeBusy`, CreateEvent via `chronos?action=new`. Config schema + validation + factory + env-loader wiring. Live test against Hostnet: real busy blocks (incl. recurring standup) flow through. Privacy boundary held — chronos returns full event details in the freebusy response, but the provider drops everything except start/end. |
| End-to-end booking against real Hostnet calendar | ~3 min | 2 | Real booking via the public booking link → event landed on Hostnet calendar → OX dispatched the iMIP invite email to the external Gmail address (verified by user). Full loop closed against a real personal calendar without using Google Cloud at all. Minor cleanup remaining: my response-parser missed the event-id shape OX returns, so the UI showed an empty ID (event itself was correctly created). |
| UI polish — Calendly-style | ~25 min | ~10 | Refactored slot list to day-grouped collapsible cards. New design system (Inter typography, soft palette, cards, chips, gradient primary button, dark mode via `prefers-color-scheme`, radial gradient vignettes, accent halos on selected slot). Several rounds of small fixes: equal-height chips (CSS grid `align-self: stretch` + `grid-auto-rows`), generic `label` margin polluting first-chip height, "less ideal" copy made progressively subtler, monochrome → multi-hue (added violet pair to blue accent for gradients). Major insight: visual feedback loop with the user is fast and high-bandwidth — each tweak takes seconds; alignment on "looks right" is conversational. |
| Self-email notifications | ~15 min | 1 | SMTP notifier (`internal/notify/smtp.go`) with STARTTLS, fire-and-forget on successful Confirm. Provider-agnostic — same code works whether calendar backend is Google, OX, CalDAV, or ICS. |
| Free-blocks freebusy protocol | ~20 min | 1 | Switched federation wire format from `busy[]` to `free[]`. Cleaner semantics + mildly more privacy-friendly (gaps between meetings within working hours no longer enumerated). Receiver derives `busy = working_hours - free` for per-party scoring rules; no info loss. Added `availability.DeriveBusyFromFree`. Updated test + README. |
| Booker cookie pre-fill | ~5 min | 1 | 90-day `HttpOnly` cookies (`mp_user_name`, `mp_user_email`) set on POST /slots; read on GET booking form; URL-encoded for cookie safety. |
| Repository hygiene + GitHub publish | ~15 min | 4 | Renamed all committed configs to `*.example.yaml` pattern; `.gitignore` excludes `configs/*.yaml` except `*.example.yaml` so personal configs stay local. `scripts/run.sh` falls back from `configs/config.<name>.yaml` to the example automatically. Misleading CalDAV-against-Google example replaced with a generic CalDAV example (Apple/Fastmail/Nextcloud/Proton). README rewritten as a focused how-to. Protocol details moved to `docs/PROTOCOL.md`. MIT LICENSE. Initial signed commit pushed to https://github.com/reinkrul/meeting-planner. Repo description + topics set via `gh`. |

**Cumulative so far:**

| Metric | Value | Confidence |
|---|---|---|
| Wall-clock elapsed | ~1h 30m | high |
| User active interaction | ~25–40 min | low (estimate) |
| User turns | ~16 | high |
| Lines of Go produced | 3,362 (15 files) + 7 templates | exact |
| Tests written / passing | 13 / 13 | exact |
| Bugs caught by tests | 0 | (none of the bugs that surfaced were table-test-shaped) |
| Bugs caught by manual smoke | 1 (state store cache coherence) | exact |
| Estimated input tokens | ~300–500k (heavy prompt-cache hits) | low |
| Estimated output tokens | ~80–120k | low |
| Estimated cost (Opus 4.7) | ~$5–$15 USD | low — verify against billing |

## Blog-worthy moments

- **Plan mode was used**, with the plan revised 4 times based on user pushback before any code was written. Notable user-driven course corrections:
  - Shared-secret federation → capability URLs (user said "URL itself the secret for the MVP")
  - Centralized rules → peer parties contribute their own preferences via federation JSON
  - Admin password → auto-enable when OAuth needed (user proposed "MVP? If not configured, enable /admin?")
- **AI proposed encryption upfront**; user pushed back ("Why key management and encryption?"). AI re-evaluated and agreed it was overkill for single-user self-hosted. Captured: AI defaults can be conservative; user lens matters.
- **Implementation was uninterrupted**: from "plan approved" to "all tests pass + smoke test green" took one continuous turn block with no user input.
- **The bug AI's own tests missed**: state store cache coherence between CLI and running server. Found at the very last step of the smoke checklist (rotate-capability). Required redesign of the state store reader (mtime-based reload), not just a tweak.
- **UX language slipped through**: "Capability URL" and "(no penalties)" both shipped in the MVP UI before user noticed. AI did not catch them in self-review. Both are minor but emblematic of the gap between "functional" and "good".
- **AI defaulted to the "best-known" Google integration (OAuth + Cloud project) and didn't surface the alternatives** until user asked. CalDAV is in fact a strictly simpler path for personal self-hosting — no project, no client credentials, no consent screen, no card. Symmetric blind spot to the encryption-by-default one: AI reaches for the "production" answer when the user wants the lightweight one.
- **…but then AI was confidently wrong about whether CalDAV works against Google**: said yes with App Password, reality is Google requires OAuth for CalDAV too (deprecated basic auth for personal accounts years ago). Only caught by actually hitting the endpoint and getting 401. The CalDAV code itself is fine — works against Apple/Fastmail/Nextcloud/Proton — but for Google specifically the entire detour was wasted. Worth noting: AI sounded equally confident about both the (true) "Calendar API is free" claim and the (false) "App Password works for CalDAV" claim. Calibration is a real issue when verifiable claims sit next to slightly-stale ones.
- **Hostnet investigation paid off after the user pushed back.** AI's first read on the Hostnet 401 was "your plan doesn't include CalDAV — dead end". User said "look deeper". Deeper probing (principal namespace, session cookies, OX HTTP API discovery) revealed that the OX *web API* worked fine even though CalDAV was gated, leading to a working integration via a different protocol entirely. The takeaway: when AI declares something a dead end, it's worth probing once more before accepting it — AI is biased toward the first explanation that fits the symptom.
- **UX work is fast and conversational with screenshots in the loop.** The Calendly-style redesign happened over ~10 quick turns: ship → user screenshots → tweak → repeat. The bottleneck was AI noticing the right things to fix, not implementation time. Specific misses AI made and user caught: chip heights different between rows; first chip in each day off because of a generic `label` margin rule; "less ideal" tag visually competing with the time itself; UI feeling too monochrome in dark mode. None were complex fixes; all required the user to flag them. AI's self-review of its own UI output is weak.
- **Repo readiness is its own task.** Going from "works on my machine" to "publishable on GitHub" took ~15 min by itself even after the code was done: renaming personal-bearing configs to `.example.yaml`, fixing a misleading example, scrubbing the user's name from non-license/non-experiment files, restructuring README into a how-to, splitting protocol details into a separate doc, MIT license, gitignore audit, gh-CLI push, description + topics. None individually hard, all easy to forget without a checklist.

## Things to add later

- Token and cost numbers from the actual Anthropic billing dashboard.
- User-self-reported active interaction time (more accurate than my estimate).
- Outcome of Google OAuth path (Path B from the plan) — whether it works first try.
- Whether the federation/preferences design holds up once real calendars are connected.
