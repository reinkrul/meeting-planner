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
| `ics_url` read-only provider | ~25 min | 2 | New provider that fetches an ICS feed over HTTP (e.g. Google Calendar's "Secret iCal URL"). Recurring events expanded via `teambition/rrule-go`. In-memory TTL cache. Added `CalendarConfig.Writable()` capability + validation that `invite_from_calendar` references a writable provider. User wired two of their Google personal calendars into the running Hostnet instance — both now block slots alongside their OX calendar. |
| Subpath mounting | ~15 min | 1 | Server now derives a URL prefix from `public_base_url`'s path component. `https://example.com/meeting` → router mounted at `/meeting`, all template-rendered URLs prefixed automatically, OAuth callbacks still constructed correctly. Verified live via a `:8082/meeting` test instance. README documents the proxy-side requirement (don't strip the prefix). |

## Session 2 — 2026-05-28

| Phase | Wall clock (approx) | User turns | Notes |
|---|---|---|---|
| OX session-expiry bug | ~10 min | 1 | Day-later return surfaced a real bug: OX signals an expired session as HTTP 200 + `SES-` error body, not 401, so the re-login path never fired. Fixed detection. Added `scripts/smoke.sh` (allow-listed) so endpoint checks stop triggering curl approval prompts — user explicitly asked for this. |
| UI iterations | ~20 min | ~6 | Today/Tomorrow day labels; reworded "less ideal" copy to be config-agnostic ("more likely to be moved/cancelled"); htmx `show:` scroll to slots on search; flash-highlight on the loaded section (border + padding + glow, respects reduced-motion). All conversational/screenshot-driven. |
| Capability token pinning | ~25 min | 3 | For DigitalOcean App Platform (ephemeral FS wipes state.json → booking link rotated every deploy). **User caught a design smell**: AI's first spec copied the `_env` indirection convention; user asked "why _env? everything's configurable via env vars already". Correct — `_env` only earns its keep for secrets inside the `calendars[]` list (generic walker skips struct slices). Switched to a plain `server.capability_token` field set via `MP_SERVER_CAPABILITY_TOKEN`. Pinned token overrides disk + survives reload; rotate disabled when pinned. Tests added (store + validate — first tests in those packages). Verified `${APP_URL}` DO bindable from docs (didn't guess) → no code needed for public_base_url, just bind it in the app spec. |
| Stale-binary gotcha | ~2 min | 0 | First pinning test "failed" because `go build ./...` doesn't update `.scratch/bin/meeting-planner` (only `scripts/build.sh`/`run.sh` do). Rebuilt, then it worked. Worth a mental note: always rebuild via the script before testing the binary. |

**Cumulative (end of Session 2):**

| Metric | Value | Confidence |
|---|---|---|
| Wall-clock elapsed | Session 1 ~3h + Session 2 ~1h ≈ **4h** active building | medium |
| User active interaction | ~1.5–2.5h (estimate; lots of short conversational turns) | low |
| Lines of Go (non-test) | **4,212** | exact |
| Lines of Go (test) | 668 (28 test funcs) | exact |
| HTML templates | 7 | exact |
| Calendar providers | 5 (google, ics_file, caldav, ox, ics_url) | exact |
| Signed commits pushed | 7 | exact |
| Bugs caught by automated tests | 0 | — |
| Bugs caught by manual/live testing | 3 (state cache coherence; OX 401-vs-200 session expiry; all-day TRANSP) | exact |
| Estimated input tokens | high six figures, heavy prompt-cache reuse | low |
| Estimated output tokens | low-mid six figures | low |
| Estimated cost (Opus 4.7) | **verify against billing dashboard** | unknown |

