---
title: MCP server
description: "Expose typed, allowlisted gog tools to MCP clients without a generic command runner."
---

# MCP server

`gog mcp` runs a Model Context Protocol server over stdio. It is for agent
clients that need Google Workspace tools but should not receive a generic shell
or arbitrary `gog` command bridge.

The server registers a small set of typed tools such as `gmail_search`,
`docs_get`, and `sheets_read_range`. Each tool has a fixed schema, maps to one
specific `gog` operation, and returns a structured result containing the tool
name, service, risk level, exit code, parsed stdout, and stderr.

## Quick start

Start a read-only server for one account:

```bash
gog --account you@example.com mcp
```

List the tools this server would expose and exit:

```bash
gog --account you@example.com mcp --list-tools
```

Limit the server to Gmail search and Docs reads:

```bash
gog --account you@example.com mcp \
  --allow-tool gmail_search,docs_get
```

Expose Docs read/write tools:

```bash
gog --account you@example.com mcp \
  --allow-write \
  --allow-tool 'docs.*'
```

Ordinary write authorization is always required for ordinary Write and
Destructive tools. At runtime this is `--allow-write`; a persistent MCP policy
can supply the equivalent `allow_write: true`. A runtime Write tool that
matches `--allow-tool` is still hidden until write authorization is present. A
Destructive tool has a second gate: `--allow-tool` must explicitly select the
literal `destructive` risk class or that tool's exact name. Service,
service-wildcard, `write`, `*`, and `all` selectors never authorize a
Destructive tool.

The same two gates apply to a persistent MCP policy: `allow_write: true` plus
an explicit `destructive` or exact-tool selector. Runtime flags can only reduce
that configured surface.

## Why this is not `gog_exec`

MCP clients are often LLM-driven. A generic "run this command" tool would expose
every current and future CLI behavior through one broad capability, including
commands that were not reviewed for MCP use.

`gog mcp` uses a narrower contract:

- no generic command execution tool
- no model-supplied argv passthrough
- fixed tool schemas validated before command execution, including required
  fields, types, and rejection of unknown fields
- read-only tools by default
- write tools require explicit server startup flags
- existing `gog` account, auth, dry-run, no-input, and command safety flags are
  preserved

This keeps MCP useful for agents while making the permission surface visible at
server startup.

## Risk annotations

MCP tool metadata distinguishes three risk classes:

| Risk | `readOnlyHint` | `destructiveHint` | Current exposure |
| --- | ---: | ---: | --- |
| Read | `true` | `false` | Registered by default |
| Write | `false` | `false` | Requires ordinary write authorization |
| Destructive | `false` | `true` | Requires ordinary write authorization plus an explicit destructive selector; no destructive domain tools are registered yet |

All three classes retain `openWorldHint=true` because the tools interact with
Google services. R01 supplied the risk classification and annotations; R02 now
adds the fail-closed destructive authorization gate without registering a
destructive domain tool.

## Tool selection

By default, all read tools are registered and write tools are hidden.

Use `--allow-tool` to narrow the registered set. Values can be comma-separated
or repeated:

```bash
gog mcp --allow-tool gmail_search --allow-tool docs_get
gog mcp --allow-tool gmail_search,docs_get
```

Accepted selectors:

| Selector | Meaning |
| --- | --- |
| `gmail_search` | One exact tool |
| `gmail` | All Gmail Read/Write tools allowed by risk mode |
| `gmail.*` | All Gmail Read/Write tools allowed by risk mode |
| `read` | All Read tools |
| `write` | All ordinary Write tools, only when `--allow-write` is also set |
| `destructive` | Explicitly select the Destructive risk class; `--allow-write` is also required |
| `*` or `all` | All Read/Write tools allowed by risk mode; never Destructive tools |
| exact destructive tool name | Explicitly select that one Destructive tool; `--allow-write` is also required |

Broad selectors are intentionally future-expanding for ordinary Read/Write
tools. Matching is evaluated against the registry at server startup: an
existing `gmail`, `gmail.*`, service, `read`, `write`, `*`, or `all` policy
includes newly registered tools in its selected ordinary class after an
upgrade (subject to the same risk-mode and `--allow-write` checks). Destructive
tools are excluded from those legacy and broad selectors. Only the literal
`destructive` selector or an exact destructive tool name opts in to that class.


Examples:

```bash
# Read-only Gmail tools.
gog mcp --allow-tool gmail

# Only Docs tools, including writes.
gog mcp --allow-write --allow-tool 'docs.*'

# Read-only server, but only Calendar and Sheets reads.
gog mcp --allow-tool calendar,sheets

# All current write tools. Read tools are not included unless also selected.
gog mcp --allow-write --allow-tool write
```

## Persistent capability policy

For several MCP clients or accounts, put the maximum registered tool surface in
`config.json` instead of duplicating capability arguments. Without an `mcp`
block, behavior is unchanged: all read tools are available, writes require
`--allow-write`, and `--allow-tool` filters the result.

