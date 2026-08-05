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

`--allow-write` is always required for write tools. A write tool that matches
`--allow-tool` is still hidden until `--allow-write` is present.

The exception is an explicit persistent MCP policy. It can authorize a narrow
write surface without repeating `--allow-write` in every client definition;
runtime flags can only reduce that configured surface.

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
| Write | `false` | `false` | Requires the existing write authorization |
| Destructive | `false` | `true` | No destructive domain tools are registered yet |

All three classes retain `openWorldHint=true` because the tools interact with
Google services. R01 only adds the risk classification and annotations; it does
not add destructive authorization or change the existing allowlist behavior.
Explicit destructive authorization is a separate R02 change.

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
| `gmail` | All Gmail tools allowed by risk mode |
| `gmail.*` | All Gmail tools allowed by risk mode |
| `read` | All read tools |
| `write` | All write tools, only when `--allow-write` is also set |
| `*` or `all` | All tools allowed by risk mode |

Broad selectors are intentionally future-expanding. Matching is evaluated
against the registry at server startup: an existing `gmail`, `gmail.*`, a
service selector, `read`, `write`, `*`, or `all` policy will include newly
registered tools in that selected ordinary read/write class after an upgrade
(subject to the same risk-mode and `--allow-write` checks). Exact tool names
remain the stable least-privilege choice. Existing ordinary `write`, service
wildcard, `*`, and `all` policies do not acquire deferred destructive tools:
R02 must add explicit destructive-tool selection and authorization.

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
an explicit tool list so a typo cannot accidentally expose every write tool.

The configured policy is a ceiling. `--allow-tool` can intersect it with a
smaller runtime set, `--readonly` removes all writes, and `--allow-write` cannot
widen a read-only policy. Baked safety profiles remain the outer immutable
ceiling. Unknown configured selectors and attempted write widening fail before
the MCP server starts. Use `gog mcp --list-tools` with the same account and flags
to inspect the final registered surface.

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
| `drive_permissions` | List permissions on one Drive file with bounded paging. |
| `docs_get` | Read a Google Doc as wrapped text, optionally one tab or all tabs. |
| `sheets_read_range` | Read values from a Sheets range. |

Write tools, hidden unless `--allow-write`:

| Tool | Purpose |
| --- | --- |
| `gmail_create_draft` | Create an inline Gmail draft; never sends mail. |
| `gmail_modify_message_labels` | Add or remove labels on one message by ID. |
| `gmail_modify_thread_labels` | Add or remove labels on one thread by ID. |
| `gmail_archive_messages` | Archive explicit Gmail messages by removing `INBOX`. |
| `gmail_archive_threads` | Archive explicit Gmail threads by removing `INBOX`. |
| `gmail_mark_messages_read` | Remove `UNREAD` from explicit messages. |
| `gmail_mark_messages_unread` | Add `UNREAD` to explicit messages. |
| `calendar_create_event` | Create an ordinary Calendar event with notifications defaulting to none. |
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

`calendar_search_events` defaults to 10 results per MCP call, independently of
the CLI's direct-use default. `drive_create_folder` is non-idempotent: Drive
allows duplicate folder names, and the adapter performs no existence check.

### Wave A registry snapshot

The current Wave A registry contains **45 typed tools: 18 Read, 27 ordinary
Write, and 0 Destructive**. The inventory is:

- `M01`–`M10`: cross-domain read/write adapter hardening, including the
  concrete-range Sheets check.
- `G01`–`G04`, `G06`–`G11`: Gmail list, draft, label, archive, and read-state
  tools. `G05` (Gmail draft update) is not registered; Gmail send is a
  separate excluded surface.
- `C01`–`C06`, `C08`–`C15`: Calendar listing, search, event, free/busy,
  conflict, and event-lifecycle tools. `C07` (Calendar event update) is not
  registered; Calendar event and whole-calendar deletion are separate
  excluded surfaces.
- `V01`–`V11`: Drive, Docs, Sheets, and Slides typed tools.
- `R01`: risk annotations only; it adds metadata and does not add destructive
  tools or destructive authorization.
- `B01`: the bounded binary transport decision in this document; it is
  specification-only until B02–B04 implement and test it.
- `E03`: explicit Drive and local-transport exclusions described below.

`G05` (Gmail draft update), `C07` (Calendar event update), `R02`, and B02–B04
remain deferred; no selector can expose them until they are registered and
separately authorized. Gmail send and Calendar deletion remain separate
excluded surfaces, not alternate identities for G05 or C07.

### Bounds and defaults

These are the provider-work and payload bounds exposed by the current schemas.
Values outside numeric limits are clamped by the adapter where noted, and
unknown fields are rejected by the closed schema.

