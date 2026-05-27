# Federation protocol

The endpoint two meeting-planner instances use to coordinate availability for a multi-party meeting. **Bespoke JSON, not an interop standard** — RFC 5545 VFREEBUSY can't carry working-hours or per-party soft preferences.

## Endpoint

```
GET /c/<token>/freebusy?from=<RFC3339>&to=<RFC3339>
```

- `<token>` is the instance's capability token (the same one in the public booking link).
- Knowing the URL = authorized. There is no separate header auth.
- Rotate via `meeting-planner rotate-capability` to revoke.

## Response

`Content-Type: application/json`

```json
{
  "timezone": "Europe/Amsterdam",
  "working_hours": {
    "start": "09:00",
    "end":   "17:00",
    "days":  ["mon", "tue", "wed", "thu", "fri"]
  },
  "free": [
    { "start": "2026-05-28T07:00:00Z", "end": "2026-05-28T10:00:00Z" },
    { "start": "2026-05-28T11:00:00Z", "end": "2026-05-28T15:00:00Z" }
  ],
  "preferences": {
    "avoid_lunch":               { "enabled": true,  "start": "12:00", "end": "13:00", "penalty": 50 },
    "avoid_back_to_back":        { "enabled": true,  "gap_minutes": 15, "penalty": 30 },
    "avoid_long_busy_stretches": { "enabled": true,  "max_stretch_minutes": 240, "penalty_per_30min_over": 20 },
    "prefer_mornings":           { "enabled": false, "penalty": 10 }
  },
  "buffer_minutes": 0
}
```

### Field semantics

- `timezone` — IANA TZ database name. Used to interpret `working_hours` (HH:MM in local time).
- `working_hours.start` / `working_hours.end` — half-open window `[start, end)` in `timezone`-local minutes-of-day.
- `working_hours.days` — three-letter lowercase weekday names: `mon`..`sun`.
- `free` — RFC 3339 timestamps. Already clamped to working hours, with the peer's own `buffer_minutes` baked in. Mutually-disjoint; chronological is recommended but not required.
- `preferences` — optional. Each soft rule shape:
  - `avoid_lunch`: `{enabled, start: "HH:MM", end: "HH:MM", penalty: int}` — overlap with this window adds penalty proportional to overlap.
  - `avoid_back_to_back`: `{enabled, gap_minutes: int, penalty: int}` — penalty per side if a busy block is within `gap_minutes` of the candidate.
  - `avoid_long_busy_stretches`: `{enabled, max_stretch_minutes: int, penalty_per_30min_over: int}` — penalty if adding this slot extends a continuous busy stretch past the cap.
  - `prefer_mornings`: `{enabled, penalty: int}` — penalty for candidates starting at or after noon local.
  - Any rule may be omitted; missing = disabled.
- `buffer_minutes` — informational. The peer's buffer is already applied to `free`; the requester does not re-apply it.

### Why free, not busy?

Sending free blocks is mildly more privacy-friendly (gaps between meetings within working hours aren't enumerated) and simpler for the consumer: "here's when I'm available" requires no further computation to use.

The proposing instance derives `busy = working_hours - free` internally when it needs busy boundaries for the `avoid_back_to_back` / `avoid_long_busy_stretches` rules. No information is lost (peer's buffer is included in the derived busy because it was already in the free blocks).

## Errors

- `404 Not Found` — capability token mismatch.
- `400 Bad Request` — missing or unparseable `from`/`to`.
- `500 Internal Server Error` — upstream calendar provider failure (peer's calendar backend was unreachable).

## Implementing a non-meeting-planner consumer

A consumer that only needs the peer's free time can ignore everything except `free` and intersect with its own free windows. The `preferences` block is purely advisory.

If interop with iCalendar tooling is the goal, generating a VFREEBUSY component from `free` is straightforward: each free gap is a `FREEBUSY;FBTYPE=FREE:` line.
