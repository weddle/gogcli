# gogcli spec

## Goal

Build a single, clean, modern Go CLI that talks to:

- Gmail API
- Google Calendar API
- Google Chat API
- Google Classroom API
- Google Drive API
- Google Drive Labels API
- Google Docs API
- Google Sheets API
- Google Forms API
- Google Maps Places API
- Google Photos Library API
- Google Photos Picker API
- Apps Script API
- Google Tasks API
- Cloud Identity API (Groups)
- Google People API (Contacts + directory)
- Google Keep API (Workspace-only, service account)

This replaces the existing separate CLIs (`gmcli`, `gccli`, `gdcli`) and the Python contacts server conceptually, but:

- no backwards compatibility
- no migration tooling

## Non-goals

- Preserving legacy command names/flags/output formats
- Importing existing `~/.gmcli`, `~/.gccli`, `~/.gdcli` state
- Exposing the whole CLI through a generic MCP command-execution bridge

## MCP server

`gog mcp` runs a typed MCP server over stdio for agent clients that need a
permissioned Google Workspace tool surface. It intentionally does not expose a
generic shell/argv bridge. Each MCP tool has a fixed schema and maps to a
specific `gog` operation.

MCP defaults are read-only. Write and Destructive tools require ordinary write
authorization: runtime `--allow-write` or a persistent policy with
`allow_write: true`. `--allow-tool` can further narrow the registered tool set
by tool name or service prefix. Destructive tools have an additional selection
gate: `--allow-tool` must contain the literal `destructive` selector or the
exact destructive tool name. Parent root context such as `--account`, `--home`,
output mode, `--no-input`, untrusted wrapping, and command safety flags is
preserved for subprocess calls.

Selector matching is evaluated against the registry at server startup. Exact
tool names are the stable least-privilege choice; service selectors
(`gmail`, `gmail.*`, and their peers), the `read`/`write` risk selectors, and
`*`/`all` are intentionally future-expanding for ordinary Read/Write tools.
After an upgrade they include newly registered ordinary tools in the selected
class, subject to the same read-only and `--allow-write` checks. The literal
`destructive` selector is the only risk-wide destructive opt-in; existing
ordinary `write`, service, service-wildcard, `*`, and `all` policies do not
acquire registered destructive tools.

### Policy migration compatibility matrix

This is the binary compatibility contract for the legacy runtime and persistent
MCP policies. `Read`, `Write`, and `Destructive` are `tools/list` visibility
results for representative tools; destructive rows now cover the registered
destructive domain tools, including `gmail_delete_draft`,
`gmail_trash_messages`, `calendar_delete_event`, `drive_trash`,
`drive_share_user`, and `drive_unshare`.

| Context | Selector | Gate/mode | Read | Write | Destructive |
| --- | --- | --- | ---: | ---: | ---: |
| Legacy runtime | omitted | no write authorization | yes | — | — |
| Legacy runtime | omitted | write authorization | yes | yes | — |
| Legacy runtime | exact Read | no write authorization | yes | — | — |
| Legacy runtime | exact ordinary Write | write authorization | — | yes | — |
| Legacy runtime | service | write authorization | yes | yes | — |
| Legacy runtime | service wildcard | write authorization | yes | yes | — |
| Legacy runtime | `read` | write authorization | yes | — | — |
| Legacy runtime | `write` | write authorization | — | yes | — |
| Legacy runtime | `all` or `*` | write authorization | yes | yes | — |
| Legacy runtime | `destructive` | write authorization | — | — | yes |
| Legacy runtime | exact Destructive | write authorization | — | — | yes |
| Legacy runtime | unknown | write authorization | — | — | — |
| Legacy runtime | empty runtime values | write authorization | yes | yes | — |
| Global policy | omitted `allow_tools` | default | yes | — | — |
| Global policy | service wildcard | `allow_write: true` | yes | yes | — |
| Global policy | `destructive` | `allow_write: true` | — | — | yes |
| Per-account replacement | account selects `read` | selected account | yes | — | — |
| Per-account replacement | account selects `destructive` | `allow_write: true` | — | — | yes |
| Readonly runtime | global `all` | `--readonly` | yes | — | — |
| Runtime narrowing | global `all` + exact Read | runtime exact selector | yes | — | — |
| Runtime narrowing | global `destructive` | runtime `destructive` | — | — | yes |
| Runtime narrowing | global `all` + unknown | unknown runtime selector | — | — | — |
| Invalid persistent policy | empty list | startup validation | error | error | error |
| Invalid persistent policy | unknown selector | startup validation | error | error | error |
| Invalid persistent policy | write authorization without list | startup validation | error | error | error |

The upgrade rule is asymmetric by design. Existing broad `write`, service,
service-wildcard, `*`, and `all` selectors widen to include newly registered
ordinary Write tools, but never acquire a newly registered Destructive tool.
Destructive exposure always requires ordinary write authorization plus the
literal `destructive` selector or the exact destructive tool name. Runtime
selectors intersect the persistent ceiling; runtime write authorization cannot
widen it, and readonly suppresses both mutation classes. An empty runtime list
means no runtime narrowing, whereas an explicitly empty persistent selector
list is invalid.


The typed surface is grouped by service. Gmail reads are
`gmail_search`, `gmail_get_message`, `gmail_get_thread`, `gmail_list_labels`,
`gmail_list_drafts`, and `gmail_get_draft`; ordinary Gmail writes are
`gmail_create_draft`, `gmail_update_draft`, `gmail_modify_message_labels`,
`gmail_modify_thread_labels`, `gmail_archive_messages`,
`gmail_archive_threads`, `gmail_mark_messages_read`, and
`gmail_mark_messages_unread`; Gmail destructive tools are
`gmail_delete_draft` and `gmail_trash_messages`. Calendar reads are
`calendar_events`, `calendar_list_calendars`, `calendar_search_events`,
`calendar_get_event`, `calendar_freebusy`, and `calendar_find_conflicts`;
Calendar writes are `calendar_create_event`, `calendar_update_event`,
`calendar_respond_to_event`, `calendar_move_event`, `calendar_create_calendar`,
`calendar_subscribe`, `calendar_unsubscribe`, `calendar_focus_time`,
`calendar_out_of_office`, and `calendar_working_location`; Calendar
destructive tools are `calendar_delete_event` only for explicit event or
recurrence-range deletion. Drive reads are `drive_search`, `drive_get`,
`drive_download`, `drive_list_folder`, and `drive_permissions`; Drive writes
are `drive_create_folder`, `drive_rename`, `drive_move`, `drive_copy`,
`drive_create_shortcut`, and `drive_create_comment`; Drive destructive tools
are `drive_trash`, `drive_share_user`, and `drive_unshare`. Docs, Sheets, and
Slides provide `docs_get`, `docs_create`, `docs_write`, `sheets_read_range`,
`sheets_create`, `sheets_update_range`, and `slides_create_from_template`.
The `sheets_read_range` schema requires `spreadsheet_id` and `range`; its
optional `dimension` enum (`ROWS` or `COLUMNS`) maps to the CLI's
`--dimension` flag. Omitting `dimension` preserves the existing CLI/API
default, and range positionals remain after the CLI `--` delimiter.
All arrays are bounded and all schemas are closed; attachment downloads,
filesystem paths, generic argv, and Gmail send operations are not part of this
surface. Permanent Gmail message deletion, Drive upload/permanent deletion,
whole-calendar deletion, and unscoped Drive sharing remain excluded.