```json5
{
  "mcp": {
    "allow_tools": ["read"],
    "allow_write": false,
    "accounts": {
      "personal@example.com": {
        "allow_tools": ["read", "docs.*", "calendar.*"],
        "allow_write": true
      },
      "work@example.com": {
        "allow_tools": ["read"],
        "allow_write": false
      }
    }
  }
}
```

An account entry is a complete replacement for the global policy, not a partial
merge. Account keys are matched case-insensitively after aliases and automatic
account selection are resolved, then that resolved account is pinned for every
MCP child command. Per-account policies require stored account credentials;
direct access tokens and ADC can use only the global policy because an account
label does not prove the authenticated principal. An omitted `allow_tools` value defaults to
`["read"]`; an explicitly empty list is rejected. `allow_write: true` requires
an explicit tool list so a typo cannot accidentally expose every ordinary write
tool.

Destructive tools require both `allow_write: true` and an explicit
`allow_tools` selector: use the literal `"destructive"` or one exact destructive
tool name. Existing `write`, service, service-wildcard, `"*"`, and `"all"`
selectors never acquire destructive tools after an upgrade. Readonly mode still
suppresses both ordinary writes and destructive tools.

The configured policy is a ceiling. `--allow-tool` can intersect it with a
smaller runtime set, `--readonly` removes all writes, and `--allow-write` cannot
widen a read-only policy. Baked safety profiles remain the outer immutable
ceiling. Unknown configured selectors and attempted write widening fail before
the MCP server starts. Use `gog mcp --list-tools` with the same account and flags
to inspect the final registered surface.

### Policy migration compatibility matrix

The matrix below is the upgrade contract. `Read`, `Write`, and `Destructive`
columns describe whether a matching tool would appear in `tools/list`; `—`
means hidden. The fixture names make the binary cases concrete:
`calendar_events` is Read, `calendar_update_event` is an ordinary Write, and
`calendar_delete_event` represents a future Destructive tool. The last fixture
is not registered in the current server, so the destructive rows are tested
without adding a domain tool.

| Policy/runtime context | Selector | Authorization or mode | Read | Write | Destructive |
| --- | --- | --- | ---: | ---: | ---: |
| Legacy runtime (no `mcp` block) | omitted | no `--allow-write` | yes | — | — |
| Legacy runtime | omitted | `--allow-write` | yes | yes | — |
| Legacy runtime | exact `calendar_events` | no `--allow-write` | yes | — | — |
| Legacy runtime | exact `calendar_update_event` | `--allow-write` | — | yes | — |
| Legacy runtime | service `calendar` | `--allow-write` | yes | yes | — |
| Legacy runtime | service wildcard `calendar.*` | `--allow-write` | yes | yes | — |
| Legacy runtime | risk `read` | `--allow-write` | yes | — | — |
| Legacy runtime | risk `write` | `--allow-write` | — | yes | — |
| Legacy runtime | `all` | `--allow-write` | yes | yes | — |
| Legacy runtime | `*` | `--allow-write` | yes | yes | — |
| Legacy runtime | risk `destructive` | `--allow-write` | — | — | yes |
| Legacy runtime | exact `calendar_delete_event` | `--allow-write` | — | — | yes |
| Legacy runtime | risk `destructive` | no `--allow-write` | — | — | — |
| Legacy runtime | unknown `future_tool` | `--allow-write` | — | — | — |
| Legacy runtime | empty runtime values (`""`, commas) | `--allow-write` | yes | yes | — |
| Global policy | omitted `allow_tools` | default policy | yes | — | — |
| Global policy | service wildcard `calendar.*` | `allow_write: true` | yes | yes | — |
| Global policy | risk `destructive` | `allow_write: true` | — | — | yes |
| Per-account replacement | account selects `read` | selected account | yes | — | — |
| Per-account replacement | account selects `destructive` | `allow_write: true` | — | — | yes |
| Readonly runtime | global `all` | `--readonly` | yes | — | — |
| Runtime narrowing | global `all`, exact `calendar_events` | runtime exact selector | yes | — | — |
| Runtime narrowing | global `destructive`, `destructive` | runtime `destructive` | — | — | yes |
| Runtime narrowing | global `all`, unknown `future_tool` | unknown runtime selector | — | — | — |
| Invalid persistent policy | empty `allow_tools: []` | startup validation | error | error | error |
| Invalid persistent policy | unknown selector | startup validation | error | error | error |
| Invalid persistent policy | `allow_write: true` without list | startup validation | error | error | error |

An empty runtime selector list is the legacy “no runtime filter” behavior; an
explicitly empty persistent `allow_tools` list is instead rejected. Unknown
runtime selectors fail closed to an empty registered surface, while unknown
configured selectors fail before the server starts. Per-account entries replace
the global policy rather than merge with it, and a selected account's policy is
validated even when it narrows the global surface.