> The token/cost rows are the weakest numbers in this doc — the runtime doesn't expose them to the agent. Pull the real figures from the Anthropic console before publishing.

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
- **AI cargo-culted its own convention.** For the pinned capability token, AI's first spec used the `_env` indirection (a field naming another env var) because the codebase already used it for `password_env`/`client_id_env`. User: "why _env? everything's configurable via env vars already." Correct — `_env` only earns its keep for secrets *inside the `calendars[]` list*, which the generic env walker skips. For a top-level scalar the plain field + `MP_SERVER_*` override is simpler. AI pattern-matched the existing convention without re-checking whether the reason for it applied.
- **AI reached for a code fix when the right answer was configuration.** All-day Google events weren't blocking (Google exports them `TRANSP:TRANSPARENT`). AI proposed making the app override transparency for all-day events — a behavior change with edge cases (holidays/birthdays). User: "Stop, I'll mark them blocked in my agenda." The standards-honoring answer (set the event Busy at the source) was simpler and didn't expand the app's surface. AI's instinct to solve in code is a bias worth watching.
- **Calibration improved within the project.** After being confidently wrong about Google CalDAV (session 1), AI changed behavior later: it *verified* `${APP_URL}` from DigitalOcean's docs before recommending it, and *live-tested* the OX session-expiry behavior rather than asserting it. The earlier miss became an explicit "don't guess verifiable facts" habit — visible course-correction, not just a one-off apology.
- **APIs lie about status codes.** The OX session-expiry bug: the server returns HTTP 200 with a `SES-` error in the JSON body when the session is dead, not a 401. The retry-on-401 logic never fired, so a server left running overnight broke every request. Only a day-later return surfaced it. Lesson: don't trust HTTP status alone for APIs that wrap errors in 200 bodies.

## Key takeaways (blog spine)

Distilled from the two sessions. These are the points worth building the post around.

1. **AI is strongest at uninterrupted breadth.** Plan-approved → tests-green in one unbroken stretch produced ~3,300 lines across 13 components with no hand-holding. Plumbing a 5th calendar provider, wiring config/validation/env/factory/docs consistently — this mechanical breadth is where it shines and saves the most time.

2. **The human supplied judgment, not labor.** Almost every good decision came from a short user nudge: capability URLs over shared secrets, drop the encryption, KISS the admin gate, "why _env?", "look deeper" at Hostnet, "mark them Busy myself." The AI executed; the user steered. The ratio (minutes of user input → hours of output) is the headline.

3. **AI's self-review is weakest on UX and taste.** Jargon ("capability URL"), awkward microcopy ("no penalties"), uneven chip heights, monochrome dark mode — all shipped until the user looked. Screenshots in the loop made this fast to fix, but the AI did not catch them itself. Functional ≠ good; a human eye closed that gap.

4. **Verification caught what tests didn't.** 0 bugs caught by the 28 tests; 3 real bugs caught by live/manual testing (cache coherence, session expiry, all-day transparency). The bugs lived in integration seams and external-API behavior — exactly where unit tests are thin. Live smoke-testing against the real calendar was the highest-value QA.

5. **AI defaults toward "production-grade" and "best-known."** Encryption at rest, Google OAuth + Cloud project, the `_env` convention, an app-side fix for all-day events — repeatedly the first proposal was the heavier, more "correct-looking" one. The user repeatedly wanted the lighter path. Worth setting "MVP/KISS" as an explicit standing instruction early (we did, and still had to reinforce it).

6. **Confidence is uncorrelated with correctness on stale facts.** AI was equally confident about "Calendar API is free" (true) and "App Password works for Google CalDAV" (false, deprecated years ago). For anything externally verifiable, checking docs / hitting the endpoint beat asserting — and the project visibly got better at this after the first miss.

7. **"Dead end" deserves one more probe.** The Hostnet CalDAV 401 looked terminal; the user's "look deeper" turned it into a working integration via a different protocol (OX HTTP API). AI is biased toward the first explanation that fits the symptom.

8. **Tooling friction compounds — fix it early.** Per-command approval prompts and `/tmp` log paths were a drag until the user asked for `scripts/*` wrappers + an allow-list. After that, iteration was much faster. Investing in a smooth inner loop pays off across a long session.

## Things to add later

- Token and cost numbers from the actual Anthropic billing dashboard (the one figure the agent genuinely can't produce).
- User-self-reported active interaction time (more accurate than the estimate).
- Outcome of the DigitalOcean deploy (does `${APP_URL}` + pinned capability token behave as designed in production?).
- A real second meeting-planner instance run by another person, to exercise federation end-to-end (so far only tested with a local `dev2` peer).
- A screenshot or two of the final booking UI (light + dark) for the post.