### Gmail drafts-only boundary (E01)

The MCP Gmail write surface is drafts-only for outbound mail:
`gmail_create_draft` and `gmail_update_draft` create or rebuild drafts and
never send. `gmail_delete_draft` is a separate Destructive tool: it permanently
deletes one explicitly identified draft, does not move it to Trash, and offers
no recovery path. `gmail_trash_messages` is also a separate Destructive tool:
it moves only explicit message IDs to recoverable Trash. Both require ordinary
write authorization plus an explicit `destructive` or exact-tool selector.
Ordinary message/thread label tools reject adding `TRASH`, so they cannot
bypass this Destructive gate; thread trash remains structurally unavailable.
Gmail send, draft-send (`post`), reply or reply-all (`replyall`), forward (`fwd`),
autoreply, and permanent message deletion (`gmail batch delete`) remain absent
under exact, `gmail`, `gmail.*`, risk, `*`, and `all` selectors.

The final Wave D registry contains **54 typed tools: 19 Read, 29 ordinary
Write, and 6 Destructive**. `M01`–`M13`, `G01`–`G11`, `C01`–`C15`, and
`V01`–`V11` cover the earlier typed adapters; `X01`–`X06` are the six
explicitly authorized destructive tools. `R01`–`R03` supply risk annotations
and persistent/runtime policy ceilings. B01–B04 supply bounded binary
transport and the Drive download Read tool. Runtime readonly suppresses every
ordinary write and destructive tool.

Sheets literal `values_json` input is decoded once with
`DecodeStrictForRange`. It must be a strict literal JSON 2D array, with JSON
numbers preserved. A fully concrete A1 range with both row and column
endpoints (for example, `A1:B2`) rejects a matrix that exceeds its row or
column bounds before the child command. Exact-fit and smaller matrices remain
valid. Named ranges and open-ended or partially bounded ranges such as `A:B`,
`1:2`, or `A1:B` retain strict JSON validation but skip the offline dimension
check because Sheets resolves their size. `@file`, `@-`, and `-` expansion
forms are rejected.

### Gmail query-mutation lockout (E02)

No registered Gmail mutation accepts `query` or `max`; all mutation schemas are
closed, and their generated argv is limited to typed operation fields and
explicit resource IDs rather than a query expansion. Callers must use the read
path first:
`gmail_search` → inspect message/thread IDs with the returned summaries or a
`gmail_get_message`/`gmail_get_thread` read → call an explicit-ID label,
archive, read-state, or `gmail_trash_messages` mutation. Archive, read-state,
and trash tools accept only bounded explicit-ID arrays.

Draft changes follow the same boundary without a message search: locate and
inspect a draft with `gmail_list_drafts`/`gmail_get_draft`, then call
`gmail_update_draft` with `draft_id`. It rebuilds the full MIME message from
inline `body` or `body_html`; omitted `to` is preserved, omitted `cc`/`bcc` are
cleared unless reply-all derives them, existing attachments and reply lineage
are preserved, and it never sends. `gmail_create_draft` creates a new inline
draft; neither draft tool accepts query expansion or a max selector.

`gmail_trash_messages` accepts 1–1,000 alphanumeric message IDs and never
accepts query, max, thread, permanent-delete, force, or generic argv fields.
It adds `TRASH` and removes `INBOX` through one aggregate `BatchModify`
request. Success exposes aggregate action/count and labels only; provider
failure is a non-zero child/MCP error envelope with no per-item success/error
records. Provider effects are indeterminate on failure because no per-item
evidence is returned. Messages stay recoverable during Gmail's retention
window.

### Drive trash (X04)

`drive_trash` is a Destructive tool that requires explicit `file_id` and
invokes only the default `gog drive delete --force -- FILE_ID` trash path.
The `--force` confirmation bypass is server-controlled; it is not an MCP
input. The closed schema exposes no `permanent`, path, upload, stdin, or
generic argv controls, so permanent deletion is not reachable through this
tool.

### Drive exclusions (E03)

Drive `drive_trash`, `drive_share_user`, and `drive_unshare` are the three
narrow Drive Destructive tools. They require ordinary write authorization plus
the literal `destructive` or exact-tool selector; service, wildcard, `write`,
`*`, and `all` selectors do not expose them.
Drive upload and permanent delete remain absent under every exact, service,
risk, wildcard, and `all` selector. No Drive schema accepts `--permanent`, a
host filesystem path, `out`, `overwrite`, raw stdout, stdin, `@file`, or
generic argv. The bounded `drive_download` Read tool is the B04 exception for
download bytes: its only model inputs are explicit `file_id` and optional
supported export `format`; its child uses server-fixed `--max-bytes 65536
--out -` solely for in-memory capture.

### Drive sharing grant (X05)

`drive_share_user` is a Destructive user-only grant. Its closed schema requires
`file_id` and a plain email; optional `role` is `reader`, `commenter`, or
`writer` (reader default). The target is always `type=user`, notification is
always disabled, and discoverability is false. Public, domain, owner, notify,
force, path, and generic action/argv inputs are absent. The adapter reuses the
CLI email and role validators and emits
`drive share --to user --email EMAIL --role ROLE -- FILE_ID`.

Permission creation and web-link lookup are a non-atomic two-step operation.
If creation succeeds but lookup fails, the command returns the original
provider error and the MCP result remains failed (`exit_code` non-zero and
`IsError` true); it never silently reports success. In JSON/MCP output,
structured permission evidence still includes the created `permissionId` and
`permission` object. This is a residual grant: reverse it with
`drive_unshare(file_id, permission_id)` using that exact ID, then re-list with
`drive_permissions` to verify cleanup. No compensation delete is attempted.

### Drive permission removal (X06)

`drive_unshare` removes exactly one permission. Its closed schema requires only
`file_id` and `permission_id`; its fixed child argv is
`drive unshare --force -- FILE_ID PERMISSION_ID`, with `--force` supplied by the
server. Callers must first use `drive_permissions` to inspect the exact
permission ID. The tool does not enumerate, re-target, trash, or permanently
delete files, and parent dry-run exits before auth/service construction.

### Calendar event deletion (X01/E04)

`calendar_delete_event` is the only Calendar deletion tool. It requires
`calendar_id`, `event_id`, and an explicit `scope` enum (`single`, `future`, or
`all`); `original_start` is required for `single`/`future` and rejected for
`all`. `send_updates` defaults to `none`, and the server appends `--force`.
Recurrence resolution remains CLI-owned. Future-scope deletion is non-atomic:
the instance is deleted before the parent recurrence is patched. Patch failure
returns non-zero plus structured `deleted=true`, exact target/parent IDs, and
`seriesUpdated=false`; callers must read back both resources and repair
manually rather than retrying blindly. Whole-calendar deletion
(`calendar_delete_calendar`) remains absent under every selector.

### Calendar integration and specialized-field exclusions (E05)

`calendar_create_event` and `calendar_update_event` are ordinary-event
schemas only. Their closed input schemas and generated argv reject Meet and
Zoom controls, Zoom password or external-credential inputs, Places lookup and
place-ID/language/region/API-key inputs, and password-bearing output controls.
They also reject attachments, source URL/title fields, private/shared/other
extended properties, and Focus Time, out-of-office, or working-location
specialized-event fields. No integration flag is synthesized in ordinary
Calendar argv.