The upgrade distinction is intentional. A newly registered ordinary Write tool
is included by an existing matching `write`, service, service-wildcard, `*`, or
`all` selector (ordinary-write widening), subject to `allow_write` and
`--readonly`. A newly registered Destructive tool is **not** included by any of
those legacy or broad selectors (destructive non-widening): it requires ordinary
write authorization **and** either the literal `destructive` selector or its
exact tool name. Runtime `--allow-tool` can only narrow a persistent ceiling;
runtime `--allow-write` cannot widen one, and `--readonly` suppresses both
ordinary Write and Destructive tools.


## Initial tools

Read tools:

| Tool | Purpose |
| --- | --- |
| `gmail_search` | Search Gmail messages with Gmail query syntax. |
| `gmail_get_message` | Read one Gmail message by ID. Sanitized content is on by default. |
| `gmail_get_thread` | Read one Gmail thread by ID. Sanitized content is on by default. |
| `gmail_list_labels` | List Gmail labels. |
| `gmail_list_drafts` | List Gmail drafts with bounded paging. |
| `gmail_get_draft` | Read one Gmail draft by ID without downloading attachment bytes. |
| `calendar_events` | List Calendar events. |
| `calendar_list_calendars` | List Calendar calendars with bounded paging. |
| `calendar_search_events` | Search Calendar events over a bounded time window. |
| `calendar_get_event` | Read one Calendar event by ID. |
| `calendar_freebusy` | Read free/busy blocks for selected calendars. |
| `calendar_find_conflicts` | Find overlapping busy blocks across calendars. |
| `drive_search` | Search Drive files by text or Drive query language. |
| `drive_get` | Read Drive file metadata by ID. |
| `drive_list_folder` | List files in a Drive folder with bounded paging and explicit shared-drive inclusion. |
| `drive_download` | Download one Drive file or supported Workspace export as bounded inline base64 content; the raw payload is capped at 65,536 bytes and no host file is created. |
| `drive_permissions` | List permissions on one Drive file with bounded paging. |
| `docs_get` | Read a Google Doc as wrapped text, optionally one tab or all tabs. |
| `sheets_read_range` | Read values from a Sheets range, optionally selecting the `ROWS` or `COLUMNS` major dimension. |

Write tools, hidden unless `--allow-write`:

| Tool | Purpose |
| --- | --- |
| `gmail_create_draft` | Create an inline Gmail draft; never sends mail. |
| `gmail_update_draft` | Rebuild an inline Gmail draft; omitted `to` is preserved, omitted `cc`/`bcc` are cleared unless reply-all derives them, attachments and reply lineage are preserved, and the draft is never sent. |
| `gmail_modify_message_labels` | Add or remove labels on one message by ID. |
| `gmail_modify_thread_labels` | Add or remove labels on one thread by ID. |
| `gmail_archive_messages` | Archive explicit Gmail messages by removing `INBOX`. |
| `gmail_archive_threads` | Archive explicit Gmail threads by removing `INBOX`. |
| `gmail_mark_messages_read` | Remove `UNREAD` from explicit messages. |
| `gmail_mark_messages_unread` | Add `UNREAD` to explicit messages. |
| `calendar_create_event` | Create an ordinary Calendar event with notifications defaulting to none. |
| `calendar_update_event` | Partially update an ordinary Calendar event by explicit calendar and event ID. |
| `calendar_respond_to_event` | Respond to a Calendar invitation without exposing notification controls. |
| `calendar_move_event` | Move an event and make the destination calendar its organizer. |
| `calendar_create_calendar` | Create a secondary Calendar; record its returned calendar ID, then use the out-of-band cleanup sequence below. |
| `calendar_subscribe` | Subscribe to a raw Calendar ID. |
| `calendar_unsubscribe` | Unsubscribe from a Calendar; `--force` is server-controlled. |
| `calendar_focus_time` | Create a Focus Time event. |
| `calendar_out_of_office` | Create an out-of-office event using RFC3339 datetimes. |
| `calendar_working_location` | Create a working-location event using date-only bounds. |
| `drive_create_folder` | Create a Drive folder. |
| `drive_rename` | Rename a Drive file or folder. |
| `drive_move` | Move a Drive file, replacing its existing parents. |
| `drive_copy` | Copy a Drive file to a new name; source metadata is pre-read, and folder copies are shallow (no descendants). |
| `drive_create_shortcut` | Create a Drive shortcut. |
| `drive_create_comment` | Create an inline Drive comment. |
| `docs_create` | Create an empty Doc; compose with `docs_write`. If the post-create pageless update fails, the created Doc remains and may require cleanup. |
| `docs_write` | Append or replace Google Docs text, optionally as Markdown. |
| `sheets_create` | Create a spreadsheet with optional tabs and parent. |
| `sheets_update_range` | Update values in a Sheets range from a literal JSON 2D array. |
| `slides_create_from_template` | Copy a Slides template and apply inline replacements. |

