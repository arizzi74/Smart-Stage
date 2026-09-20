# HTTP API

Smart Stage runs two HTTP listeners in one process. Their roles are fixed and
cannot be changed by a pairing credential, cookie, Host header or forwarded
address header.

| Listener | Default address | Pages and role |
| --- | --- | --- |
| Local Admin | `127.0.0.1:8787` (`--admin-port`) | `/admin`; administration and playback |
| Remote control | `0.0.0.0:8788` (`--bind`, `--port`) | `/command`; playback and stage on/off; `/` redirects here |

Admin always binds IPv4 loopback. Its handler also requires an actual loopback
peer and exactly `127.0.0.1` with the configured Admin port in `Host`; adding LAN
hosts to a discovery list cannot broaden this check. The remote listener rejects
`/admin` and `/api/local-session`, even when reached from localhost.

API requests/responses use JSON except the QR PNG endpoint. Session creation,
pairing and mutation requests require `Content-Type: application/json` and the
exact page `Origin`. Session mutations also require `X-CSRF-Token`. Host must
match the address and port allowed by that listener. No CORS is enabled;
forwarded-IP headers are not trusted. Bodies are limited to 1 MiB; unknown fields,
trailing values, excessive strings/counts and invalid request IDs are rejected.
Cross-site/same-site fetch metadata is rejected on APIs. Public GET/HEAD
navigation to remote `/command` or `/` remains available, so an Admin link or a
phone camera can open the controller.

## Sessions and phone pairing

Local Admin uses `POST /api/local-session` with `{}`. This route exists only on
the loopback listener and needs an explicit matching Origin. It returns
`{"role":"admin","csrfToken":"...","expires":"RFC3339 timestamp"}` and sets
`smartstage_admin_session`. A valid existing Admin session is reused on reload;
no Admin key is entered or accepted through remote pairing.

Remote control uses `POST /api/pair` with `{"key":"12345678"}`. The value is an
exactly eight-digit, cryptographically random numeric code generated once per
process; leading zeroes are retained. Success returns
`{"role":"command","csrfToken":"...","expires":"RFC3339 timestamp"}` and sets
`smartstage_command_session`. This endpoint can only issue Command sessions.
Admin sessions copied under the Command cookie name are rejected, and vice versa.

Both cookies are HttpOnly, SameSite=Strict, Path=/, Max-Age=86400, without a Domain
attribute (Secure for a TLS request). Session and CSRF secrets have 192 random
bits. Cookies are scoped by host rather than port, so distinct names and an
explicit session-role check enforce the listener boundary. Sessions expire after
24 hours and are invalidated by process restart; the remote code is regenerated
at restart. Pairing admits at most ten attempts per actual peer IP per minute
and 100 attempts globally per minute, including correct submissions. Exhaustion
returns 429 with `Retry-After: 60`. At most 128 sessions and 1,024 peer buckets
are retained. Pairing limits do not apply to existing playback sessions or STOP.

Admin displays links such as
`http://192.168.1.20:8788/command#token=12345678`. The browser reads the fragment,
removes it from the address bar before requests, and submits the code in the
pairing JSON body. URL fragments are not sent in HTTP requests. The link grants
playback access to anyone who has it; trusted-LAN HTTP does not encrypt the
pairing exchange. Links, codes and QR images are disclosed only by authenticated
local Admin, and are not printed in startup logs or returned in Command state.

`POST /api/logout` invalidates the session and clears that listener's cookie.
`GET /api/state` returns `{role,csrfToken,state}` for session resumption.
Both roles can control playback and stage on/off; Command sessions cannot browse/configure or
receive source paths/raw native errors.