The dedicated `calendar_focus_time`, `calendar_out_of_office`, and
`calendar_working_location` tools keep those specialized fields in separate
schemas; their registration cannot widen either ordinary create/update schema.
No Calendar Meet, Zoom, Places, or credential tool is registered under exact,
service, wildcard, risk, or `all` selectors. Calendar output continues to
redact Zoom join passwords by default (`pwd=REDACTED` and
`Passcode: REDACTED`); the direct CLI password-inclusion control is not an MCP
input.


### Bounded binary transport (B01 decision; B03 encoder)

`drive_download` is registered as a bounded Read tool in B04. B01 selects
**inline padded standard base64 in the `tools/call` structured result**, rather
than MCP resources. B03 implements the reusable inline encoder; it adds no
resource URI, callback, HTTP endpoint, temporary file, or host path. The
pinned `mark3labs/mcp-go` v0.57.0 stdio transport is newline-delimited
JSON-RPC, and its client can consume `CallTool` structured content plus the
JSON text fallback. The single tool-call shape keeps direct stdio and the
intended LiteLLM route compatible without resource discovery or a second
request.

The successful structured content object in the standard MCP envelope is:

```json
{
  "name": "report.pdf",
  "mimeType": "application/pdf",
  "size": 1234,
  "contentBase64": "..."
}
```

`contentBase64` uses padded RFC 4648 standard base64; `size` is the exact
decoded byte count; and `mimeType` must be non-empty and syntactically valid.
The adapter delegates MIME selection to the existing Drive/export command and
does not sniff bytes. `name` is metadata only: it is reduced to a final
component after both slash characters, maps empty/`.`/`..` to `download`, is
capped at 255 UTF-8 bytes, and is never interpreted as a host path.

The raw-content ceiling is **65,536 bytes (64 KiB) per call**, inclusive. B02's
shared `--max-bytes` CLI cap counts raw bytes with the same semantics; B04
passes the ceiling as a server-controlled fixed argument and exposes only
explicit `file_id` plus optional supported export `format`.

For each call B04 first invokes `drive get --fields id,name,mimeType` through
the typed child path, then invokes `drive download` with fixed
`--max-bytes 65536 --out -` for server-side in-memory capture. The raw child
omits only inherited `--json`; that fixed capture flag is never model supplied.
`max_bytes`, `tab`, `out`, `overwrite`, raw stdout, host paths, stdin, `@file`,
and generic argv are absent from the schema. At cap succeeds; cap+1 fails with
a non-zero structured error (`binary_size_limit`) and no partial base64 or
truncation sentinel. CLI file-output mode removes an over-limit temporary
partial and leaves an existing destination untouched.

The MCP `--max-output-bytes` stdout/stderr cap remains separate. The B03
encoder checks the encoded binary object against that configured cap before
constructing the result; its bounded child runner must reject capture overflow
rather than parse the current truncation marker or emit malformed JSON. The
64 KiB raw ceiling is sized to fit the default 102,400-byte stdout cap with
base64 and bounded metadata overhead.

Binary bytes exist only in the completed `tools/call` response. The server
retains no URI, temporary file, cache, or replay handle; timeout, cancellation,
or client disconnect discards the result. Repeated calls for the same ID/name
are independent and cannot overwrite host data. `--overwrite` remains a
direct-CLI-only control and is absent from the MCP schema and argv.

B02–B04 must cover cap−1/cap/cap+1, exact base64 and metadata, invalid MIME,
stdout-cap overflow without malformed JSON, timeout/disconnect cleanup, repeat
calls without collision, and rejection of all filesystem/resource/generic-argv
escape fields.

B03 supplies the reusable inline encoder used by B04. The encoder and adapter
create no resource URI, temporary file, cache, or replay handle; their bytes
exist only in the completed `tools/call` response.

### Tool bounds and partial failures

The current schema bounds and defaults are normative alongside the registry:
- Gmail search is `max` 1–100 (default 10); draft listing is 1–100 (default
  20); `gmail_update_draft` requires `draft_id` and at least one inline
  `body`/`body_html`, with subject required unless a reply target is supplied.
  Draft update rebuilds the full MIME message: omitted `to` is preserved,
  omitted `cc`/`bcc` are cleared unless reply-all derives them, a lone plain or
  HTML body retains that content type, and both bodies produce multipart
  alternative. Explicit-ID archive/read-state arrays and
  `gmail_trash_messages` accept 1–1,000 IDs; trash IDs are alphanumeric and
  remain recoverable through Gmail's `TRASH` label.
- Calendar event/search results are 1–250 (default 10), calendar listing is
  1–250 (default 100), and event/conflict `days` is 0–31. `calendar_events`
  accepts a nonempty opaque `page_token` forwarded unchanged as
  `--page=<token>` (including leading dashes), or bounded `all_pages`, but not
  both; multi-calendar selectors, event types, sorting/order, and field masks
  are outside this MCP schema.
  Calendar-ID arrays are capped at 100, event attendees/recurrence at 100,
  reminders at 5, and Focus Time recurrence at 100. Event notifications
  default to `none`; `calendar_update_event` leaves omitted fields unchanged.
  `calendar_delete_event` requires `calendar_id`, `event_id`, and scope
  (`single`, `future`, or `all`); `original_start` is required for single/future
  and rejected for all. The adapter appends `--force` and leaves recurrence
  resolution to the CLI.
  Supplied empty `summary`, `description`, `location`, `attendees`, `rrule`,
  `reminders`, or `event_color` values serialize explicit clears. Supplied
  guest-permission `false` values serialize disables. Empty `scope` or
  `send_updates`, and `all_day` or timezone values without required paired
  `start`/`end` fields, are rejected before execution.
- Drive search and folder listing are 1–100 (default 20); `drive_search` can
  scope to one shared drive with `drive_id`; permission listing is 1–100
  (default 100). Folder listing includes shared drives by default; `drive_download`
  requires `file_id`, optionally accepts the supported export `format` enum, and
  returns inline bytes capped at 65,536 raw bytes.
- `docs_get.max_bytes` is 0–20,000,000 (default 2,000,000), with 0 retaining
  the CLI unlimited value. `docs_write` appends by default and requires an
  explicit replace mode when append is disabled.
- Sheets tab names and Slides replacements are capped at 100; Sheets updates
  default to `USER_ENTERED` and use the concrete-A1 rule above.

MCP does not make child commands transactional. The standard structured result
always preserves `exit_code`, parsed `stdout`, and `stderr`; a non-zero result
does not roll back provider effects. Gmail archive and thread/label handlers
preserve per-item success/error records where their CLI contracts do so.
`gmail_trash_messages` is different: its bounded 1–1,000-ID call is one
aggregate `BatchModify` request, so success exposes only aggregate
action/count/labels and provider failure exposes the standard non-zero
envelope without per-item evidence. Provider effects are indeterminate on
failure. The direct CLI may process more than 1,000 IDs in multiple
`BatchModify` chunks; MCP's bound prevents one call from crossing that chunk
boundary.
`docs_create` may leave a created Doc when its post-create pageless update
fails. `sheets_create` keeps a successfully created spreadsheet when an
advisory parent move fails (`movedToParent=false`, `moveError`, and a warning
on `stderr`). `slides_create_from_template` copies first; replacement failure
returns non-zero and reports the created presentation ID for cleanup.


### Semantic compatibility notes

- `calendar_find_conflicts` (`C05`) preserves the CLI's deduplicated
  pairwise-overlap output in the CLI's detection order (its map/pairwise
  traversal). Results are not globally sorted by start, end, or calendar;
  callers must not assume global sorting.