The Gmail outbound write contract is drafts-only. `gmail_create_draft` and
`gmail_update_draft` create or rebuild drafts and never send mail. The MCP
registry intentionally has no tool for sending, draft-send (`post`), reply or
reply-all (`replyall`), forward (`fwd`), autoreply, or permanent Gmail message
deletion (`gmail batch delete`);
none of those operations can be enabled by an exact, service, wildcard, risk,
or `all` selector.

`gmail_update_draft` rebuilds the full MIME message through `gmail drafts update`;
it does not patch individual headers or MIME parts. Omitted `to` preserves the
existing To recipients. Callers must re-supply `cc` and `bcc` to retain them:
omitting either clears that header unless `reply_all` derives recipients from
the replied-to message. A plain body alone produces a plain-text body, an HTML
body alone produces an HTML body, and supplying both produces a multipart
alternative body. Existing attachments and reply lineage are preserved. Send,
file, attachment-change, `from`, quote, clear-context, path, and generic argv
inputs are not exposed.

`calendar_update_event` is a partial update: omitted fields remain unchanged.
Empty `summary`, `description`, `location`, `attendees`, `rrule`, and
`reminders` values clear their corresponding fields; an empty `event_color`
clears the event color. Supplied `false` guest-permission booleans disable those
permissions. Empty `scope` or `send_updates` values, and `all_day` or timezone
values without their required paired `start`/`end` fields, are rejected.
Recurring updates accept `scope` (`single`, `future`, or `all`) and
`original_start` when required. `send_updates` is always passed explicitly and
defaults to `none`; integrations, attachments, and specialized event types are
not exposed.

### Gmail mutation lockout (E02)

Gmail mutation tools never accept `query` or `max`; a mutation call cannot
expand a Gmail search and write in one step. The required workflow is
**search → inspect → mutate explicit IDs**:

1. Call `gmail_search` with a narrow query and bounded `max`.
2. Inspect the returned message IDs and summaries, then use
   `gmail_get_message` or `gmail_get_thread` when the target needs
   confirmation.
3. Call the appropriate explicit-ID mutation: label and read-state tools use
   message or thread IDs, while archive tools accept bounded arrays of
   explicit IDs.

Drafts use the same explicit-resource boundary: use `gmail_list_drafts` and
`gmail_get_draft` to locate and inspect a draft, then call `gmail_update_draft`
with its required `draft_id`. `gmail_update_draft` rebuilds the full MIME
message from inline `body` or `body_html`; omitted `to` is preserved, omitted
`cc`/`bcc` are cleared unless reply-all derives them, existing attachments and
reply lineage are preserved, and the draft is never sent.
`gmail_create_draft` creates a new inline draft rather than mutating a query
result. Neither draft tool exposes `query` or `max`.

`calendar_search_events` defaults to 10 results per MCP call, independently of
the CLI's direct-use default. `drive_create_folder` is non-idempotent: Drive
allows duplicate folder names, and the adapter performs no existence check.

### Wave B registry snapshot

The current registry contains **48 typed tools: 19 Read, 29 ordinary Write,
and 0 Destructive**. The inventory is:

- `M01`–`M13`: cross-domain typed adapter hardening, including the concrete-range
  Sheets check, shared-drive search scoping, calendar event paging, and the
  Sheets dimension enum.
- `G01`–`G11`: Gmail list, draft, label, archive, read-state, and inline
  full-message draft-update tools. Gmail send is a separate excluded surface.
- `C01`–`C15`: Calendar listing, search, event, free/busy, conflict, event
  lifecycle, and ordinary partial-update tools. Calendar event and whole-calendar
  deletion are separate excluded surfaces.
- `V01`–`V11`: Drive, Docs, Sheets, and Slides typed tools.
- `R01`–`R02`: risk annotations and persistent/runtime policy ceilings; R02
  suppresses all ordinary writes and destructive tools in runtime readonly mode.
- `B01`–`B04`: bounded binary transport decision, CLI raw-content cap,
  reusable inline binary encoder, and the bounded Drive download Read tool.
- `E03`: explicit Drive and local-transport exclusions described below.

Selectors expose `drive_download` only as the read-only bounded transport
described below. Gmail send and Calendar deletion remain separate excluded
surfaces.


### Bounds and defaults

These are the provider-work and payload bounds exposed by the current schemas.
Values outside numeric limits are clamped by the adapter where noted, and
unknown fields are rejected by the closed schema.