The current user request replaces the original specification's LAN Admin and
no-secrets-in-URLs design; see [the decision record](decisions.md#local-admin-and-camera-pairing-20-september-2026).

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
`generation`, `stopEpoch`, `cues`, `validationJob`, `updatePending`.

`updatePending` reserves the host while a startup update is checked or an update
is prepared. PLAY, show edits and enabling stage output are rejected during
this reservation; STOP remains available. A failed check or preparation releases
the reservation. The update cannot reserve playing/loading/stopping playback
or an enabled stage, including a black stage between cues.

Cue views contain `id,label,position,kind,duration,validation` and optional `color`
(`#RRGGBB`; omitted/empty uses the default button color). Validation jobs
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
| `POST /api/quit` | `{}` requests graceful host shutdown; HTTP 202 `{quitting:true}` is flushed before playback, stage and listeners close. Admin session, same-origin and CSRF protections apply. |
| `POST /api/admin-presence` | `{}` marks an authenticated Admin page as present for ten seconds; HTTP 200 `{present:true}`. An active Admin SSE also counts. Ordinary session/state probes do not suppress browser launch. |
| `POST /api/choose-files` | `{}` opens the supported native media chooser; HTTP 202 `{choosing:true}`. Selected original paths pass the same root/playlist validation as native file-open events. Admin session/state responses include `capabilities.chooseFiles`; unsupported hosts do not show the action. |
| `GET /api/update` | Update status: `currentVersion`, `latestVersion`, `phase`, `available`, `canInstall`, `message`, `releaseURL`, `checkedAt`, and optional `lastUpdate` outcome. |
| `POST /api/update/check` | `{}` starts an asynchronous check of official GitHub releases; HTTP 202 with status. Checks within one minute reuse the existing result. |
| `POST /api/update/install` | `{}` reserves stopped playback with stage output disabled and prepares the discovered update; HTTP 202 with status. The browser cannot supply a URL, version, executable or destination. |
| `GET /api/remote-control` | `{token:"eight digits",links:[{label:"interface",url:"http://…/command#token=…",qrURL:"/api/remote-control/qr?index=0"}]}`. Links track reachable listener addresses; an empty list is `[]`. |
| `GET /api/remote-control/qr?index=N` | PNG of the exact selected link, encoded locally with a four-module white quiet zone and `Cache-Control: no-store`. Requires the Admin session. Unknown/stale index returns 404; invalid query shape returns 400. |
| `GET /api/files?path=...&showHidden=false` | Canonical directory, parent, roots/volumes, breadcrumbs, at most 1,000 visible entries and truncation. Dot-prefixed names are hidden by default; `showHidden=true` includes them. Empty path chooses home or first permitted root. Explicit paths remain accessible within media roots. Entries include name/path/directory/bytes/modification nanoseconds. |
| `GET /api/playlist` | Full configuration model, source paths and derived validation cache. |
| `PUT /api/playlist` | `{expectedRevision:N,cues:[{id:"existing or empty",label:"...",path:"host file",color:"#RRGGBB"}]}`; returns saved configuration. Omitted color preserves an existing value; empty resets to default. Invalid colors are rejected. New IDs are server-generated; empty labels default only for new cues. Array order is cue order. |
| `POST /api/validate` | `{}` starts bounded background native validation; HTTP 202. |
| `POST /api/inspect` | `{path:"host file"}` returns native validation/metadata without rendering; used by Host files Inspect. Same root restrictions apply. |
| `GET /api/devices` | `{audio:[{id,name,default}],displays:[{id,name,x,y,width,height,primary,mirrored}]}`. |
| `PUT /api/outputs` | `{audioId:"default or endpoint",displayId:"ID or empty",allowPrimary:false}`; stopped/error only; saves and disarms stage. |
| `POST /api/stage-output` | `{enabled:true|false}`; enable requires available display and primary acknowledgement. Disable stops before hiding. HTTP 202. |

`POST /api/stage-output` is also available to authenticated Command sessions with
the usual exact Origin and CSRF checks. `{enabled:true}` requires stopped playback
and previously configured/acknowledged outputs; `{enabled:false}` stops playback
and closes the stage. Command cannot change output devices or any other Admin
configuration. Browser Escape invokes this same stage-off operation; native Escape
also stops and closes the stage, including when its generation has become stale.

Authenticated Command access to other Admin APIs returns 403. The remote listener
returns 404 for `/admin` and `/api/local-session`. Active/loading cue removal/source
replacement returns 409; label/order edits are allowed. Files remain in place.
Cache is not accepted as edit input. Schema 1 stores cue identities, labels,
canonical paths/order, output preferences and playlist revision. Validation
statuses: `unchecked`, `checking`, `ready`, `missing`, `unsupported`, `error`.
Raw reasons/media cache metadata are available only to administrators.

Updates are installed automatically during startup by default. Startup checks
time out after ten seconds; an unavailable network does not prevent subsequent
show use. Later checks run every six hours and never automatically restart the
current session. A discovered update can be installed on the next launch or
through the explicit Admin action. `phase` is one of
`idle|checking|available|downloading|restarting|error|unsupported`.
Development builds do not update themselves. Preview builds consider newer
previews and stable releases; stable builds consider stable releases only.
Restart invalidates sessions and remote codes; local Admin reconnects and shows
the new QR code. Command sessions cannot inspect or trigger update operations.

## Errors and bounds

Errors: `{"error":{"code":"...","message":"..."}}`.

| HTTP | Codes |
| --- | --- |
| 400 | `invalid_json`, `invalid_request`, `invalid_label`, `invalid_path`, `browse_failed`, `invalid_cue_id`, `duplicate_cue_id`, `too_many_cues`, `cue_invalid`, `output_unavailable`, `display_unavailable`, `primary_confirmation`, `invalid_index` |
| 401 | `unpaired`, `pair_failed` |
| 403 | `origin_denied`, `origin_required`, `csrf_denied`, `admin_required` |
| 404 | `cue_not_found`, `link_not_found`, `unknown_route` |
| 405 | `method`, `unknown_route` |
| 409 | `revision_conflict`, `request_conflict`, `stale_epoch`, `stale_instance`, `active_cue`, `must_stop`, `updating`, `update_unavailable` |
| 415 | `content_type` |
| 429 | `pair_failed` (guessing limit), `session_failed` (local session capacity) |
| 500 | `save_failed`, `internal_error`, `stream_unavailable`, `qr_failed` |
| 503 | `busy`, `overloaded`, `unavailable`, `updates_unavailable` |

Each listener admits at most 256 accepted TCP connections (including idle
connections) and 16 ordinary concurrent operations. At most 64 SSE streams are
shared across the application. Excess TCP connections wait in the OS backlog;
STOP bypasses the ordinary-operation and pairing limits. Remote links are
bounded to 64 entries and QR payloads to 1,024 bytes.

Maximum 500 cues, labels 512 UTF-8 bytes, paths 32,768 bytes, saved configuration
4 MiB, headers 16 KiB. HTML labels/paths are written as text, never injected
markup. There is no media streaming, arbitrary-path PLAY, upload or download API.

## Embedded third-party notices

`GET /licenses.txt` and `HEAD /licenses.txt` are available on both listeners
without pairing, subject to their normal Host/Origin checks. The text contains
the QR encoder's version, source URL and complete MIT copyright/license notice.
It is embedded in the executable, so single-file ZIP distributions need no
companion license file at runtime.