- `calendar_create_calendar` (`C10`) requires out-of-band cleanup because
  whole-calendar deletion is excluded from MCP. Create the secondary
  calendar, record the returned calendar ID, then run this command outside
  MCP:

  ```bash
  gog calendar delete-calendar CALENDAR_ID
  ```

- `drive_copy` (`V06`) preserves the CLI's source pre-read before issuing the
  copy. If that pre-read fails, no copy is attempted. Folder copies are
  shallow: only the folder itself is copied, not its descendants.

## Language/runtime

- Go `1.26` (see `go.mod`)

## CLI framework

- `github.com/alecthomas/kong`
- Root command: `gog`
- Global flag:
  - `--color=auto|always|never` (default `auto`)
  - `--json` (JSON output to stdout)
  - `--plain` (TSV output to stdout; stable/parseable; disables colors)
  - `--force` (skip confirmations for destructive commands)
  - `--no-input` (never prompt; fail instead)
  - `--version` (print version)

Notes:

- We run `SilenceUsage: true` and print errors ourselves (colored when possible).
- `NO_COLOR` is respected.

Environment:

- `GOG_COLOR=auto|always|never` (default `auto`, overridden by `--color`)
- `GOG_JSON=1` (default JSON output; overridden by flags)
- `GOG_PLAIN=1` (default plain output; overridden by flags)

## Output (TTY-aware colors)

- `github.com/muesli/termenv` is used to detect rich TTY capabilities and render colored output.
- Colors are enabled when:
  - output is a rich terminal and `--color=auto`, and `NO_COLOR` is not set; or
  - `--color=always`
- Colors are disabled when:
  - `--color=never`; or
  - `NO_COLOR` is set

Implementation: `internal/ui/ui.go`.

## Auth + secret storage

### OAuth client credentials (non-secret-ish)

- Stored on disk in the per-user config directory:
  - `$(os.UserConfigDir())/gogcli/credentials.json` (default client)
  - `$(os.UserConfigDir())/gogcli/credentials-<client>.json` (named clients)
- Written with mode `0600`.
- Command:
  - `gog auth credentials <credentials.json>`
  - `gog --client <name> auth credentials <credentials.json>`
  - `gog auth credentials list`
  - `gog auth credentials remove [<client>|all]`
- Supports Google’s downloaded JSON format:
  - `installed.client_id/client_secret` or `web.client_id/client_secret`

Implementation: `internal/config/*`.

### Refresh tokens (secrets)

- Stored in OS credential store via `github.com/99designs/keyring`.
- Key namespace is `gogcli` by default (keyring `ServiceName`); override with `GOG_KEYRING_SERVICE_NAME`.
- Key format: `token:<client>:<email>` (default client uses `token:default:<email>`)
- Canonical identity key format for new tokens with an OIDC subject: `token-sub:<client>:<sub>`. Email-keyed entries remain as compatibility lookup keys.
- Legacy key format: `token:<email>` (migrated on first read)
- Stored payload is JSON (refresh token + metadata like OIDC subject, current email, selected services/scopes).
- Email is treated as display/contact state; Google's OIDC `sub` is used to detect the same account after an email rename and migrate aliases/defaults/client mappings on reauthorization.
- Keyring operations are bounded by a timeout (default `30s` on macOS and `10s` elsewhere, configurable via `GOG_KEYRING_OPEN_TIMEOUT`) so non-surfacing permission prompts and unresponsive backends return actionable guidance instead of hanging indefinitely.
- Fallback: if no OS credential store is available, keyring may use its encrypted "file" backend:
  - Directory: `$(os.UserConfigDir())/gogcli/keyring/` (one file per key; gog-managed key names are encoded for portable filenames)
  - Password: prompts on TTY; for non-interactive runs set `GOG_KEYRING_PASSWORD`

Current minimal management commands (implemented):

- `gog auth tokens list` (keys only; does not decrypt token payloads)
- `gog auth tokens delete <email>`
- `gog auth list` reports unreadable token entries instead of failing the whole listing, so one bad file-keyring entry does not hide other accounts.

Implementation: `internal/secrets/store.go`.

### OAuth flow

- Desktop OAuth 2.0 flow using local HTTP redirect on an ephemeral port.
- Supports a browserless/manual flow (paste redirect URL) for headless environments.
- Supports a remote/server-friendly 2-step manual flow:
  - Step 1 prints an auth URL (`gog auth add ... --remote --step 1`)
  - Step 2 exchanges the pasted redirect URL and requires `state` validation (`--remote --step 2 --auth-url ...`)
  - Browser, manual, remote, and account-manager flows bind authorization
    requests and token exchanges with S256 PKCE.
  - Remote steps must share the same config home and OAuth client. Unfinished
    pre-v0.24.0 flows must restart at step 1.
- Refresh token issuance:
  - requests `access_type=offline`
  - supports `--force-consent` to force the consent prompt when Google doesn't return a refresh token
  - uses `include_granted_scopes=true` to support incremental auth re-runs

Scope selection note:

- The consent screen shows the scopes the CLI requested.
- Users cannot selectively un-check individual requested scopes in the consent screen; they either approve all requested scopes or cancel.
- To request fewer scopes, choose fewer services via `gog auth add --services ...` or use `gog auth add --readonly` where applicable.

## Config layout

- Base config dir: `$(os.UserConfigDir())/gogcli/`
- Files:
  - `config.json` (JSON5; comments and trailing commas allowed)
  - `credentials.json` (OAuth client id/secret; default client)
  - `credentials-<client>.json` (OAuth client id/secret; named clients)
- State:
  - `state/gmail-watch/<account>.json` (Gmail watch state)
  - `oauth-manual-state-<state>.json` (temporary manual OAuth state and PKCE verifier cache; expires quickly; no tokens)
- Secrets:
  - refresh tokens in keyring

We intentionally avoid storing refresh tokens in plain JSON on disk.

Environment:

- `GOG_ACCOUNT=you@gmail.com` (email or alias; used when `--account` is not set; otherwise uses keyring default or a single stored token)
- `GOG_CLIENT=work` (select OAuth client bucket; see `--client`)
- `GOG_KEYRING_PASSWORD=...` (used when keyring falls back to encrypted file backend in non-interactive environments)
- `GOG_KEYRING_BACKEND={auto|keychain|file}` (force backend; use `file` to avoid Keychain prompts and pair with `GOG_KEYRING_PASSWORD` for non-interactive)
- `GOG_KEYCHAIN_TRUST_APPLICATION={auto|true|false}` (control macOS Keychain application trust; auto enables it only for a stably signed binary)
- `GOG_KEYRING_SERVICE_NAME=...` (override keyring namespace/service name; default `gogcli`)
- `GOG_KEYRING_OPEN_TIMEOUT=30s` (max time to wait for a keyring open/operation — e.g. a macOS Keychain permission prompt — before failing; Go duration, default `30s` on macOS and `10s` elsewhere)
- `GOG_TIMEZONE=America/New_York` (default output timezone; IANA name or `UTC`; `local` forces local timezone)
- `GOG_ENABLE_COMMANDS=calendar,tasks,gmail.search` (optional prefix allowlist; dot paths allowed; parent paths allow children)
- `GOG_ENABLE_COMMANDS_EXACT=calendar.events,gmail.search` (optional exact allowlist; dot paths allowed; parent paths do not allow children)
- `GOG_DISABLE_COMMANDS=gmail.send,gmail.drafts.send` (optional denylist; dot paths allowed)
- `GOG_GMAIL_NO_SEND=1` (block Gmail send operations)
- `config.json` can also set `keyring_backend` (JSON5; env vars take precedence)
- `config.json` can also set `default_timezone` (IANA name or `UTC`)
- `config.json` can also set `places_api_key` (or use `GOG_PLACES_API_KEY` / `GOOGLE_PLACES_API_KEY`) for Calendar Places lookups.
- `config.json` can also set `account_aliases` for `gog auth alias` (JSON5)
- `config.json` can also set `account_clients` (email -> client) and `client_domains` (domain -> client)
- `config.json` can also set `gmail_no_send` and `no_send_accounts` for send guards