| Tool or family | Bound or default |
| --- | --- |
| `gmail_list_drafts` | `max` is 1–100 (default 20); `page_token` requests one next page. |
| `gmail_update_draft` | Requires `draft_id` and at least one inline `body`/`body_html`; subject is required unless a reply target is supplied. |
| Gmail explicit-ID archive/read-state tools | One call accepts 1–1,000 IDs; thread/label handlers can preserve per-item success/error records. |
| `calendar_events` | `max` is 1–250 (default 10); `days` is 0–31 (default 0); a nonempty `page_token` is forwarded unchanged as one `--page=<token>` argv flag (including leading dashes), while `all_pages` fetches bounded pages and is mutually exclusive with `page_token`; multi-calendar selectors (`cal`, `calendars`, `all`), event-type filters (`event_types`), sorting/order (`sort`, `order`), and field masks (`fields`) are not exposed. |
| `calendar_update_event` | `rrule` is capped at 100 and `reminders` at 5; omitted fields are unchanged, empty supported values clear fields, and `send_updates` defaults to `none`. |
| `calendar_search_events` | `max` is 1–250 (default 10). |
| `calendar_freebusy` / `calendar_find_conflicts` | Calendar-ID arrays are capped at 100. Conflicts require at least two IDs unless `all=true`; `days` is 0–31 (default 0). |
| `calendar_create_event` | `attendees` and `rrule` are capped at 100; `reminders` at 5; `all_day` defaults false and `send_updates` defaults `none`. |
| `calendar_focus_time` | `rrule` is capped at 100; defaults are calendar `primary`, summary `Focus Time`, `auto_decline=all`, and `chat_status=doNotDisturb`. |
| `calendar_out_of_office` / `calendar_working_location` | Both default to calendar `primary`; out-of-office defaults summary `Out of office` and `auto_decline=all`; working-location bounds are date-only. |
| `calendar_subscribe` | `color_id`, when supplied, is 1–24; `hidden` defaults false and `selected` true. |
| `drive_search` | `max` is 1–100 (default 20); optional `drive_id` scopes the search to one shared drive. |
| `drive_download` | Read-only inline binary result; `file_id` is required, `format` is optional and limited to `pdf`, `csv`, `xlsx`, `pptx`, `txt`, `png`, `docx`, `md`, or `html`; raw content is capped at 65,536 bytes. |
| `drive_list_folder` | `max` is 1–100 (default 20), with `include_shared_drives` defaulting true; `page_token` is one next page. |
| `drive_permissions` | `max` is 1–100 (default 100); `page_token` is one next page. |
| `docs_get` | `max_bytes` is 0–20,000,000 (default 2,000,000); 0 preserves the CLI unlimited value. `tab` and `all_tabs` are mutually exclusive. |
| `docs_write` | `append` defaults true, `replace` false, and `markdown` false. Explicit `append=false` requires `replace=true`. |
| `sheets_create` / `slides_create_from_template` | `sheet_names` and template `replacements` are each capped at 100; replacements require at least one `key=value`. |
| `sheets_read_range` | Optional `dimension` is `ROWS` or `COLUMNS`; omitted dimension and render preserve the CLI/API defaults. |
| `sheets_update_range` | `input` defaults `USER_ENTERED`; `values_json` is literal JSON only and obeys the concrete-A1 rules below. |

`calendar_search_events` and the other paged tools return bounded pages rather
than silently fetching an unbounded result set. `drive_create_folder` remains
non-idempotent: Drive permits duplicate names and the adapter does not check
for an existing folder.


### Semantic compatibility notes

- `calendar_find_conflicts` (`C05`) preserves the CLI's deduplicated
  pairwise-overlap output in the CLI's detection order (its map/pairwise
  traversal). Results are not globally sorted by start, end, or calendar;
  callers must not assume global sorting.
- `calendar_create_calendar` (`C10`) requires out-of-band cleanup because
  whole-calendar deletion is not an MCP tool. The exact sequence is: create
  the secondary calendar, record the returned calendar ID, then run this
  command outside MCP:

  ```bash
  gog calendar delete-calendar CALENDAR_ID
  ```

- `drive_copy` (`V06`) pre-reads source metadata before issuing the copy. If
  the source pre-read fails, no copy is attempted. Folder copies are shallow:
  only the folder itself is copied, not its descendants.

### Partial-failure hazards

MCP does not add a transaction around a child `gog` command. Every call keeps
the standard structured envelope (`exit_code`, parsed `stdout`, and `stderr`);
a non-zero exit marks the result as an error but does not roll back provider
effects. In particular:

- Gmail explicit-ID operations can return per-item success/error records. The
  MCP schema caps each explicit-ID request at 1,000, so one MCP call does not
  span multiple `BatchModify` chunks; thread handlers may still apply earlier
  IDs before a later ID fails. The direct CLI may process multiple 1,000-ID
  chunks, but MCP does not add rollback. Inspect the structured result and
  `exit_code`; do not infer that a failed call changed nothing.
- `docs_create` creates the Doc before applying a requested pageless update.
  If that second step fails, the Doc remains and may need caller cleanup.
- `sheets_create` creates the spreadsheet first. A requested parent move is
  advisory: a failed move leaves the spreadsheet in Drive root, reports
  `movedToParent=false` and `moveError` in JSON, and writes the warning to
  `stderr` while the create remains successful.