| Tool or family | Bound or default |
| --- | --- |
| `gmail_search` | `max` is 1–100 (default 10); `include_body` defaults false. |
| `gmail_list_drafts` | `max` is 1–100 (default 20); `page_token` requests one next page. |
| Gmail explicit-ID archive/read-state tools | One call accepts 1–1,000 IDs; thread/label handlers can preserve per-item success/error records. |
| `calendar_events` | `max` is 1–250 (default 10); `days` is 0–31 (default 0); `today` and `tomorrow` default false. |
| `calendar_search_events` | `max` is 1–250 (default 10). |
| `calendar_freebusy` / `calendar_find_conflicts` | Calendar-ID arrays are capped at 100. Conflicts require at least two IDs unless `all=true`; `days` is 0–31 (default 0). |
| `calendar_create_event` | `attendees` and `rrule` are capped at 100; `reminders` at 5; `all_day` defaults false and `send_updates` defaults `none`. |
| `calendar_focus_time` | `rrule` is capped at 100; defaults are calendar `primary`, summary `Focus Time`, `auto_decline=all`, and `chat_status=doNotDisturb`. |
| `calendar_out_of_office` / `calendar_working_location` | Both default to calendar `primary`; out-of-office defaults summary `Out of office` and `auto_decline=all`; working-location bounds are date-only. |
| `calendar_subscribe` | `color_id`, when supplied, is 1–24; `hidden` defaults false and `selected` true. |
| `drive_search` | `max` is 1–100 (default 20). |
| `drive_list_folder` | `max` is 1–100 (default 20), with `include_shared_drives` defaulting true; `page_token` is one next page. |
| `drive_permissions` | `max` is 1–100 (default 100); `page_token` is one next page. |
| `docs_get` | `max_bytes` is 0–20,000,000 (default 2,000,000); 0 preserves the CLI unlimited value. `tab` and `all_tabs` are mutually exclusive. |
| `docs_write` | `append` defaults true, `replace` false, and `markdown` false. Explicit `append=false` requires `replace=true`. |
| `sheets_create` / `slides_create_from_template` | `sheet_names` and template `replacements` are each capped at 100; replacements require at least one `key=value`. |
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

The Drive registry deliberately contains metadata, search, folder listing,
permissions listing, and the bounded ordinary write tools shown above only.
No exact, service, risk, wildcard, or `all` selector can expose Drive
**permanent delete, upload, download, share, or unshare**: those tools are not
registered. No Drive schema or generated child argv accepts `--permanent`, a
host filesystem path, `--out`, stdin (`-` or `@-`), or `@file` expansion.

`drive_download` remains deferred under the B01 decision until B02–B04. B01
records the approved bounded inline-base64 transport only; it does not add a
download tool, a host path, a resource URI, stdin, `@file`, or generic argv
surface. Direct CLI Drive upload/download behavior is outside the MCP
registry and is not reachable through MCP selectors.

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
  '{"spreadsheet_id":"SPREADSHEET_ID","range":"Sheet1!A1:C10"}'
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

`drive_download` remains deferred until B02–B04. The recorded transport
decision is **inline standard base64 in the `tools/call` structured result**.
It is not an MCP resource and it never returns a host path.

This choice is based on the pinned `mark3labs/mcp-go` v0.57.0 contract. Its
stdio transport carries one newline-delimited JSON-RPC message at a time, and
its client can decode a `tools/call` result's `StructuredContent` (with the
existing JSON text fallback). The library also has `ReadResource` and
`BlobResourceContents`, but using them would require advertising resource
capabilities and making a second `resources/read` request. This decision keeps
direct stdio and the intended LiteLLM route on the same single tool-call
contract; neither path needs resource discovery, a callback, or an HTTP URL.

The successful `stdout` value inside the standard MCP result envelope is:

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
`tab`, `out`, `overwrite`, `--out -`, raw stdout, host paths, stdin, `@file`,
and generic argv are absent.

Exactly 65,536 bytes succeeds. A payload over the limit fails closed with a
non-zero result and an error object such as
`{"error":"binary_size_limit","limit_bytes":65536}`; it never returns a
partial base64 string or a truncation sentinel. For the CLI file-output path,
an over-limit result removes any temporary partial output, leaves an existing
destination unchanged, and preserves the normal no-clobber default.

The normal MCP `--max-output-bytes` setting remains an independent cap on the
child's stdout and stderr. B03 must detect capture overflow rather than parse
the existing `... [output truncated]` marker as a binary result. If the
encoded envelope cannot fit under the configured output cap, the call fails
without a partial `contentBase64`; it must not silently truncate or emit
invalid JSON. The 64 KiB raw ceiling is chosen to fit the default 102,400-byte
stdout cap after base64 and bounded metadata overhead.

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

No `drive_download` tool is registered by this decision. It records the
transport that B02–B04 must implement and does not itself implement download.

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