Flag aliases:
- `--out` also accepts `--output`.
- `--out-dir` also accepts `--output-dir` (Gmail thread attachment downloads).
- Drive download/export commands accept `--out -` to write file bytes to stdout; `--json --out -` is rejected.

## Commands (current + planned)

### Implemented

- `gog auth credentials <credentials.json|->`
- `gog auth credentials list`
- `gog auth credentials remove [<client>|all]`
- `gog --client <name> auth credentials <credentials.json|->`
- `gog auth add <email> [--services user|all-user|all|gmail,calendar,chat,classroom,drive,driveactivity,drivelabels,docs,slides,contacts,tasks,sheets,people,forms,sites,meet,photos,photospicker,appscript,analytics,searchconsole,ads,youtube] [--readonly] [--drive-scope full|readonly|file] [--gmail-scope full|readonly] [--extra-scopes CSV] [--manual] [--remote] [--step 1|2] [--auth-url URL] [--listen-addr HOST[:PORT]] [--redirect-host HOST] [--timeout DURATION] [--force-consent]`
- `gog auth services [--markdown]`
- `gog auth manage [--services ...] [--listen-addr HOST[:PORT]] [--redirect-host HOST] [--dry-run]` (interactive browser flow; real execution fails with usage exit code 2 under `--no-input`)
- `gog auth keep <email> --key <service-account.json>` (Google Keep; Workspace only)
- `gog auth list`
- `gog auth doctor [--check]` (diagnose keyring/password drift and refresh-token failures)
- `gog auth alias list`
- `gog auth alias set <alias> <email>`
- `gog auth alias unset <alias>`
- `gog auth status`
- `gog auth remove <email>`
- `gog auth tokens list`
- `gog auth tokens delete <email>`
- `gog config get <key>`
- `gog config keys`
- `gog config list`
- `gog config path`
- `gog config set <key> <value>`
- `gog config unset <key>`
- `gog version`
- `gog drive ls [--all] [--parent ID] [--max N] [--page TOKEN] [--query Q] [--[no-]all-drives]` (`--all` and `--parent` are mutually exclusive)
- MCP `drive_list_folder` maps to `gog drive ls`: `folder_id` selects `--parent`, `max` is bounded to 1–100, `page_token` selects `--page`, and `include_shared_drives` defaults true (false emits `--no-all-drives`). It exposes neither `--all`, `--query`, nor `--fields`; results preserve `files` and `nextPageToken`.
- `gog drive search <text> [--raw-query] [--max N] [--page TOKEN] [--[no-]all-drives]`
- `gog drive get <fileId>`
- `gog drive download <fileId> [--out PATH|-] [--format F]` (`--format` only applies to Google Workspace files; `--format md` exports a Google Doc as Markdown)
- `gog drive upload <localPath> [--name N] [--parent ID] [--convert] [--convert-to doc|sheet|slides] [--keep-frontmatter]` (Markdown → Google Doc with `--convert` or `--convert-to doc`: leading `---`/`---` frontmatter is stripped before upload unless `--keep-frontmatter`; delimiter-based, not a full YAML parse; large non-JSON uploads print progress to stderr)
- `gog drive sync push <localDirectory> --parent ID [--dry-run] [--[no-]all-drives]` (recursively reconciles local contents without deleting remote-only files; duplicate names, wrong types, Google-native files, and local symlinks fail before mutation)
- `gog drive mkdir <name> [--parent ID]`
- `gog drive delete <fileId> [--permanent]`
- `gog drive move <fileId> --parent ID`
- `gog drive rename <fileId> <newName>`
- `gog drive shortcut create <targetId> --parent ID [--name N]`
- `gog drive share <fileId> --to anyone|user|domain [--email addr] [--domain example.com] [--role reader|writer|commenter] [--discoverable]`
- `gog drive permissions <fileId> [--max N] [--page TOKEN]`
- `gog drive unshare <fileId> <permissionId>`
- `gog drive url <fileIds...>`
- `gog drive drives [--max N] [--page TOKEN] [--query Q]`
- `gog drive changes start-token [--drive DRIVE_ID]`
- `gog drive changes list --token TOKEN [--max N] [--all] [--drive DRIVE_ID]`
- `gog drive changes poll --state-file PATH [--interval DURATION] [--on-change COMMAND] [--filter-file FILE_ID] [--drive DRIVE_ID]`
- `gog drive changes serve --state-file PATH (--channel-token TOKEN|--channel-token-file PATH) [--listen ADDR] [--notification-timeout DURATION] [--on-change COMMAND] [--filter-file FILE_ID] [--auto-renew --webhook-url HTTPS_URL]`
- `gog drive changes watch --token TOKEN --webhook-url URL [--channel-id ID] [--channel-token TOKEN]`
- `gog drive changes stop <channelId> <resourceId>`
- `gog drive activity query [--file FILE_ID|--folder FOLDER_ID] [--actions edit,share] [--from RFC3339] [--to RFC3339] [--filter FILTER]`
- `gog drive audit sharing [--file FILE_ID|--parent FOLDER_ID] [--depth N] [--max N] [--internal-domain DOMAIN] [--public-only|--external-only] [--fail-found]`
- `gog drive audit user <email> [--file FILE_ID|--parent FOLDER_ID] [--depth N] [--max N] [--fail-found]`
- `gog drive bulk remove-public [--file FILE_ID|--parent FOLDER_ID] [--depth N] [--dry-run] [--force]`
- `gog drive bulk update-role --from reader|commenter|writer --to reader|commenter|writer [--file FILE_ID|--parent FOLDER_ID] [--type user|group|domain|anyone] [--target EMAIL_OR_DOMAIN] [--dry-run] [--force]`
- `gog drive labels list [--max N] [--page TOKEN] [--customer CUSTOMERS_ID] [--published-only]` (requires a Google Workspace customer)
- `gog drive labels get <labelId|labels/ID> [--view basic|full]` (requires a Google Workspace customer)
- `gog drive labels file list <fileId> [--max N] [--page TOKEN]`
- `gog drive labels file apply <fileId> <labelId> [--text FIELD=VALUE] [--selection FIELD=CHOICE[,CHOICE]] [--integer FIELD=N] [--date FIELD=YYYY-MM-DD] [--user FIELD=email] [--unset FIELD] [--fields-json JSON]`
- `gog drive labels file remove <fileId> <labelId>`

Drive hierarchy semantics:

- Files and folders are identified by stable opaque IDs, not paths.
- New files have one parent folder. The API still returns `parents` as an array
  so legacy My Drive records with multiple parents can be read; `drive move`
  removes every old parent and installs exactly the requested parent.