- `slides_create_from_template` copies the presentation before applying
  replacements. Replacement failure is non-atomic, returns a non-zero result,
  and reports the created presentation ID on `stderr` for manual cleanup.

### Drive exclusions (E03)

The Drive registry contains metadata, search, folder listing, permissions,
the bounded `drive_download` Read tool, and the bounded ordinary Write tools
shown above. No exact, service, risk, wildcard, or `all` selector can expose
Drive **permanent delete, upload, share, or unshare**: those tools are not
registered.

No Drive schema accepts `--permanent`, a host filesystem path, `out`,
`overwrite`, raw stdout, stdin (`-` or `@-`), `@file`, or generic argv
expansion. `drive_download` has only `file_id` and optional supported `format`;
the adapter supplies `--max-bytes 65536` and `--out -` internally to a
dedicated child capture. That fixed `--out -` is never model-controlled, is
not returned as a path, and cannot be selected or redirected by a caller.
Upload remains excluded pending a separately approved bounded-content design.

### Calendar integration and specialized-field exclusions (E05)

`calendar_create_event` and `calendar_update_event` are deliberately ordinary
event tools. Their closed schemas and generated child argv do not expose
Google Meet or Zoom controls (`with_meet`, `regenerate_meet`, `with_zoom`,
`regenerate_zoom`, or `remove_zoom`), Zoom password controls, or any external
Zoom credentials. They also do not expose Places lookup, place-ID,
language/region, Places API-key, or other credential inputs.

The ordinary tools do not accept password-bearing output controls, attachments,
source URL/title fields, or private/shared/other extended properties. Their
argv is limited to the ordinary event fields documented above; integration,
attachment, source, and extended-property flags are never synthesized. Calendar
event output keeps the existing default Zoom redaction (`pwd=REDACTED` and
`Passcode: REDACTED`); the CLI's password-inclusion flag is not an MCP input.

Focus Time, out-of-office, and working-location events remain separate
dedicated tools with their own fixed schemas. Their registration does not widen
the ordinary create/update schemas, and no generic Calendar integration tool is
registered. Exact, `calendar`, `calendar.*`, `write`, `*`, and `all` selectors
therefore cannot expose Meet, Zoom, Places, credential, or specialized-event
surfaces through an ordinary event call.


The generated command reference for the server itself is
[`gog mcp`](commands/gog-mcp.md).

MCP clients discover the registered surface through the protocol's standard
`tools/list` request. For shell-side inspection before starting the server, use
`gog mcp --list-tools`; no model-callable discovery tool is added.

## Client configuration

MCP clients usually need a command and an argument list. Put account selection
and safety policy on the server command, not inside tool calls.

Minimal stdio configuration:

```json
{
  "command": "gog",
  "args": ["--account", "you@example.com", "mcp"]
}
```

Read-only Docs and Sheets configuration:

```json
{
  "command": "gog",
  "args": [
    "--account", "you@example.com",
    "--enable-commands-exact", "mcp,docs.cat,sheets.get",
    "mcp",
    "--allow-tool", "docs_get,sheets_read_range"
  ]
}
```

Docs read/write configuration:

```json
{
  "command": "gog",
  "args": [
    "--account", "you@example.com",
    "--enable-commands-exact", "mcp,docs.cat,docs.write",
    "--no-input",
    "mcp",
    "--allow-write",
    "--allow-tool", "docs.*"
  ]
}
```

For headless services, set `GOG_KEYRING_BACKEND=file` and
`GOG_KEYRING_PASSWORD` on the MCP client process or service unit. A successful
interactive shell check does not prove the MCP client inherited those
variables; verify through the same process manager that launches the server.

## mcporter examples

List registered tools and their schemas:

```bash
mcporter list \
  --stdio gog \
  --stdio-arg --account \
  --stdio-arg you@example.com \
  --stdio-arg mcp \
  --stdio-arg --allow-tool \
  --stdio-arg 'docs.*' \
  --schema \
  --json
```

Dry-run a Docs write through MCP:

```bash
mcporter call \
  --stdio gog \
  --stdio-arg --account \
  --stdio-arg you@example.com \
  --stdio-arg --dry-run \
  --stdio-arg mcp \
  --stdio-arg --allow-write \
  --stdio-arg --allow-tool \
  --stdio-arg docs_write \
  docs_write \
  '{"document_id":"DOCUMENT_ID","text":"MCP smoke test\n","append":true}'
```

Read a Sheet range:

```bash
mcporter call \
  --stdio gog \
  --stdio-arg --account \
  --stdio-arg you@example.com \
  --stdio-arg mcp \
  --stdio-arg --allow-tool \
  --stdio-arg sheets_read_range \
  sheets_read_range \
  '{"spreadsheet_id":"SPREADSHEET_ID","range":"Sheet1!A1:C10","dimension":"ROWS"}'
```

Update a Sheet range:

