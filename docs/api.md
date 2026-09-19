# HTTP API

Requests/responses use JSON. Pair and mutation requests require
`Content-Type: application/json` and the exact page `Origin`. Session mutations
also require `X-CSRF-Token`. Host must name a configured local address and port.
No CORS is enabled. Bodies are limited to 1 MiB; unknown fields, trailing values,
excessive strings/counts and invalid request IDs are rejected.

## Pairing

`POST /api/pair`: `{"key":"launch key"}` returns
`{"role":"admin|command","csrfToken":"...","expires":"RFC3339 timestamp"}`
and sets `smartstage_session`: HttpOnly, SameSite=Strict, Path=/, Max-Age=86400
(Secure for a TLS request). Sessions/keys rotate on process restart. Ten attempts
per source IP per minute; at most 128 sessions and 1,024 limiter buckets. The
server does not trust forwarded-IP headers. Never place keys in URLs.

`POST /api/logout` invalidates the session and clears the cookie.
`GET /api/state` returns `{role,csrfToken,state}` for session resumption.
Both roles can control playback; command sessions cannot browse/configure or
receive source paths/raw native errors.

## Playback

```json
POST /api/play
{"requestId":"unique-client-id","instanceId":"current-instance","stopEpoch":1,"cueId":"saved-id"}

POST /api/stop
{"requestId":"another-unique-client-id"}
```

IDs contain 8–128 ASCII letters/digits/underscores/hyphens. Intentional presses
use new IDs; automatic PLAY retry with a new ID is forbidden. The last 4,096
accepted IDs are idempotent; a different payload conflicts. STOP has no
revision/epoch precondition and bypasses ordinary-operation concurrency limits.

HTTP 202 acknowledges acceptance:

```json
{"accepted":true,"duplicate":false,"instanceId":"...","revision":5,"generation":3,"stopEpoch":2}
```

Observe state for actual native transitions. A stale instance/epoch, unknown or
invalid cue is rejected without replacing playback. An accepted native failure
stops/blackens and remains visible in state.

## State/events

State contains `instanceId`, `revision`, `playlistRevision`, `state`
(`stopped|loading|playing|stopping|error`), `activeCueId`, `activePosition`
(one-based, zero if inactive), `elapsed`, `duration` (seconds; zero unknown),
`lastError`, `outputs`, `resolvedAudioId`, `stageEnabled`, `outputFault`,
`generation`, `stopEpoch`, `cues`, `validationJob`.

Cue views contain `id,label,position,kind,duration,validation`. Validation jobs
contain `running,completed,total`. Output preferences contain
`audioId,displayId,allowPrimary`.

`GET /api/events`: authenticated SSE, initial/subsequent `event: state` messages
with full role-appropriate state and IDs `instanceId:revision`; ten-second
`event: heartbeat` messages contain `{}`. Reconnect always gets a fresh snapshot,
not replayed history. Fetch `/api/state` after revision gaps. At most 64 streams;
expiry/logout ends them. Commands do not travel over SSE.

## Admin routes

| Route | Contract |
| --- | --- |
| `GET /api/files?path=...` | Canonical directory, parent, roots/volumes, breadcrumbs, at most 1,000 entries and truncation. Empty path chooses home or first permitted root. Entries include name/path/directory/bytes/modification nanoseconds. |
| `GET /api/playlist` | Full configuration model, source paths and derived validation cache. |
| `PUT /api/playlist` | `{expectedRevision:N,cues:[{id:"existing or empty",label:"...",path:"host file"}]}`; returns saved configuration. New IDs are server-generated; empty labels default only for new cues. Array order is cue order. |
| `POST /api/validate` | `{}` starts bounded background native validation; HTTP 202. |
| `POST /api/inspect` | `{path:"host file"}` returns native validation/metadata without rendering; used by Host files Inspect. Same root restrictions apply. |
| `GET /api/devices` | `{audio:[{id,name,default}],displays:[{id,name,x,y,width,height,primary,mirrored}]}`. |
| `PUT /api/outputs` | `{audioId:"default or endpoint",displayId:"ID or empty",allowPrimary:false}`; stopped/error only; saves and disarms stage. |
| `POST /api/stage-output` | `{enabled:true|false}`; enable requires available display and primary acknowledgement. Disable stops before hiding. HTTP 202. |

Command access to admin routes returns 403. Active/loading cue removal/source
replacement returns 409; label/order edits are allowed. Files remain in place.
Cache is not accepted as edit input. Schema 1 stores cue identities, labels,
canonical paths/order, output preferences and playlist revision. Validation
statuses: `unchecked`, `checking`, `ready`, `missing`, `unsupported`, `error`.
Raw reasons/media cache metadata are available only to administrators.

## Errors and bounds

Errors: `{"error":{"code":"...","message":"..."}}`.

| HTTP | Codes |
| --- | --- |
| 400 | `invalid_json`, `invalid_request`, `invalid_label`, `invalid_path`, `browse_failed`, `invalid_cue_id`, `duplicate_cue_id`, `too_many_cues`, `cue_invalid`, `output_unavailable`, `display_unavailable`, `primary_confirmation` |
| 401 | `unpaired` |
| 403 | `origin_denied`, `origin_required`, `csrf_denied`, `admin_required` |
| 404 | `cue_not_found` |
| 405 | `method`, `unknown_route` |
| 409 | `revision_conflict`, `request_conflict`, `stale_epoch`, `stale_instance`, `active_cue`, `must_stop` |
| 415 | `content_type` |
| 429 | `pair_failed` (invalid key or rate/session limit) |
| 500 | `save_failed`, `internal_error`, `stream_unavailable` |
| 503 | `busy`, `overloaded`, `unavailable` |

Maximum 500 cues, labels 512 UTF-8 bytes, paths 32,768 bytes, saved configuration
4 MiB, headers 16 KiB. HTML labels/paths are written as text, never injected
markup. There is no media streaming, arbitrary-path PLAY, upload or download API.