- An item visible from another folder is represented by a separate shortcut
  file with its own ID, name, parent, and permissions. Shortcut metadata exposes
  `shortcutDetails.targetId`, `targetMimeType`, and `targetResourceKey`.
- Mutations apply to the exact ID passed. Commands do not silently dereference
  shortcut IDs to their targets.
- `drive tree`, `drive inventory`, and `drive du` treat shortcuts as leaves,
  including shortcuts whose targets are folders.
- Tree and inventory output one row per discovered placement. Size summaries
  aggregate each placement independently, even when legacy parent links expose
  the same folder ID through multiple paths.
- Folder scans reject an ancestor cycle instead of following it indefinitely.

- `gog slides thumbnail <presentationId> <slideId> [--size small|medium|large] [--format png|jpeg] [--out PATH]`
- `gog slides element <create-shape|create-line|transform|style|z-order|group|ungroup|alt-text|delete> ...` (native page-element lifecycle; exact batch payloads available with `--dry-run --json`)
- `gog calendar calendars`
- `gog calendar subscribe <calendarId>`
- `gog calendar unsubscribe <calendarId>`
- `gog calendar create-calendar <summary> [--description D] [--timezone TZ] [--location L]`
- `gog calendar delete-calendar <ownedSecondaryCalendarId>`
- `gog calendar acl <calendarId>`
- `gog calendar events <calendarId> [--cal ID_OR_NAME] [--calendars CSV] [--all] [--from RFC3339] [--to RFC3339] [--max N] [--page TOKEN] [--query Q] [--event-types TYPES] [--weekday]`
  - `--event-types` filters to one or more event types (repeatable or comma-separated): `default`, `birthday`, `focus-time`, `from-gmail`, `out-of-office`, `working-location`. Unset returns all types (the API default).
- `gog calendar event|get <calendarId> <eventId>`
- `GOG_CALENDAR_WEEKDAY=1` defaults `--weekday` for `gog calendar events`
- `gog calendar create <calendarId> --summary S --from DT --to DT [--timezone TZ] [--start-timezone TZ] [--end-timezone TZ] [--description D] [--location L|--location-search Q|--place-id ID] [--place-language LANG] [--place-region REGION] [--attendees a@b.com,c@d.com] [--all-day] [--event-type TYPE]`
- `gog calendar update <calendarId> <eventId> [--summary S] [--from DT] [--to DT] [--start-timezone TZ] [--end-timezone TZ] [--description D] [--location L|--location-search Q|--place-id ID] [--place-language LANG] [--place-region REGION] [--attendees ...] [--add-attendee ...] [--attachment URL ...] [--all-day] [--with-meet|--regenerate-meet] [--event-type TYPE]`
- `gog calendar delete <calendarId> <eventId> [--scope single|future|all] [--original-start DT] [--send-updates all|externalOnly|none] [--force]`
- `gog calendar freebusy [calendarIds] [--cal ID_OR_NAME] [--calendars CSV] [--all] --from RFC3339 --to RFC3339`
- `gog calendar conflicts [--cal ID_OR_NAME] [--calendars CSV] [--all] [--from RFC3339|date|relative] [--to RFC3339|date|relative] [--today|--week|--days N]`
- `gog calendar respond <calendarId> <eventId> --status accepted|declined|tentative [--send-updates all|none|externalOnly]`

`calendar unsubscribe` removes only the selected entry from the caller's
calendar list. `calendar delete-calendar` permanently deletes an owned
secondary calendar; Google may briefly retain a stale calendar-list row after
the authoritative calendar resource is gone.

Google Calendar appointment schedules are not exposed by the Calendar API, so
the CLI cannot list or manage them.

- `gog maps places search <query> [--language LANG] [--region REGION] [--fields FIELD_MASK] [--max N]`
- `gog maps places details <placeId> [--language LANG] [--region REGION] [--fields FIELD_MASK]`
- `gog maps directions --origin ORIGIN --destination DESTINATION [--mode driving|walking|bicycling|transit] [--language LANG] [--region REGION]`
- `gog maps distance --origins CSV --destinations CSV [--mode driving|walking|bicycling|transit] [--units metric|imperial] [--language LANG] [--region REGION]`
- `gog maps geocode <address...> [--language LANG] [--region REGION]`
- `gog maps reverse-geocode --lat FLOAT --lng FLOAT [--language LANG] [--region REGION]`
- `gog photos list [--max N] [--page TOKEN]`
- `gog photos search [--album ALBUM_ID] [--media-type PHOTO|VIDEO|ALL_MEDIA] [--from YYYY-MM-DD] [--to YYYY-MM-DD] [--include-archived] [--max N] [--page TOKEN]`
- `gog photos get <mediaItemId>`
- `gog photos download <mediaItemId> [--out PATH|-] [--video]`
- `gog photos picker create [--max-items N] [--open]`
- `gog photos picker get <sessionId>`
- `gog photos picker wait <sessionId> [--timeout DURATION]`
- `gog photos picker list <sessionId> [--max N] [--page TOKEN] [--all]`
- `gog photos picker download <sessionId> <mediaItemId> [--out PATH|-] [--overwrite]`
- `gog photos picker delete <sessionId>`
- `gog time now [--timezone TZ]`
- `gog classroom courses [--state ...] [--max N] [--page TOKEN]`
- `gog classroom courses get <courseId>`
- `gog classroom courses create --name NAME [--owner me] [--state ACTIVE|...]`
- `gog classroom courses update <courseId> [--name ...] [--state ...]`
- `gog classroom courses delete <archivedCourseId>`
- `gog classroom courses archive <courseId>`
- `gog classroom courses unarchive <courseId>`
- `gog classroom courses join <courseId> [--role student|teacher] [--user me]`
- `gog classroom courses leave <courseId> [--role student|teacher] [--user me]`
- `gog classroom courses url <courseId...>`

Course state mutations wait for the requested state to become visible through
the Classroom API before returning success. If Google still serves stale state
after the bounded retry window, the command exits with retryable code `8`.