```bash
mcporter call \
  --stdio gog \
  --stdio-arg --account \
  --stdio-arg you@example.com \
  --stdio-arg mcp \
  --stdio-arg --allow-write \
  --stdio-arg --allow-tool \
  --stdio-arg sheets_update_range \
  sheets_update_range \
  '{"spreadsheet_id":"SPREADSHEET_ID","range":"Sheet1!A1:B1","values_json":"[[\"status\",\"ok\"]]","input":"RAW"}'
```

`sheets_update_range.values_json` must be a literal JSON 2D array. MCP decodes
it once with strict trailing-content rejection and number preservation. For a
fully concrete A1 range with both row and column endpoints (for example,
`A1:B2`), a matrix exceeding the requested row count or any row's column count
is rejected before the child command; an exact-fit or smaller matrix remains
valid. Named ranges and open-ended or partially bounded ranges such as `A:B`,
`1:2`, or `A1:B` retain strict literal-JSON validation but do not receive an
offline dimension check because their size is resolved by Sheets. MCP rejects
`@file`, `@-`, and `-` expansion forms so a model cannot cause the server
process to read arbitrary local files or stdin.

## Safety model

Tool calls run as subprocesses of the same `gog` executable. The server adds a
non-interactive, agent-oriented root context to every child command:

- `--json`
- `--wrap-untrusted`
- `--no-input`
- `--color=never`

The server also preserves selected parent root flags:

- `--account`
- `--client`
- `--home`
- `--dry-run`
- `--results-only`
- `--select`
- direct access tokens

And it preserves command safety flags:

- `--gmail-no-send`
- `--enable-commands`
- `--enable-commands-exact`
- `--disable-commands`

Use both MCP tool allowlists and command allowlists when the server is exposed
to an untrusted or semi-trusted agent:

```bash
gog --account you@example.com \
  --enable-commands-exact mcp,docs.cat,docs.write \
  --disable-commands gmail.send,gmail.drafts.send \
  --gmail-no-send \
  mcp \
  --allow-write \
  --allow-tool 'docs.*'
```

If a tool maps to a disabled command, the tool call returns a non-zero exit code
and the child command error in `stderr`.

## Output shape

Successful calls return structured MCP content shaped like:

```json
{
  "tool": "docs_get",
  "service": "docs",
  "risk": "read",
  "exit_code": 0,
  "stdout": {
    "documentId": "..."
  },
  "stderr": ""
}
```

If a child command prints valid JSON, `stdout` is parsed as JSON with numeric
literals preserved. Otherwise `stdout` is returned as a string. Empty stdout is
omitted.

If the child command exits non-zero, the MCP result is marked as an error and
includes the same structured fields with `exit_code` and `stderr`.

## Limits and timeouts

Each tool call has a subprocess timeout and bounded stdout/stderr capture:

```bash
gog mcp --timeout-seconds 30 --max-output-bytes 262144
```

Defaults:

- timeout: 60 seconds
- max captured stdout/stderr: 102400 bytes each

Use command-specific limits too. For example, `docs_get` has a `max_bytes`
argument, and search tools have `max` arguments.

## Bounded binary output (B01 decision)

`drive_download` now wires the B04 bounded Read adapter to the reusable B03
encoder. The recorded transport is **inline standard base64 in the `tools/call`
structured result**. It is not an MCP resource and it never returns a host
path.

This choice is based on the pinned `mark3labs/mcp-go` v0.57.0 contract. Its
stdio transport carries one newline-delimited JSON-RPC message at a time, and
its client can decode a `tools/call` result's `StructuredContent` (with the
existing JSON text fallback). The library also has `ReadResource` and
`BlobResourceContents`, but using them would require advertising resource
capabilities and making a second `resources/read` request. This decision keeps
direct stdio and the intended LiteLLM route on the same single tool-call
contract; neither path needs resource discovery, a callback, or an HTTP URL.

The successful structured content value in the standard MCP result envelope is:

```json
{
  "name": "report.pdf",
  "mimeType": "application/pdf",
  "size": 1234,
  "contentBase64": "..."
}
```

Metadata and encoding are normative:

- `contentBase64` is padded RFC 4648 standard base64 (not base64url) of the
  exact bytes represented by `size`.
- `size` is the decoded byte count, including zero, and must agree with the
  decoded content.
- `mimeType` is the non-empty, syntactically valid MIME type selected by the
  existing Drive/export command. Invalid or missing MIME metadata is an error;
  the adapter does not guess by sniffing bytes.