- `gog classroom students <courseId> [--max N] [--page TOKEN]`
- `gog classroom students get <courseId> <userId>`
- `gog classroom students add <courseId> <userId> [--enrollment-code CODE]`
- `gog classroom students remove <courseId> <userId>`
- `gog classroom teachers <courseId> [--max N] [--page TOKEN]`
- `gog classroom teachers get <courseId> <userId>`
- `gog classroom teachers add <courseId> <userId>`
- `gog classroom teachers remove <courseId> <userId>`
- `gog classroom roster <courseId> [--students] [--teachers]`
- `gog classroom coursework <courseId> [--state ...] [--topic TOPIC_ID] [--scan-pages N] [--max N] [--page TOKEN]`
- `gog classroom coursework get <courseId> <courseworkId>`
- `gog classroom coursework create <courseId> --title TITLE [--type ASSIGNMENT|...]`
- `gog classroom coursework update <courseId> <courseworkId> [--title ...]`
- `gog classroom coursework delete <courseId> <courseworkId>`
- `gog classroom coursework assignees <courseId> <courseworkId> [--mode ...] [--add-student ...]`
- `gog classroom materials <courseId> [--state ...] [--topic TOPIC_ID] [--scan-pages N] [--max N] [--page TOKEN]`
- `gog classroom materials get <courseId> <materialId>`
- `gog classroom materials create <courseId> --title TITLE`
- `gog classroom materials update <courseId> <materialId> [--title ...]`
- `gog classroom materials delete <courseId> <materialId>`
- `gog classroom submissions <courseId> <courseworkId> [--state ...] [--max N] [--page TOKEN]`
- `gog classroom submissions get <courseId> <courseworkId> <submissionId>`
- `gog classroom submissions turn-in <courseId> <courseworkId> <submissionId>`
- `gog classroom submissions reclaim <courseId> <courseworkId> <submissionId>`
- `gog classroom submissions return <courseId> <courseworkId> <submissionId>`
- `gog classroom submissions grade <courseId> <courseworkId> <submissionId> [--draft N] [--assigned N]`
- `gog classroom announcements <courseId> [--state ...] [--max N] [--page TOKEN]`
- `gog classroom announcements get <courseId> <announcementId>`
- `gog classroom announcements create <courseId> --text TEXT`
- `gog classroom announcements update <courseId> <announcementId> [--text ...]`
- `gog classroom announcements delete <courseId> <announcementId>`
- `gog classroom announcements assignees <courseId> <announcementId> [--mode ...]`
- `gog classroom topics <courseId> [--max N] [--page TOKEN]`
- `gog classroom topics get <courseId> <topicId>`
- `gog classroom topics create <courseId> --name NAME`
- `gog classroom topics update <courseId> <topicId> --name NAME`
- `gog classroom topics delete <courseId> <topicId>`
- `gog classroom invitations [--course ID] [--user ID]`
- `gog classroom invitations get <invitationId>`
- `gog classroom invitations create <courseId> <userId> --role STUDENT|TEACHER|OWNER`
- `gog classroom invitations accept <invitationId>`
- `gog classroom invitations delete <invitationId>`
- `gog classroom guardians <studentId> [--max N] [--page TOKEN]`
- `gog classroom guardians get <studentId> <guardianId>`
- `gog classroom guardians delete <studentId> <guardianId>`
- `gog classroom guardian-invitations <studentId> [--state ...] [--max N] [--page TOKEN]`
- `gog classroom guardian-invitations get <studentId> <invitationId>`
- `gog classroom guardian-invitations create <studentId> --email EMAIL`
- `gog classroom profile [userId]`
- `gog contacts dedupe [--match email,phone,name] [--max N] [--resource people/...] [--apply]`
- `gog gmail search <query> [--max N] [--page TOKEN]`
- `gog gmail messages search <query> [--max N] [--page TOKEN] [--include-body] [--body-format text|html] [--full]`
- `gog gmail autoreply <query> [--max N] [--subject S] [--body B|--body-file PATH|--body-html HTML] [--from addr] [--reply-to addr] [--label L] [--archive] [--mark-read] [--skip-bulk] [--allow-self]`
- `gog gmail thread get <threadId> [--download]`
- `gog gmail thread modify <threadId> [--add ...] [--remove ...]`
- `gog gmail get <messageId> [--format full|metadata|raw] [--headers ...]`
- `gog gmail attachment <messageId> <attachmentId> [--out PATH] [--name NAME] [--inline] [--inline-max-bytes BYTES]`
- `gog gmail url <threadIds...>`
- `gog gmail reply <messageId> [--body B|--body-file PATH|--body-html HTML|--body-html-file PATH] [--to ...] [--cc ...] [--bcc ...] [--remove ...] [--subject S] [--no-quote] [--from addr|--auto-from-addressed-alias] [--signature|--signature-from addr|--signature-file path] [--attach <file>...]`
- `gog gmail reply-all <messageId> [--body B|--body-file PATH|--body-html HTML|--body-html-file PATH] [--to ...] [--cc ...] [--bcc ...] [--remove ...] [--subject S] [--no-quote] [--from addr|--auto-from-addressed-alias] [--signature|--signature-from addr|--signature-file path] [--attach <file>...]`
- `gog gmail forward <messageId> --to a@b.com [--cc ...] [--bcc ...] [--note TEXT|--note-file PATH] [--from addr] [--skip-attachments]`
- `gog gmail labels list`
- `gog gmail labels get <labelIdOrName>`
- `gog gmail labels create <name>`
- `gog gmail labels rename <labelIdOrName> <newName>`
- `gog gmail labels modify <threadIds...> [--add ...] [--remove ...]`
- `gog gmail send --to a@b.com [--subject S] [--body B|--body-file PATH] [--body-html H|--body-html-file PATH] [--cc ...] [--bcc ...] [--reply-to-message-id <messageId>] [--reply-to addr] [--from addr] [--signature|--signature-from addr|--signature-file path] [--attach <file>...]`
- `gog gmail drafts list [--max N] [--page TOKEN]`
- `gog gmail drafts get <draftId> [--download]`
- `gog gmail drafts create [--subject S] [--to a@b.com] [--body B] [--body-html H] [--cc ...] [--bcc ...] [--reply-to-message-id <messageId>|--thread-id <threadId>] [--reply-all] [--reply-to addr] [--from addr|--auto-from-addressed-alias] [--attach <file>...]`
- `gog gmail drafts update <draftId> [--subject S] [--to a@b.com] [--body B] [--body-html H] [--cc ...] [--bcc ...] [--reply-to-message-id <messageId>|--thread-id <threadId>] [--reply-all] [--reply-to addr] [--from addr|--auto-from-addressed-alias] [--attach <file>...]`
- `gog gmail drafts send <draftId>`
- `gog gmail drafts delete <draftId>`
- `gog gmail watch start|status|renew|stop|serve`
- `gog gmail history --since <historyId>`
- `gog chat spaces list [--max N] [--page TOKEN]`
- `gog chat spaces find <displayName> [--max N] [--exact]`
- `gog chat spaces create <displayName> [--member email,...]`
- `gog chat messages list <space> [--max N] [--page TOKEN] [--order ORDER] [--thread THREAD] [--unread]`
- `gog chat messages send <space> --text TEXT [--thread THREAD]`
- `gog chat threads list <space> [--max N] [--page TOKEN]`
- `gog chat dm space <email>`
- `gog chat dm send <email> --text TEXT [--thread THREAD]`
- `gog tasks lists [--max N] [--page TOKEN]`
- `gog tasks lists create <title>`
- `gog tasks list <tasklistId> [--max N] [--page TOKEN]`
- `gog tasks get <tasklistId> <taskId>`
- `gog tasks add <tasklistId> --title T [--notes N] [--due RFC3339|YYYY-MM-DD] [--repeat daily|weekly|monthly|yearly] [--repeat-count N] [--repeat-until DT] [--parent ID] [--previous ID]`
- `gog tasks update <tasklistId> <taskId> [--title T] [--notes N] [--due RFC3339|YYYY-MM-DD] [--status needsAction|completed]`
- `gog tasks done <tasklistId> <taskId>`
- `gog tasks undo <tasklistId> <taskId>`
- `gog tasks delete <tasklistId> <taskId>`
- `gog tasks clear <tasklistId>`
- `gog contacts search <query> [--max N]`
- `gog contacts list [--max N] [--page TOKEN]`
- `gog contacts get <people/...|email>`
- `gog contacts export <people/...|email|name> [--out PATH|-]`
- `gog contacts export --query <query> [--max N] [--out PATH|-]`
- `gog contacts export --all [--page-size N] [--page TOKEN] [--out PATH|-]`
- `gog contacts create --given NAME [--family NAME] [--email addr] [--phone num] [--relation type=person]`
- `gog contacts update <people/...> [--given NAME] [--family NAME] [--email addr] [--phone num] [--birthday YYYY-MM-DD] [--notes TEXT] [--relation type=person] [--from-file PATH|-] [--ignore-etag]`
- `gog contacts delete <people/...>`
- `gog contacts directory list [--max N] [--page TOKEN]`
- `gog contacts directory search <query> [--max N] [--page TOKEN]`
- `gog contacts other list [--max N] [--page TOKEN]`
- `gog contacts other search <query> [--max N]`
- `gog people me`
- `gog people get <people/...|userId>`
- `gog people search <query> [--max N] [--page TOKEN]`
- `gog people relations [<people/...|userId>] [--type TYPE]`