- `name` is a logical display name, never a path. It is derived from the
  remote display name and selected export extension, reduced to the final
  component after both `/` and `\`; empty, `.`, and `..` become `download`.
  It is capped at 255 UTF-8 bytes and is never joined with a directory or
  passed to `--out`.

### Limits and failure behavior

The binary transport has a hard **65,536-byte (64 KiB) raw-content limit per
call**, inclusive. B02 adds the shared CLI `--max-bytes` cap with the same
raw-byte counting and failure semantics; B04 supplies this limit as a
server-controlled fixed argument and does not expose a `max_bytes` input.
`file_id` and optional supported export `format` are the only B04
model-controlled inputs.

For each call B04 first invokes the typed `drive get --fields id,name,mimeType`
child, then invokes `drive download` with fixed `--max-bytes 65536 --out -`
for server-side capture. The raw child is the only place that omits the
inherited `--json` flag; the model cannot supply or alter either fixed flag.
`tab`, `out`, `overwrite`, raw stdout, host paths, stdin, `@file`, and generic
argv are absent from the schema.

Exactly 65,536 bytes succeeds. A payload over the limit fails closed with a
non-zero result and an error object such as
`{"error":"binary_size_limit","limit_bytes":65536}`; it never returns a
partial base64 string or a truncation sentinel. For the CLI file-output path,
an over-limit result removes any temporary partial output, leaves an existing
destination unchanged, and preserves the normal no-clobber default.

The normal MCP `--max-output-bytes` setting remains an independent cap on the
child's stdout and stderr. The B03 encoder checks the encoded binary object
against that configured cap before constructing the result. If the encoded
envelope cannot fit, the call fails without a partial `contentBase64`; it must
not silently truncate or emit invalid JSON. The 64 KiB raw ceiling is chosen to
fit the default 102,400-byte stdout cap after base64 and bounded metadata
overhead.

### Lifetime, reachability, and collisions

The bytes live only in the completed `tools/call` response. The server keeps no
resource URI, temp file, cache entry, or replay handle; after the response is
sent (or the call is canceled, times out, or the client disconnects), the
bytes are discarded. A client that needs persistence must copy the decoded
bytes itself. `mcp-go/client.CallTool` can read the structured object directly,
and clients limited to the text fallback receive the same JSON object; no
second request is required.

There is no MCP-side overwrite or collision: repeated calls for the same Drive
ID and name produce independent inline results and cannot clobber host data.
`--overwrite` remains a CLI-only control for direct file downloads and is not
part of the B04 schema or argv.

B02–B04 must test this contract without a live provider: raw payloads at
limit−1, limit, and limit+1; exact base64 decode and MIME/name/size metadata;
invalid MIME; output-cap overflow without malformed JSON; timeout or
client-disconnect cleanup; repeated-call collision absence; and B04 schema
rejection of every path, overwrite, raw-stdout, resource, and generic-argv
escape.

B03 supplies the reusable inline encoder used by B04. The encoder and adapter
create no resource URI, temporary file, cache, or replay handle; their bytes
exist only in the completed `tools/call` response.

## Authentication

The MCP server uses normal `gog` auth. Before wiring a client, verify the same
account and scopes from a shell:

```bash
gog --account you@example.com auth doctor --check
gog --account you@example.com mcp --list-tools
```

Then verify through the MCP client entrypoint. In services and desktop MCP
clients, most auth failures are environment inheritance problems: missing
`GOG_ACCOUNT`, missing file-keyring password, different `GOG_HOME`, or a
different OAuth client selected by `--client`.

## Troubleshooting

`no MCP tools enabled`

: Your `--allow-tool` filters excluded everything, or you selected only write
  tools without `--allow-write`.

`command "..." is disabled`

: The MCP tool was registered, but the child `gog` command was blocked by
  `--enable-commands`, `--enable-commands-exact`, `--disable-commands`, or a
  baked safety profile.

Tool missing in the client

: Run `gog mcp --list-tools` with the same flags. If the tool is not listed,
  fix `--allow-tool` or add `--allow-write` for write tools. If it is listed,
  refresh or restart the MCP client.

Auth works in Terminal but not in the MCP client

: Compare `--account`, `--client`, `--home`, `GOG_HOME`,
  `GOG_KEYRING_BACKEND`, and `GOG_KEYRING_PASSWORD` in the process that starts
  the MCP server.

Large output is truncated

: Increase `--max-output-bytes`, narrow the request, or use tool arguments such
  as `max`, `max_bytes`, date ranges, or Drive field masks.

## Slides filesystem exclusion (E06)

The registered Slides creation surface is `slides_create_from_template`, which
accepts only a template ID, title, inline repeated `key=value` replacements,
an optional parent ID, and the `exact` matching switch. Replacement content is
always carried in the MCP call and becomes repeated `--replace` arguments in
the child invocation. The generated command shape is:

```text
slides create-from-template TEMPLATE_ID TITLE [--replace KEY=VALUE ...] [--parent PARENT_ID] [--exact]
```

The MCP schema is closed and deliberately has no replacement-file,
Markdown/source/path, stdin, `@file`, output-file, or generic argv/command
fields. It does not pass `--replacements`, `--content-file`, `--mmdc`, or any
other filesystem-backed Slides input to the child CLI. `slides_create_from_markdown`
is not registered; exact, service, wildcard, risk-class, `*`, `all`, and
destructive selectors cannot expose a create-from-markdown MCP tool.