Date/time input conventions (shared parser):

- Date-only: `YYYY-MM-DD`
- Datetime: `RFC3339` / `RFC3339Nano` / `YYYY-MM-DDTHH:MM[:SS]` / `YYYY-MM-DD HH:MM[:SS]`
- Numeric timezone offset accepted: `YYYY-MM-DDTHH:MM:SS-0800`
- Calendar range flags also accept relatives: `now`, `today`, `tomorrow`, `yesterday`, weekday names (`monday`, `next friday`)
- Tracking `--since` also accepts durations like `24h`

### Planned high-level command tree

- `gog auth …`
  - `gog auth credentials <credentials.json>`
  - `gog auth credentials list`
  - `gog --client <name> auth credentials <credentials.json>`
- `gog gmail …`
- `gog chat …`
- `gog calendar …`
- `gog drive …`
- `gog contacts …`
- `gog tasks …`
- `gog people …`

Planned service identifiers (canonical):

- `gmail`
- `calendar`
- `chat`
- `drive`
- `contacts`
- `tasks`
- `people`

## Google API dependencies (planned)

- `golang.org/x/oauth2`
- `golang.org/x/oauth2/google`
- `google.golang.org/api/option`
- `google.golang.org/api/gmail/v1`
- `google.golang.org/api/calendar/v3`
- `google.golang.org/api/chat/v1`
- `google.golang.org/api/drive/v3`
- `google.golang.org/api/people/v1`
- `google.golang.org/api/tasks/v1`

## Scopes (planned)

We store a single refresh token per Google account email.

- `gog auth add` requests a union of scopes based on `--services`.
- Each API client refreshes an access token for the subset of scopes needed for that service.
- If you later want additional services, re-run `gog auth add <email> --services ...` (may require `--force-consent` to mint a new refresh token).

- Gmail: `https://mail.google.com/` (or narrower scopes if we decide later)
- Calendar: `https://www.googleapis.com/auth/calendar`
- Chat:
  - `https://www.googleapis.com/auth/chat.spaces`
  - `https://www.googleapis.com/auth/chat.messages`
  - `https://www.googleapis.com/auth/chat.memberships`
  - `https://www.googleapis.com/auth/chat.users.readstate.readonly`
- Drive: `https://www.googleapis.com/auth/drive`
- Drive Labels: `https://www.googleapis.com/auth/drive.labels.readonly`
- Contacts/Directory:
  - `https://www.googleapis.com/auth/contacts`
  - `https://www.googleapis.com/auth/contacts.other.readonly`
  - `https://www.googleapis.com/auth/directory.readonly`
- People:
  - `profile` (OIDC)
- YouTube:
  - `https://www.googleapis.com/auth/youtube.readonly` for normal account reads
  - `https://www.googleapis.com/auth/youtube.force-ssl` as an explicit extra scope for comments and mutations
- Photos: `https://www.googleapis.com/auth/photoslibrary.readonly.appcreateddata`
- Photos Picker: `https://www.googleapis.com/auth/photospicker.mediaitems.readonly` (explicit opt-in)

## Output formats

Default: human-friendly tables (stdlib `text/tabwriter`).

- Parseable stdout:
  - `--json`: JSON objects/arrays suitable for scripting
  - `--plain`: stable TSV (tabs preserved; no alignment; no colors)
- Human-facing hints/progress are written to stderr so stdout can be safely captured.
- Colors are only used for human-facing output and are disabled automatically for `--json` and `--plain`.

We avoid heavy table deps unless we decide we need them.

## Code layout (current)

- `cmd/gog/main.go` — binary entrypoint
- `internal/cmd/*` — kong command structs
- `internal/ui/*` — color + printing
- `internal/config/*` — config paths + credential parsing/writing
- `internal/secrets/*` — keyring store

## Formatting, linting, tests

### Formatting

Pinned tools, installed into local `.tools/` via `make tools`:

- `mvdan.cc/gofumpt@v0.7.0`
- `golang.org/x/tools/cmd/goimports@v0.38.0`
- `github.com/golangci/golangci-lint/cmd/golangci-lint@v1.62.2`

Commands:

- `make fmt` — applies `goimports` + `gofumpt`
- `make fmt-check` — formats and fails if Go files or `go.mod/go.sum` change

### Lint

- `golangci-lint` with config in `.golangci.yml`
- `make lint`

### Tests

- stdlib `testing` (+ `httptest` when we add OAuth/API tests)
- `make test`

### Integration tests (local only)

There is an opt-in integration test suite guarded by build tags (not run in CI).

- Requires:
  - stored `credentials.json` (or `credentials-<client>.json`) via `gog auth credentials ...`
  - refresh token in keyring via `gog auth add <email>`
- Run:
  - `GOG_IT_ACCOUNT=you@gmail.com go test -tags=integration ./internal/integration`
  - optional: `GOG_CLIENT=work` to select a non-default OAuth client

## CI (GitHub Actions)

Workflow: `.github/workflows/ci.yml`

- runs on push + PR
- uses `actions/setup-go` with `go-version-file: go.mod`
- runs:
  - `make tools`
  - `make fmt-check`
  - `go test ./...`
  - `golangci-lint` (pinned `v1.62.2`)

## Next implementation steps

- Expand Gmail further (labels by name everywhere, richer body rendering, compose edge cases).
- Improve People updates (multi-field + richer contact data).
- Harden UX (consistent output formats, retries/backoff on specific transient errors).

## MCP risk annotations

MCP tools use three risk classes in their protocol annotations:

| Risk | `readOnlyHint` | `destructiveHint` | Exposure |
| --- | ---: | ---: | --- |
| Read | `true` | `false` | Existing read-only default |
| Write | `false` | `false` | Existing write authorization |
| Destructive | `false` | `true` | Ordinary write authorization plus explicit `destructive` or exact-tool selection; all six X01–X06 tools use this gate |

Every class keeps `openWorldHint=true` because MCP calls Google services.
R01 supplied the third risk class and annotations. R02/R03 add the fail-closed
destructive selection gate used by X01–X06: legacy and broad selectors never
authorize that class, and readonly mode suppresses it even when both gates are
configured.

### Slides filesystem exclusion (E06)

The typed Slides creation tool is `slides_create_from_template`. Its closed
schema permits only `template_id`, `title`, inline repeated `replacements`,
optional `parent`, and `exact`. Each replacement is emitted as a literal
`--replace KEY=VALUE` pair in the exact child argv; there is no
`--replacements` JSON file input.

No Slides MCP schema accepts Markdown/source paths, local paths, stdin, `@file`
references, output files, or generic argv/command fields. The CLI-only
`slides create-from-markdown` command is intentionally not represented as an
MCP tool. It remains absent under exact, Slides service, wildcard, read/write,
destructive, `*`, and `all` selectors; selector broadening cannot introduce a
filesystem-backed Markdown creation surface.
