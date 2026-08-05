package cmd

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/steipete/gogcli/internal/sheetsvalues"
)

func mcpAllTools() []mcpToolSpec {
	return []mcpToolSpec{
		// Read tools are kept in deterministic service order.
		mcpGmailSearchTool(),
		mcpGmailGetMessageTool(),
		mcpGmailGetThreadTool(),
		mcpGmailListLabelsTool(),
		mcpGmailListDraftsTool(),
		mcpGmailGetDraftTool(),
		mcpCalendarEventsTool(),
		mcpCalendarListCalendarsTool(),
		mcpCalendarSearchTool(),
		mcpCalendarGetEventTool(),
		mcpCalendarFreeBusyTool(),
		mcpCalendarConflictsTool(),
		mcpDriveSearchTool(),
		mcpDriveGetTool(),
		mcpDriveDownloadTool(),
		mcpDriveListFolderTool(),
		mcpDrivePermissionsTool(),
		mcpDocsGetTool(),
		mcpSheetsReadRangeTool(),

		// Write tools follow reads and stay grouped by service.
		mcpGmailCreateDraftTool(),
		mcpGmailUpdateDraftTool(),
		mcpGmailModifyMessageLabelsTool(),
		mcpGmailModifyThreadLabelsTool(),
		mcpGmailArchiveMessagesTool(),
		mcpGmailArchiveThreadsTool(),
		mcpGmailMarkMessagesReadTool(),
		mcpGmailMarkMessagesUnreadTool(),
		mcpCalendarCreateEventTool(),
		mcpCalendarUpdateTool(),
		mcpCalendarRespondTool(),
		mcpCalendarMoveTool(),
		mcpCalendarCreateCalendarTool(),
		mcpCalendarSubscribeTool(),
		mcpCalendarUnsubscribeTool(),
		mcpCalendarFocusTimeTool(),
		mcpCalendarOOOTool(),
		mcpCalendarWorkingLocationTool(),
		mcpDriveCreateFolderTool(),
		mcpDriveRenameTool(),
		mcpDriveMoveTool(),
		mcpDriveCopyTool(),
		mcpDriveCreateShortcutTool(),
		mcpDriveCreateCommentTool(),
		mcpDocsCreateTool(),
		mcpDocsWriteTool(),
		mcpSheetsCreateTool(),
		mcpSheetsUpdateRangeTool(),
		mcpSlidesCreateFromTemplateTool(),

		// Destructive tools require explicit write authorization and a destructive selector.
		mcpGmailDeleteDraftTool(),
		mcpGmailTrashMessagesTool(),
		mcpCalendarDeleteEventTool(),
		mcpDriveTrashTool(),
		mcpDriveShareUserTool(),
		mcpDriveUnshareTool(),
	}
}

func mcpGmailSearchTool() mcpToolSpec {
	return mcpToolSpec{
		Name:        "gmail_search",
		Service:     "gmail",
		Risk:        mcpRiskRead,
		Description: "Search Gmail messages with Gmail query syntax. Returns message summaries and optional sanitized bodies.",
		Options: []mcp.ToolOption{
			mcp.WithString("query", mcp.Description("Gmail search query, e.g. newer_than:7d from:person@example.com"), mcp.Required()),
			mcp.WithInteger("max", mcp.Description("Maximum results"), mcp.DefaultNumber(10), mcp.Min(1), mcp.Max(100)),
			mcp.WithBoolean("include_body", mcp.Description("Include decoded message body"), mcp.DefaultBool(false)),
		},
		BuildArgs: func(req mcp.CallToolRequest) ([]string, error) {
			query, err := requireMCPString(req, "query")
			if err != nil {
				return nil, err
			}
			args := []string{"gmail", "messages", "search", "--max", strconv.Itoa(clampMCPInt(req.GetInt("max", 10), 1, 100))}
			if req.GetBool("include_body", false) {
				args = append(args, "--include-body")
			}
			return append(args, "--", query), nil
		},
	}
}

func mcpGmailGetMessageTool() mcpToolSpec {
	return mcpToolSpec{
		Name:        "gmail_get_message",
		Service:     "gmail",
		Risk:        mcpRiskRead,
		Description: "Get one Gmail message by ID. Sanitized content is enabled by default; attachment metadata is preserved and attachment bytes are never downloaded.",
		Options: []mcp.ToolOption{
			mcp.WithString("message_id", mcp.Description("Gmail message ID"), mcp.Required()),
			mcp.WithBoolean("sanitize_content", mcp.Description("Strip URLs/HTML and omit raw payloads"), mcp.DefaultBool(true)),
		},
		BuildArgs: func(req mcp.CallToolRequest) ([]string, error) {
			messageID, err := requireMCPString(req, "message_id")
			if err != nil {
				return nil, err
			}
			args := []string{"gmail", "get"}
			if req.GetBool("sanitize_content", true) {
				args = append(args, "--sanitize-content")
			}
			return append(args, "--", messageID), nil
		},
	}
}

func mcpGmailGetThreadTool() mcpToolSpec {
	return mcpToolSpec{
		Name:        "gmail_get_thread",
		Service:     "gmail",
		Risk:        mcpRiskRead,
		Description: "Get one Gmail thread by ID. Sanitized content is enabled by default.",
		Options: []mcp.ToolOption{
			mcp.WithString("thread_id", mcp.Description("Gmail thread ID"), mcp.Required()),
			mcp.WithBoolean("sanitize_content", mcp.Description("Strip URLs/HTML and omit raw payloads"), mcp.DefaultBool(true)),
			mcp.WithBoolean("full", mcp.Description("Include full message bodies"), mcp.DefaultBool(false)),
		},
		BuildArgs: func(req mcp.CallToolRequest) ([]string, error) {
			threadID, err := requireMCPString(req, "thread_id")
			if err != nil {
				return nil, err
			}
			args := []string{"gmail", "thread", "get"}
			if req.GetBool("sanitize_content", true) {
				args = append(args, "--sanitize-content")
			}
			if req.GetBool("full", false) {
				args = append(args, "--full")
			}
			return append(args, "--", threadID), nil
		},
	}
}

func mcpDriveSearchTool() mcpToolSpec {
	return mcpToolSpec{
		Name:        "drive_search",
		Service:     "drive",
		Risk:        mcpRiskRead,
		Description: "Search Google Drive files using text search or Drive query language, optionally scoped to a shared drive.",
		Options: []mcp.ToolOption{
			mcp.WithString("query", mcp.Description("Search text or Drive query"), mcp.Required()),
			mcp.WithInteger("max", mcp.Description("Maximum results"), mcp.DefaultNumber(20), mcp.Min(1), mcp.Max(100)),
			mcp.WithBoolean("raw_query", mcp.Description("Treat query as Drive query language"), mcp.DefaultBool(false)),
			mcp.WithString("parent", mcp.Description("Optional parent folder/shared drive ID")),
			mcp.WithString("drive_id", mcp.Description("Optional shared-drive/team-drive ID to scope the search")),
		},
		BuildArgs: func(req mcp.CallToolRequest) ([]string, error) {
			query, err := requireMCPString(req, "query")
			if err != nil {
				return nil, err
			}
			args := []string{"drive", "search", "--max", strconv.Itoa(clampMCPInt(req.GetInt("max", 20), 1, 100))}
			rawQuery := req.GetBool("raw_query", false)
			if rawQuery {
				args = append(args, "--raw-query")
			}
			if parent := strings.TrimSpace(req.GetString("parent", "")); parent != "" {
				if rawQuery {
					return nil, fmt.Errorf("--parent cannot be combined with --raw-query; include the \"'<parentId>' in parents\" clause in your raw query instead")
				}
				args = append(args, "--parent", parent)
			}
			if driveID := strings.TrimSpace(req.GetString("drive_id", "")); driveID != "" {
				args = append(args, "--drive", driveID)
			}
			return append(args, "--", query), nil
		},
	}
}

func mcpDriveGetTool() mcpToolSpec {
	return mcpToolSpec{
		Name:        "drive_get",
		Service:     "drive",
		Risk:        mcpRiskRead,
		Description: "Get Google Drive file metadata by ID.",
		Options: []mcp.ToolOption{
			mcp.WithString("file_id", mcp.Description("Drive file ID"), mcp.Required()),
			mcp.WithString("fields", mcp.Description("Optional Drive API field mask")),
		},
		BuildArgs: func(req mcp.CallToolRequest) ([]string, error) {
			fileID, err := requireMCPString(req, "file_id")
			if err != nil {
				return nil, err
			}
			args := []string{"drive", "get"}
			if fields := strings.TrimSpace(req.GetString("fields", "")); fields != "" {
				args = append(args, "--fields", fields)
			}
			return append(args, "--", fileID), nil
		},
	}
}

func mcpDriveListFolderTool() mcpToolSpec {
	return mcpToolSpec{
		Name:        "drive_list_folder",
		Service:     "drive",
		Risk:        mcpRiskRead,
		Description: "List files in a Drive folder (default: root), including shared drives.",
		Options: []mcp.ToolOption{
			mcp.WithString("folder_id", mcp.Description("Folder or shared-drive ID; default root")),
			mcp.WithInteger("max", mcp.Description("Maximum results"), mcp.DefaultNumber(20), mcp.Min(1), mcp.Max(100)),
			mcp.WithString("page_token", mcp.Description("Opaque page token from a prior nextPageToken for the next page")),
			mcp.WithBoolean("include_shared_drives", mcp.Description("Include shared drives; default true"), mcp.DefaultBool(true)),
		},
		BuildArgs: func(req mcp.CallToolRequest) ([]string, error) {
			args := []string{"drive", "ls", "--max", strconv.Itoa(clampMCPInt(req.GetInt("max", 20), 1, 100))}
			if pageToken := strings.TrimSpace(req.GetString("page_token", "")); pageToken != "" {
				args = append(args, "--page", pageToken)
			}
			if !req.GetBool("include_shared_drives", true) {
				args = append(args, "--no-all-drives")
			}
			if folderID := strings.TrimSpace(req.GetString("folder_id", "")); folderID != "" {
				args = append(args, "--parent", folderID)
			}
			return args, nil
		},
	}
}

func mcpDocsGetTool() mcpToolSpec {
	return mcpToolSpec{
		Name:        "docs_get",
		Service:     "docs",
		Risk:        mcpRiskRead,
		Description: "Read a Google Doc as wrapped text, all tabs, or one tab.",
		Options: []mcp.ToolOption{
			mcp.WithString("document_id", mcp.Description("Google Docs document ID"), mcp.Required()),
			mcp.WithString("tab", mcp.Description("Optional tab title or ID")),
			mcp.WithBoolean("all_tabs", mcp.Description("Read all tabs"), mcp.DefaultBool(false)),
			mcp.WithInteger("max_bytes", mcp.Description("Maximum text bytes, 0 for unlimited"), mcp.DefaultNumber(2000000), mcp.Min(0)),
		},
		BuildArgs: func(req mcp.CallToolRequest) ([]string, error) {
			docID, err := requireMCPString(req, "document_id")
			if err != nil {
				return nil, err
			}
			args := []string{"docs", "cat", "--max-bytes", strconv.Itoa(clampMCPInt(req.GetInt("max_bytes", 2000000), 0, 20_000_000))}
			tab := strings.TrimSpace(req.GetString("tab", ""))
			_, tabProvided := req.GetArguments()["tab"]
			if tabProvided && tab == "" {
				return nil, fmt.Errorf("tab cannot be empty")
			}
			allTabs := req.GetBool("all_tabs", false)
			if tab != "" && allTabs {
				return nil, fmt.Errorf("tab and all_tabs are mutually exclusive")
			}
			if tab != "" {
				args = append(args, "--tab", tab)
			}
			if allTabs {
				args = append(args, "--all-tabs")
			}
			return append(args, "--", docID), nil
		},
	}
}

func mcpSheetsReadRangeTool() mcpToolSpec {
	return mcpToolSpec{
		Name:        "sheets_read_range",
		Service:     "sheets",
		Risk:        mcpRiskRead,
		Description: "Read values from a Google Sheets range.",
		Options: []mcp.ToolOption{
			mcp.WithString("spreadsheet_id", mcp.Description("Google Sheets spreadsheet ID"), mcp.Required()),
			mcp.WithString("range", mcp.Description("A1 notation or named range"), mcp.Required()),
			mcp.WithString("dimension", mcp.Description("Major dimension"), mcp.Enum("ROWS", "COLUMNS")),
			mcp.WithString("render", mcp.Description("Value render option"), mcp.Enum("FORMATTED_VALUE", "UNFORMATTED_VALUE", "FORMULA")),
		},
		BuildArgs: func(req mcp.CallToolRequest) ([]string, error) {
			spreadsheetID, err := requireMCPString(req, "spreadsheet_id")
			if err != nil {
				return nil, err
			}
			rangeSpec, err := requireMCPString(req, "range")
			if err != nil {
				return nil, err
			}
			args := []string{"sheets", "get"}
			if dimension := strings.TrimSpace(req.GetString("dimension", "")); dimension != "" {
				args = append(args, "--dimension", dimension)
			}
			if render := strings.TrimSpace(req.GetString("render", "")); render != "" {
				args = append(args, "--render", render)
			}
			return append(args, "--", spreadsheetID, rangeSpec), nil
		},
	}
}

func mcpCalendarEventsTool() mcpToolSpec {
	return mcpToolSpec{
		Name:        "calendar_events",
		Service:     "calendar",
		Risk:        mcpRiskRead,
		Description: "List Google Calendar events from primary or selected calendars with bounded paging.",
		Options: []mcp.ToolOption{
			mcp.WithString("calendar_id", mcp.Description("Calendar ID or selector; default primary")),
			mcp.WithString("from", mcp.Description("Start time: RFC3339, date, or relative value")),
			mcp.WithString("to", mcp.Description("End time: RFC3339, date, or relative value")),
			mcp.WithBoolean("today", mcp.Description("Today only"), mcp.DefaultBool(false)),
			mcp.WithBoolean("tomorrow", mcp.Description("Tomorrow only"), mcp.DefaultBool(false)),
			mcp.WithInteger("days", mcp.Description("Next N days"), mcp.DefaultNumber(0), mcp.Min(0), mcp.Max(31)),
			mcp.WithInteger("max", mcp.Description("Maximum results per page"), mcp.DefaultNumber(10), mcp.Min(1), mcp.Max(250)),
			mcp.WithString("page_token", mcp.Description("Opaque page token from a prior response")),
			mcp.WithBoolean("all_pages", mcp.Description("Fetch all pages within the CLI and MCP output bounds"), mcp.DefaultBool(false)),
			mcp.WithString("query", mcp.Description("Free text search")),
		},
		BuildArgs: func(req mcp.CallToolRequest) ([]string, error) {
			pageToken, err := mcpDefaultNonEmptyString(req, "page_token", "")
			if err != nil {
				return nil, err
			}
			allPages := req.GetBool("all_pages", false)
			if pageToken != "" && allPages {
				return nil, fmt.Errorf("page_token cannot be combined with all_pages")
			}

			args := []string{"calendar", "events"}
			calendarID := strings.TrimSpace(req.GetString("calendar_id", ""))
			for _, pair := range [][2]string{{"from", "--from"}, {"to", "--to"}, {"query", "--query"}} {
				if v := strings.TrimSpace(req.GetString(pair[0], "")); v != "" {
					args = append(args, pair[1], v)
				}
			}
			if req.GetBool("today", false) {
				args = append(args, "--today")
			}
			if req.GetBool("tomorrow", false) {
				args = append(args, "--tomorrow")
			}
			if days := req.GetInt("days", 0); days > 0 {
				args = append(args, "--days", strconv.Itoa(clampMCPInt(days, 1, 31)))
			}
			args = append(args, "--max", strconv.Itoa(clampMCPInt(req.GetInt("max", 10), 1, 250)))
			if pageToken != "" {
				args = append(args, "--page="+pageToken)
			}
			if allPages {
				args = append(args, "--all-pages")
			}
			if calendarID != "" {
				args = append(args, "--", calendarID)
			}
			return args, nil
		},
	}
}

func mcpDocsWriteTool() mcpToolSpec {
	return mcpToolSpec{
		Name:        "docs_write",
		Service:     "docs",
		Risk:        mcpRiskWrite,
		Description: "Write text to a Google Doc. Requires --allow-write on the MCP server.",
		Options: []mcp.ToolOption{
			mcp.WithString("document_id", mcp.Description("Google Docs document ID"), mcp.Required()),
			mcp.WithString("text", mcp.Description("Text or markdown to write"), mcp.Required()),
			mcp.WithString("tab", mcp.Description("Optional tab title or ID")),
			mcp.WithBoolean("append", mcp.Description("Append instead of replacing"), mcp.DefaultBool(true)),
			mcp.WithBoolean("replace", mcp.Description("Replace all existing content"), mcp.DefaultBool(false)),
			mcp.WithBoolean("markdown", mcp.Description("Convert markdown to Docs formatting"), mcp.DefaultBool(false)),
		},
		BuildArgs: func(req mcp.CallToolRequest) ([]string, error) {
			docID, err := requireMCPString(req, "document_id")
			if err != nil {
				return nil, err
			}
			text, err := requireMCPText(req, "text")
			if err != nil {
				return nil, err
			}
			args := []string{"docs", "write", "--text", text}
			reqArgs := req.GetArguments()
			tab := strings.TrimSpace(req.GetString("tab", ""))
			_, tabProvided := reqArgs["tab"]
			if tabProvided && tab == "" {
				return nil, fmt.Errorf("tab cannot be empty")
			}
			replace := req.GetBool("replace", false)
			appendProvided := false
			if reqArgs != nil {
				_, appendProvided = reqArgs["append"]
			}
			appendMode := req.GetBool("append", true)
			if replace && appendProvided && appendMode {
				return nil, fmt.Errorf("append and replace are mutually exclusive")
			}
			switch {
			case replace:
				args = append(args, "--replace")
			case appendMode:
				args = append(args, "--append")
			default:
				return nil, fmt.Errorf("append=false requires replace=true to avoid implicit document replacement")
			}
			if req.GetBool("markdown", false) {
				args = append(args, "--markdown")
			}
			if tab != "" {
				args = append(args, "--tab", tab)
			}
			return append(args, "--", docID), nil
		},
	}
}

func mcpSheetsUpdateRangeTool() mcpToolSpec {
	return mcpToolSpec{
		Name:        "sheets_update_range",
		Service:     "sheets",
		Risk:        mcpRiskWrite,
		Description: "Update values in a Google Sheets range. Requires --allow-write on the MCP server.",
		Options: []mcp.ToolOption{
			mcp.WithString("spreadsheet_id", mcp.Description("Google Sheets spreadsheet ID"), mcp.Required()),
			mcp.WithString("range", mcp.Description("A1 notation or named range"), mcp.Required()),
			mcp.WithString("values_json", mcp.Description("JSON 2D array of values"), mcp.Required()),
			mcp.WithString("input", mcp.Description("Value input option"), mcp.Enum("RAW", "USER_ENTERED"), mcp.DefaultString("USER_ENTERED")),
		},
		BuildArgs: func(req mcp.CallToolRequest) ([]string, error) {
			spreadsheetID, err := requireMCPString(req, "spreadsheet_id")
			if err != nil {
				return nil, err
			}
			rangeSpec, err := requireMCPString(req, "range")
			if err != nil {
				return nil, err
			}
			valuesJSON, err := requireMCPLiteralValuesJSON(req, "values_json")
			if err != nil {
				return nil, err
			}
			input := strings.TrimSpace(req.GetString("input", "USER_ENTERED"))
			if input == "" {
				input = "USER_ENTERED"
			}
			return []string{"sheets", "update", "--values-json", valuesJSON, "--input", input, "--", spreadsheetID, rangeSpec}, nil
		},
	}
}

func mcpGmailListLabelsTool() mcpToolSpec {
	return mcpToolSpec{
		Name: "gmail_list_labels", Service: "gmail", Risk: mcpRiskRead,
		Description: "List Gmail labels.",
		Options:     []mcp.ToolOption{},
		BuildArgs: func(req mcp.CallToolRequest) ([]string, error) {
			return []string{"gmail", "labels", "list"}, nil
		},
	}
}

func mcpGmailListDraftsTool() mcpToolSpec {
	return mcpToolSpec{
		Name: "gmail_list_drafts", Service: "gmail", Risk: mcpRiskRead,
		Description: "List Gmail drafts with bounded paging.",
		Options: []mcp.ToolOption{
			mcp.WithInteger("max", mcp.Description("Maximum results"), mcp.DefaultNumber(20), mcp.Min(1), mcp.Max(100)),
			mcp.WithString("page_token", mcp.Description("Opaque page token")),
		},
		BuildArgs: func(req mcp.CallToolRequest) ([]string, error) {
			args := []string{"gmail", "drafts", "list", "--max", strconv.Itoa(clampMCPInt(req.GetInt("max", 20), 1, 100))}
			if token := strings.TrimSpace(req.GetString("page_token", "")); token != "" {
				args = append(args, "--page", token)
			}
			return args, nil
		},
	}
}

func mcpGmailGetDraftTool() mcpToolSpec {
	return mcpToolSpec{
		Name: "gmail_get_draft", Service: "gmail", Risk: mcpRiskRead,
		Description: "Get one Gmail draft by ID without downloading attachments.",
		Options: []mcp.ToolOption{
			mcp.WithString("draft_id", mcp.Description("Gmail draft ID"), mcp.Required()),
		},
		BuildArgs: func(req mcp.CallToolRequest) ([]string, error) {
			id, err := requireMCPString(req, "draft_id")
			if err != nil {
				return nil, err
			}
			return []string{"gmail", "drafts", "get", "--", id}, nil
		},
	}
}

func mcpGmailDeleteDraftTool() mcpToolSpec {
	return mcpToolSpec{
		Name: "gmail_delete_draft", Service: "gmail", Risk: mcpRiskDestructive,
		Description: "Permanently delete one Gmail draft by ID. Drafts are not moved to Trash and cannot be recovered. Requires ordinary write authorization plus explicit destructive authorization.",
		Options: []mcp.ToolOption{
			mcp.WithString("draft_id", mcp.Description("Gmail draft ID"), mcp.Required()),
		},
		BuildArgs: func(req mcp.CallToolRequest) ([]string, error) {
			id, err := requireMCPString(req, "draft_id")
			if err != nil {
				return nil, err
			}
			return []string{"gmail", "drafts", "delete", "--force", "--", id}, nil
		},
	}
}

func mcpGmailCreateDraftTool() mcpToolSpec {
	return mcpToolSpec{
		Name: "gmail_create_draft", Service: "gmail", Risk: mcpRiskWrite,
		Description: "Create a Gmail draft from inline text or HTML. Never sends mail. Requires --allow-write.",
		Options: []mcp.ToolOption{
			mcp.WithString("to", mcp.Description("Recipients (comma-separated)")),
			mcp.WithString("cc", mcp.Description("CC recipients (comma-separated)")),
			mcp.WithString("bcc", mcp.Description("BCC recipients (comma-separated)")),
			mcp.WithString("subject", mcp.Description("Subject (required unless replying/threading)")),
			mcp.WithString("body", mcp.Description("Plain-text body")),
			mcp.WithString("body_html", mcp.Description("HTML body")),
			mcp.WithString("reply_to_message_id", mcp.Description("Reply-to Gmail message ID")),
			mcp.WithString("thread_id", mcp.Description("Existing Gmail thread ID")),
			mcp.WithBoolean("reply_all", mcp.Description("Reply to all; requires a reply target"), mcp.DefaultBool(false)),
			mcp.WithString("reply_to", mcp.Description("Reply-To header")),
			mcp.WithBoolean("quote", mcp.Description("Quote original; requires a reply target"), mcp.DefaultBool(false)),
			mcp.WithString("from", mcp.Description("Verified send-as alias")),
		},
		BuildArgs: func(req mcp.CallToolRequest) ([]string, error) {
			args := []string{"gmail", "drafts", "create"}
			get := func(key string) string { return strings.TrimSpace(req.GetString(key, "")) }
			to, cc, bcc := get("to"), get("cc"), get("bcc")
			subject, body, html := get("subject"), req.GetString("body", ""), req.GetString("body_html", "")
			replyID, threadID, replyTo, from := get("reply_to_message_id"), get("thread_id"), get("reply_to"), get("from")
			if subject == "" && replyID == "" && threadID == "" {
				return nil, fmt.Errorf("subject required unless reply_to_message_id or thread_id is set")
			}
			if strings.TrimSpace(body) == "" && strings.TrimSpace(html) == "" {
				return nil, fmt.Errorf("body or body_html is required")
			}
			if replyID != "" && threadID != "" {
				return nil, fmt.Errorf("reply_to_message_id and thread_id are mutually exclusive")
			}
			replyAll := req.GetBool("reply_all", false)
			quote := req.GetBool("quote", false)
			if (replyAll || quote) && replyID == "" && threadID == "" {
				return nil, fmt.Errorf("reply_all and quote require reply_to_message_id or thread_id")
			}
			for _, pair := range [][2]string{{to, "--to"}, {cc, "--cc"}, {bcc, "--bcc"}, {subject, "--subject"}, {replyID, "--reply-to-message-id"}, {threadID, "--thread-id"}, {replyTo, "--reply-to"}, {from, "--from"}} {
				if pair[0] != "" {
					args = append(args, pair[1], pair[0])
				}
			}
			if strings.TrimSpace(body) != "" {
				args = append(args, "--body", body)
			}
			if strings.TrimSpace(html) != "" {
				args = append(args, "--body-html", html)
			}
			if replyAll {
				args = append(args, "--reply-all")
			}
			if quote {
				args = append(args, "--quote")
			}
			return args, nil
		},
	}
}

func mcpGmailModifyMessageLabelsTool() mcpToolSpec {
	return mcpGmailModifyLabelsTool(
		"gmail_modify_message_labels",
		"Modify non-trash labels on one Gmail message by ID. Requires --allow-write; use gmail_trash_messages for Trash.",
		"message_id",
		[]string{"gmail", "messages", "modify"},
	)
}

func mcpGmailModifyThreadLabelsTool() mcpToolSpec {
	return mcpGmailModifyLabelsTool(
		"gmail_modify_thread_labels",
		"Modify non-trash labels on one Gmail thread by ID. Requires --allow-write; moving threads to Trash is not exposed.",
		"thread_id",
		[]string{"gmail", "thread", "modify"},
	)
}

func mcpGmailModifyLabelsTool(name, description, idField string, prefix []string) mcpToolSpec {
	return mcpToolSpec{
		Name: name, Service: "gmail", Risk: mcpRiskWrite, Description: description,
		Options: []mcp.ToolOption{
			mcp.WithString(idField, mcp.Description("Gmail ID"), mcp.Required()),
			mcp.WithString("add", mcp.Description("Comma-separated labels to add")),
			mcp.WithString("remove", mcp.Description("Comma-separated labels to remove")),
		},
		BuildArgs: func(req mcp.CallToolRequest) ([]string, error) {
			id, err := requireMCPString(req, idField)
			if err != nil {
				return nil, err
			}
			add := strings.TrimSpace(req.GetString("add", ""))
			remove := strings.TrimSpace(req.GetString("remove", ""))
			if add == "" && remove == "" {
				return nil, fmt.Errorf("must specify add and/or remove")
			}
			for _, label := range strings.Split(add, ",") {
				if strings.EqualFold(strings.TrimSpace(label), "TRASH") {
					return nil, fmt.Errorf("adding the TRASH label is destructive; use gmail_trash_messages with explicit destructive authorization")
				}
			}
			args := append([]string(nil), prefix...)
			if add != "" {
				args = append(args, "--add", add)
			}
			if remove != "" {
				args = append(args, "--remove", remove)
			}
			return append(args, "--", id), nil
		},
	}
}

func mcpGmailArchiveMessagesTool() mcpToolSpec {
	return mcpGmailExplicitIDsTool("gmail_archive_messages", "Archive Gmail messages by explicit ID.", "message_ids", []string{"gmail", "archive"})
}

func mcpGmailArchiveThreadsTool() mcpToolSpec {
	return mcpGmailExplicitIDsTool("gmail_archive_threads", "Archive Gmail threads by explicit ID.", "thread_ids", []string{"gmail", "archive", "--thread"})
}

func mcpGmailMarkMessagesReadTool() mcpToolSpec {
	return mcpGmailExplicitIDsTool("gmail_mark_messages_read", "Mark Gmail messages as read by explicit ID.", "message_ids", []string{"gmail", "mark-read", "--"})
}

func mcpGmailMarkMessagesUnreadTool() mcpToolSpec {
	return mcpGmailExplicitIDsTool("gmail_mark_messages_unread", "Mark Gmail messages as unread by explicit ID.", "message_ids", []string{"gmail", "unread", "--"})
}

func mcpGmailTrashMessagesTool() mcpToolSpec {
	return mcpToolSpec{
		Name: "gmail_trash_messages", Service: "gmail", Risk: mcpRiskDestructive,
		Description: "Move explicit Gmail messages to Trash. Gmail keeps trashed messages recoverable for its retention window; requires --allow-write and explicit destructive authorization.",
		Options: []mcp.ToolOption{
			mcp.WithArray("message_ids", mcp.Description("Explicit Gmail message IDs to move to Trash"), mcp.Required(), mcp.MinItems(1), mcp.MaxItems(1000), mcp.WithStringItems()),
		},
		BuildArgs: func(req mcp.CallToolRequest) ([]string, error) {
			ids, err := requireMCPGmailTrashMessageIDs(req)
			if err != nil {
				return nil, err
			}
			return append([]string{"gmail", "trash"}, ids...), nil
		},
	}
}

func requireMCPGmailTrashMessageIDs(req mcp.CallToolRequest) ([]string, error) {
	ids, err := requireMCPStringArray(req, "message_ids", 1000)
	if err != nil {
		return nil, err
	}
	for i, id := range ids {
		for _, r := range id {
			if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')) {
				return nil, fmt.Errorf("message_ids[%d] must contain only letters and digits", i)
			}
		}
	}
	return ids, nil
}

func mcpGmailExplicitIDsTool(name, description, field string, prefix []string) mcpToolSpec {
	return mcpToolSpec{
		Name: name, Service: "gmail", Risk: mcpRiskWrite, Description: description + " Requires --allow-write.",
		Options: []mcp.ToolOption{
			mcp.WithArray(field, mcp.Description("Explicit Gmail IDs"), mcp.Required(), mcp.MinItems(1), mcp.MaxItems(1000), mcp.WithStringItems()),
		},
		BuildArgs: func(req mcp.CallToolRequest) ([]string, error) {
			ids, err := requireMCPStringArray(req, field, 1000)
			if err != nil {
				return nil, err
			}
			args := append([]string(nil), prefix...)
			if len(args) == 0 || args[len(args)-1] != "--" {
				args = append(args, "--")
			}
			return append(args, ids...), nil
		},
	}
}

func requireMCPStringArray(req mcp.CallToolRequest, key string, limit int) ([]string, error) {
	raw, ok := req.GetArguments()[key]
	if !ok {
		return nil, fmt.Errorf("required argument %q not found", key)
	}
	items, ok := raw.([]any)
	if !ok {
		if typed, okTyped := raw.([]string); okTyped {
			items = make([]any, len(typed))
			for i := range typed {
				items[i] = typed[i]
			}
		} else {
			return nil, fmt.Errorf("argument %q is not an array", key)
		}
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("%s must contain at least one value", key)
	}
	if limit > 0 && len(items) > limit {
		return nil, fmt.Errorf("%s must contain at most %d values", key, limit)
	}
	out := make([]string, 0, len(items))
	for i, item := range items {
		value, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("%s[%d] must be a string", key, i)
		}
		value = strings.TrimSpace(value)
		if value == "" {
			return nil, fmt.Errorf("%s[%d] must not be empty", key, i)
		}
		out = append(out, value)
	}
	return out, nil
}

func mcpCalendarListCalendarsTool() mcpToolSpec {
	return mcpToolSpec{
		Name: "calendar_list_calendars", Service: "calendar", Risk: mcpRiskRead,
		Description: "List Google Calendar calendars with bounded paging.",
		Options: []mcp.ToolOption{
			mcp.WithInteger("max", mcp.Description("Maximum results per page"), mcp.DefaultNumber(100), mcp.Min(1), mcp.Max(250)),
			mcp.WithString("page_token", mcp.Description("Page token")),
			mcp.WithBoolean("all_pages", mcp.Description("Fetch all pages"), mcp.DefaultBool(false)),
		},
		BuildArgs: func(req mcp.CallToolRequest) ([]string, error) {
			args := []string{"calendar", "calendars", "--max", strconv.Itoa(clampMCPInt(req.GetInt("max", 100), 1, 250))}
			if page := strings.TrimSpace(req.GetString("page_token", "")); page != "" {
				args = append(args, "--page", page)
			}
			if req.GetBool("all_pages", false) {
				args = append(args, "--all")
			}
			return args, nil
		},
	}
}

func mcpCalendarSearchTool() mcpToolSpec {
	return mcpToolSpec{
		Name: "calendar_search_events", Service: "calendar", Risk: mcpRiskRead,
		Description: "Search Google Calendar events by free-text query over a bounded time window.",
		Options: []mcp.ToolOption{
			mcp.WithString("query", mcp.Description("Search query"), mcp.Required()),
			mcp.WithString("calendar_id", mcp.Description("Calendar ID or selector")),
			mcp.WithString("from", mcp.Description("Start time")),
			mcp.WithString("to", mcp.Description("End time")),
			mcp.WithInteger("max", mcp.Description("Maximum results"), mcp.DefaultNumber(10), mcp.Min(1), mcp.Max(250)),
		},
		BuildArgs: func(req mcp.CallToolRequest) ([]string, error) {
			query, err := requireMCPString(req, "query")
			if err != nil {
				return nil, err
			}
			args := []string{"calendar", "search"}
			for _, pair := range [][2]string{{"from", "--from"}, {"to", "--to"}} {
				if value := strings.TrimSpace(req.GetString(pair[0], "")); value != "" {
					args = append(args, pair[1], value)
				}
			}
			if calendarID := strings.TrimSpace(req.GetString("calendar_id", "")); calendarID != "" {
				args = append(args, "--calendar", calendarID)
			}
			args = append(args, "--max", strconv.Itoa(clampMCPInt(req.GetInt("max", 10), 1, 250)), "--", query)
			return args, nil
		},
	}
}

func mcpCalendarGetEventTool() mcpToolSpec {
	return mcpToolSpec{
		Name: "calendar_get_event", Service: "calendar", Risk: mcpRiskRead,
		Description: "Get one Google Calendar event by ID with timezone-aware rendering.",
		Options: []mcp.ToolOption{
			mcp.WithString("calendar_id", mcp.Description("Calendar ID or selector"), mcp.Required()),
			mcp.WithString("event_id", mcp.Description("Event ID or Calendar URL"), mcp.Required()),
			mcp.WithString("timezone", mcp.Description("Display timezone")),
		},
		BuildArgs: func(req mcp.CallToolRequest) ([]string, error) {
			calendarID, err := requireMCPString(req, "calendar_id")
			if err != nil {
				return nil, err
			}
			eventID, err := requireMCPString(req, "event_id")
			if err != nil {
				return nil, err
			}
			args := []string{"calendar", "event"}
			if timezone := strings.TrimSpace(req.GetString("timezone", "")); timezone != "" {
				args = append(args, "--timezone", timezone)
			}
			return append(args, "--", calendarID, eventID), nil
		},
	}
}

func mcpCalendarFreeBusyTool() mcpToolSpec {
	return mcpToolSpec{
		Name: "calendar_freebusy", Service: "calendar", Risk: mcpRiskRead,
		Description: "Get free/busy blocks for selected calendars in a time range.",
		Options: []mcp.ToolOption{
			mcp.WithString("calendar_id", mcp.Description("Primary selected calendar")),
			mcp.WithArray("extra_calendar_ids", mcp.Description("Additional calendar IDs"), mcp.WithStringItems(), mcp.MaxItems(100)),
			mcp.WithBoolean("all", mcp.Description("Query all calendars"), mcp.DefaultBool(false)),
			mcp.WithString("from", mcp.Description("Start time"), mcp.Required()),
			mcp.WithString("to", mcp.Description("End time"), mcp.Required()),
		},
		BuildArgs: func(req mcp.CallToolRequest) ([]string, error) {
			from, err := requireMCPString(req, "from")
			if err != nil {
				return nil, err
			}
			to, err := requireMCPString(req, "to")
			if err != nil {
				return nil, err
			}
			primary := strings.TrimSpace(req.GetString("calendar_id", ""))
			extras, err := requireMCPOptionalStringArray(req, "extra_calendar_ids")
			if err != nil {
				return nil, err
			}
			all := req.GetBool("all", false)
			if all && (primary != "" || len(extras) > 0) {
				return nil, fmt.Errorf("all cannot be combined with calendar selectors")
			}
			args := []string{"calendar", "freebusy"}
			for _, id := range extras {
				args = append(args, "--cal", id)
			}
			if all {
				args = append(args, "--all")
			}
			args = append(args, "--from", from, "--to", to)
			if primary != "" {
				args = append(args, "--", primary)
			}
			return args, nil
		},
	}
}

func requireMCPOptionalStringArray(req mcp.CallToolRequest, key string) ([]string, error) {
	if _, ok := req.GetArguments()[key]; !ok {
		return nil, nil
	}
	raw := req.GetArguments()[key]
	if typed, ok := raw.([]string); ok && len(typed) > 100 {
		return nil, fmt.Errorf("%s must contain at most 100 values", key)
	}
	items, ok := raw.([]any)
	if !ok {
		if typed, okTyped := raw.([]string); okTyped {
			out := make([]string, len(typed))
			for i := range typed {
				out[i] = strings.TrimSpace(typed[i])
			}
			return rejectEmptyMCPArrayItems(key, out)
		}
		return nil, fmt.Errorf("argument %q is not an array", key)
	}
	if len(items) > 100 {
		return nil, fmt.Errorf("%s must contain at most 100 values", key)
	}
	out := make([]string, 0, len(items))
	for i, item := range items {
		value, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("%s[%d] must be a string", key, i)
		}
		out = append(out, strings.TrimSpace(value))
	}
	return rejectEmptyMCPArrayItems(key, out)
}

func rejectEmptyMCPArrayItems(key string, values []string) ([]string, error) {
	for i, value := range values {
		if value == "" {
			return nil, fmt.Errorf("%s[%d] must not be empty", key, i)
		}
	}
	return values, nil
}

func mcpCalendarConflictsTool() mcpToolSpec {
	return mcpToolSpec{
		Name: "calendar_find_conflicts", Service: "calendar", Risk: mcpRiskRead,
		Description: "Find busy-time overlaps across two or more calendars.",
		Options: []mcp.ToolOption{
			mcp.WithString("from", mcp.Description("Start time")),
			mcp.WithString("to", mcp.Description("End time")),
			mcp.WithArray("calendar_ids", mcp.Description("At least two explicit calendar IDs unless all"), mcp.WithStringItems(), mcp.MaxItems(100)),
			mcp.WithBoolean("all", mcp.Description("Query all calendars"), mcp.DefaultBool(false)),
			mcp.WithInteger("days", mcp.Description("Next N days"), mcp.DefaultNumber(0), mcp.Min(0), mcp.Max(31)),
		},
		BuildArgs: func(req mcp.CallToolRequest) ([]string, error) {
			from, to := strings.TrimSpace(req.GetString("from", "")), strings.TrimSpace(req.GetString("to", ""))
			days := req.GetInt("days", 0)
			if (from == "" || to == "") && days <= 0 {
				return nil, fmt.Errorf("provide from and to or a positive days value")
			}
			if (from == "") != (to == "") && days <= 0 {
				return nil, fmt.Errorf("from and to must be supplied together")
			}
			ids, err := requireMCPOptionalStringArray(req, "calendar_ids")
			if err != nil {
				return nil, err
			}
			all := req.GetBool("all", false)
			if all && len(ids) > 0 {
				return nil, fmt.Errorf("all cannot be combined with calendar_ids")
			}
			if !all && len(ids) < 2 {
				return nil, fmt.Errorf("calendar_ids must contain at least two calendars unless all is true")
			}
			args := []string{"calendar", "conflicts"}
			if from != "" {
				args = append(args, "--from", from)
				args = append(args, "--to", to)
			}
			if days > 0 {
				args = append(args, "--days", strconv.Itoa(clampMCPInt(days, 1, 31)))
			}
			if all {
				args = append(args, "--all")
			} else {
				for _, id := range ids {
					args = append(args, "--cal", id)
				}
			}
			return args, nil
		},
	}
}

func mcpCalendarCreateEventTool() mcpToolSpec {
	return mcpToolSpec{
		Name: "calendar_create_event", Service: "calendar", Risk: mcpRiskWrite,
		Description: "Create an ordinary Google Calendar event. Requires --allow-write.",
		Options: []mcp.ToolOption{
			mcp.WithString("calendar_id", mcp.Description("Calendar ID"), mcp.Required()),
			mcp.WithString("summary", mcp.Description("Event title"), mcp.Required()),
			mcp.WithString("from", mcp.Description("Start time"), mcp.Required()),
			mcp.WithString("to", mcp.Description("End time"), mcp.Required()),
			mcp.WithString("description", mcp.Description("Description")),
			mcp.WithString("location", mcp.Description("Location")),
			mcp.WithBoolean("all_day", mcp.Description("All-day event"), mcp.DefaultBool(false)),
			mcp.WithString("start_timezone", mcp.Description("Start IANA timezone")),
			mcp.WithString("end_timezone", mcp.Description("End IANA timezone")),
			mcp.WithString("timezone", mcp.Description("Common IANA timezone")),
			mcp.WithArray("attendees", mcp.Description("Attendees"), mcp.WithStringItems(), mcp.MaxItems(100)),
			mcp.WithArray("rrule", mcp.Description("Recurrence rules"), mcp.WithStringItems(), mcp.MaxItems(100)),
			mcp.WithArray("reminders", mcp.Description("Reminder method:duration values"), mcp.WithStringItems(), mcp.MaxItems(5)),
			mcp.WithString("color_id", mcp.Description("Event color ID")),
			mcp.WithString("visibility", mcp.Enum("default", "public", "private", "confidential")),
			mcp.WithString("transparency", mcp.Enum("opaque", "busy", "transparent", "free")),
			mcp.WithString("send_updates", mcp.Enum("none", "all", "externalOnly"), mcp.DefaultString("none")),
			mcp.WithBoolean("guests_can_invite", mcp.Description("Guests may invite others")),
			mcp.WithBoolean("guests_can_modify", mcp.Description("Guests may modify")),
			mcp.WithBoolean("guests_can_see_others", mcp.Description("Guests may see others")),
		},
		BuildArgs: func(req mcp.CallToolRequest) ([]string, error) {
			calendarID, err := requireMCPString(req, "calendar_id")
			if err != nil {
				return nil, err
			}
			summary, err := requireMCPString(req, "summary")
			if err != nil {
				return nil, err
			}
			from, err := requireMCPString(req, "from")
			if err != nil {
				return nil, err
			}
			to, err := requireMCPString(req, "to")
			if err != nil {
				return nil, err
			}
			if validationErr := validateMCPArgStrings(map[string]string{"calendar_id": calendarID, "summary": summary, "from": from, "to": to}); validationErr != nil {
				return nil, validationErr
			}
			timezone := strings.TrimSpace(req.GetString("timezone", ""))
			startTimezone := strings.TrimSpace(req.GetString("start_timezone", ""))
			endTimezone := strings.TrimSpace(req.GetString("end_timezone", ""))
			if timezone != "" && (startTimezone != "" || endTimezone != "") {
				return nil, fmt.Errorf("timezone cannot be combined with start_timezone or end_timezone")
			}
			allDay := req.GetBool("all_day", false)
			if allDay && (!isMCPDateOnly(from) || !isMCPDateOnly(to)) {
				return nil, fmt.Errorf("all_day requires date-only from and to")
			}
			if !allDay {
				if validationErr := validateMCPRFC3339("from", from); validationErr != nil {
					return nil, validationErr
				}
				if validationErr := validateMCPRFC3339("to", to); validationErr != nil {
					return nil, validationErr
				}
			}
			for key, value := range map[string]string{
				"timezone": timezone, "start_timezone": startTimezone, "end_timezone": endTimezone,
			} {
				if value != "" {
					if validationErr := validateMCPTimezone(key, value); validationErr != nil {
						return nil, validationErr
					}
				}
			}
			reminders, err := requireMCPOptionalStringArray(req, "reminders")
			if err != nil {
				return nil, err
			}
			if len(reminders) > 5 {
				return nil, fmt.Errorf("reminders must contain at most 5 values")
			}
			args := []string{"calendar", "create", "--summary", summary, "--from", from, "--to", to}
			for _, pair := range [][2]string{{"description", "--description"}, {"location", "--location"}, {"start_timezone", "--start-timezone"}, {"end_timezone", "--end-timezone"}, {"timezone", "--timezone"}, {"color_id", "--event-color"}, {"visibility", "--visibility"}, {"transparency", "--transparency"}} {
				if value := strings.TrimSpace(req.GetString(pair[0], "")); value != "" {
					if validationErr := validateMCPArgStrings(map[string]string{pair[0]: value}); validationErr != nil {
						return nil, validationErr
					}
					args = append(args, pair[1], value)
				}
			}
			if allDay {
				args = append(args, "--all-day")
			}
			if attendees, e := requireMCPOptionalStringArray(req, "attendees"); e != nil {
				return nil, e
			} else if len(attendees) > 0 {
				args = append(args, "--attendees", strings.Join(attendees, ","))
			}
			for _, item := range []struct {
				key, flag string
				values    []string
			}{{"rrule", "--rrule", nil}, {"reminders", "--reminder", reminders}} {
				values := item.values
				if item.key == "rrule" {
					values, err = requireMCPOptionalStringArray(req, item.key)
					if err != nil {
						return nil, err
					}
				}
				for _, value := range values {
					args = append(args, item.flag, value)
				}
			}
			sendUpdates := strings.TrimSpace(req.GetString("send_updates", "none"))
			if sendUpdates == "" {
				sendUpdates = "none"
			}
			if validationErr := validateMCPEnum("send_updates", sendUpdates, "none", "all", "externalOnly"); validationErr != nil {
				return nil, validationErr
			}
			args = append(args, "--send-updates", sendUpdates)
			reqArgs := req.GetArguments()
			for _, item := range []struct {
				key, flag string
			}{{"guests_can_invite", "--guests-can-invite"}, {"guests_can_modify", "--guests-can-modify"}, {"guests_can_see_others", "--guests-can-see-others"}} {
				if _, present := reqArgs[item.key]; present {
					if req.GetBool(item.key, false) {
						args = append(args, item.flag)
					} else {
						args = append(args, "--no-"+strings.TrimPrefix(item.flag, "--"))
					}
				}
			}
			return append(args, "--", calendarID), nil
		},
	}
}

func isMCPDateOnly(value string) bool {
	if len(strings.TrimSpace(value)) != len("2006-01-02") {
		return false
	}
	_, err := time.Parse("2006-01-02", strings.TrimSpace(value))
	return err == nil
}

func validateMCPRFC3339(key, value string) error {
	if _, err := time.Parse(time.RFC3339, strings.TrimSpace(value)); err != nil {
		return fmt.Errorf("%s must be RFC3339: %w", key, err)
	}
	return nil
}

func validateMCPTimezone(key, value string) error {
	if _, err := time.LoadLocation(strings.TrimSpace(value)); err != nil {
		return fmt.Errorf("%s must be an IANA timezone: %w", key, err)
	}
	return nil
}

func validateMCPEnum(key, value string, allowed ...string) error {
	for _, candidate := range allowed {
		if value == candidate {
			return nil
		}
	}
	return fmt.Errorf("%s has unsupported value %q", key, value)
}

func mcpDefaultNonEmptyString(req mcp.CallToolRequest, key, defaultValue string) (string, error) {
	if _, supplied := req.GetArguments()[key]; !supplied {
		return defaultValue, nil
	}
	value := strings.TrimSpace(req.GetString(key, ""))
	if value == "" {
		return "", fmt.Errorf("%s must not be empty when supplied", key)
	}
	return value, nil
}

func validateMCPArgStrings(values map[string]string) error {
	for key, value := range values {
		if strings.ContainsAny(value, "\r\n") || strings.HasPrefix(value, "-") {
			return fmt.Errorf("%s contains an unsafe argument value", key)
		}
	}
	return nil
}

func mcpCalendarRespondTool() mcpToolSpec {
	return mcpToolSpec{
		Name: "calendar_respond_to_event", Service: "calendar", Risk: mcpRiskWrite,
		Description: "Respond to a calendar invitation with a status and optional comment. Requires --allow-write.",
		Options: []mcp.ToolOption{
			mcp.WithString("calendar_id", mcp.Description("Calendar ID"), mcp.Required()),
			mcp.WithString("event_id", mcp.Description("Event ID"), mcp.Required()),
			mcp.WithString("status", mcp.Enum("accepted", "declined", "tentative", "needsAction"), mcp.Required()),
			mcp.WithString("comment", mcp.Description("Optional comment")),
		},
		BuildArgs: func(req mcp.CallToolRequest) ([]string, error) {
			calendarID, err := requireMCPString(req, "calendar_id")
			if err != nil {
				return nil, err
			}
			eventID, err := requireMCPString(req, "event_id")
			if err != nil {
				return nil, err
			}
			status, err := requireMCPString(req, "status")
			if err != nil {
				return nil, err
			}
			args := []string{"calendar", "respond", "--status", status}
			if comment := strings.TrimSpace(req.GetString("comment", "")); comment != "" {
				args = append(args, "--comment", comment)
			}
			return append(args, "--", calendarID, eventID), nil
		},
	}
}

func mcpCalendarMoveTool() mcpToolSpec {
	return mcpToolSpec{
		Name: "calendar_move_event", Service: "calendar", Risk: mcpRiskWrite,
		Description: "Move an event to another calendar; the destination becomes organizer. Requires --allow-write.",
		Options: []mcp.ToolOption{
			mcp.WithString("source_calendar_id", mcp.Description("Source calendar ID"), mcp.Required()),
			mcp.WithString("event_id", mcp.Description("Event ID"), mcp.Required()),
			mcp.WithString("destination_calendar_id", mcp.Description("Destination calendar ID"), mcp.Required()),
			mcp.WithString("send_updates", mcp.Enum("all", "externalOnly", "none"), mcp.DefaultString("none")),
		},
		BuildArgs: func(req mcp.CallToolRequest) ([]string, error) {
			source, err := requireMCPString(req, "source_calendar_id")
			if err != nil {
				return nil, err
			}
			eventID, err := requireMCPString(req, "event_id")
			if err != nil {
				return nil, err
			}
			destination, err := requireMCPString(req, "destination_calendar_id")
			if err != nil {
				return nil, err
			}
			if strings.EqualFold(source, destination) {
				return nil, fmt.Errorf("source and destination calendars must differ")
			}
			sendUpdates := strings.TrimSpace(req.GetString("send_updates", "none"))
			if sendUpdates == "" {
				sendUpdates = "none"
			}
			return []string{"calendar", "move", "--send-updates", sendUpdates, "--", source, eventID, destination}, nil
		},
	}
}

func mcpCalendarCreateCalendarTool() mcpToolSpec {
	return mcpToolSpec{
		Name: "calendar_create_calendar", Service: "calendar", Risk: mcpRiskWrite,
		Description: "Create a secondary Google Calendar. Requires --allow-write; delete it separately if cleanup is needed.",
		Options: []mcp.ToolOption{
			mcp.WithString("summary", mcp.Description("Calendar name"), mcp.Required()),
			mcp.WithString("description", mcp.Description("Calendar description")),
			mcp.WithString("timezone", mcp.Description("IANA timezone")),
			mcp.WithString("location", mcp.Description("Calendar location")),
		},
		BuildArgs: func(req mcp.CallToolRequest) ([]string, error) {
			summary, err := requireMCPString(req, "summary")
			if err != nil {
				return nil, err
			}
			args := []string{"calendar", "create-calendar"}
			for _, pair := range [][2]string{{"description", "--description"}, {"timezone", "--timezone"}, {"location", "--location"}} {
				if value := strings.TrimSpace(req.GetString(pair[0], "")); value != "" {
					args = append(args, pair[1], value)
				}
			}
			return append(args, "--", summary), nil
		},
	}
}

func mcpCalendarSubscribeTool() mcpToolSpec {
	return mcpToolSpec{
		Name: "calendar_subscribe", Service: "calendar", Risk: mcpRiskWrite,
		Description: "Subscribe to a raw Google Calendar ID. Requires --allow-write.",
		Options: []mcp.ToolOption{
			mcp.WithString("calendar_id", mcp.Description("Raw calendar ID"), mcp.Required()),
			mcp.WithString("color_id", mcp.Description("Color ID")),
			mcp.WithBoolean("hidden", mcp.Description("Hide from calendar list"), mcp.DefaultBool(false)),
			mcp.WithBoolean("selected", mcp.Description("Show events"), mcp.DefaultBool(true)),
		},
		BuildArgs: func(req mcp.CallToolRequest) ([]string, error) {
			id, err := requireMCPString(req, "calendar_id")
			if err != nil {
				return nil, err
			}
			args := []string{"calendar", "subscribe"}
			if color := strings.TrimSpace(req.GetString("color_id", "")); color != "" {
				colorNumber, parseErr := strconv.Atoi(color)
				if parseErr != nil || colorNumber < 1 || colorNumber > 24 {
					return nil, fmt.Errorf("color_id must be an integer from 1 through 24")
				}
				args = append(args, "--color-id", color)
			}
			if req.GetBool("hidden", false) {
				args = append(args, "--hidden")
			}
			if !req.GetBool("selected", true) {
				args = append(args, "--no-selected")
			}
			return append(args, "--", id), nil
		},
	}
}

func mcpCalendarUnsubscribeTool() mcpToolSpec {
	return mcpToolSpec{
		Name: "calendar_unsubscribe", Service: "calendar", Risk: mcpRiskWrite,
		Description: "Unsubscribe from a Google Calendar. Requires --allow-write.",
		Options: []mcp.ToolOption{
			mcp.WithString("calendar_id", mcp.Description("Calendar ID or alias"), mcp.Required()),
		},
		BuildArgs: func(req mcp.CallToolRequest) ([]string, error) {
			id, err := requireMCPString(req, "calendar_id")
			if err != nil {
				return nil, err
			}
			return []string{"calendar", "unsubscribe", "--force", "--", id}, nil
		},
	}
}

func mcpCalendarFocusTimeTool() mcpToolSpec {
	return mcpToolSpec{
		Name: "calendar_focus_time", Service: "calendar", Risk: mcpRiskWrite,
		Description: "Create a Focus Time event. Requires --allow-write.",
		Options: []mcp.ToolOption{
			mcp.WithString("from", mcp.Required()),
			mcp.WithString("to", mcp.Required()),
			mcp.WithString("calendar_id", mcp.DefaultString("primary")),
			mcp.WithString("summary", mcp.DefaultString("Focus Time")),
			mcp.WithString("auto_decline", mcp.Enum("none", "all", "new"), mcp.DefaultString("all")),
			mcp.WithString("decline_message"),
			mcp.WithString("chat_status", mcp.Enum("available", "doNotDisturb"), mcp.DefaultString("doNotDisturb")),
			mcp.WithArray("rrule", mcp.WithStringItems(), mcp.MaxItems(100)),
		},
		BuildArgs: func(req mcp.CallToolRequest) ([]string, error) {
			from, err := requireMCPString(req, "from")
			if err != nil {
				return nil, err
			}
			to, err := requireMCPString(req, "to")
			if err != nil {
				return nil, err
			}
			if validationErr := validateMCPRFC3339("from", from); validationErr != nil {
				return nil, validationErr
			}
			if validationErr := validateMCPRFC3339("to", to); validationErr != nil {
				return nil, validationErr
			}
			id, err := mcpDefaultNonEmptyString(req, "calendar_id", "primary")
			if err != nil {
				return nil, err
			}
			summary, err := mcpDefaultNonEmptyString(req, "summary", "Focus Time")
			if err != nil {
				return nil, err
			}
			autoDecline := strings.TrimSpace(req.GetString("auto_decline", "all"))
			if validationErr := validateMCPEnum("auto_decline", autoDecline, "none", "all", "new"); validationErr != nil {
				return nil, validationErr
			}
			chatStatus := strings.TrimSpace(req.GetString("chat_status", "doNotDisturb"))
			if validationErr := validateMCPEnum("chat_status", chatStatus, "available", "doNotDisturb"); validationErr != nil {
				return nil, validationErr
			}
			args := []string{"calendar", "focus-time", "--summary", summary, "--from", from, "--to", to, "--auto-decline", autoDecline, "--chat-status", chatStatus}
			if message := strings.TrimSpace(req.GetString("decline_message", "")); message != "" {
				args = append(args, "--decline-message", message)
			}
			rrules, err := requireMCPOptionalStringArray(req, "rrule")
			if err != nil {
				return nil, err
			}
			for _, rule := range rrules {
				args = append(args, "--rrule", rule)
			}
			return append(args, "--", id), nil
		},
	}
}

func mcpCalendarOOOTool() mcpToolSpec {
	return mcpToolSpec{
		Name: "calendar_out_of_office", Service: "calendar", Risk: mcpRiskWrite,
		Description: "Create an out-of-office event. Requires RFC3339 datetimes and --allow-write.",
		Options: []mcp.ToolOption{
			mcp.WithString("from", mcp.Required()),
			mcp.WithString("to", mcp.Required()),
			mcp.WithString("calendar_id", mcp.DefaultString("primary")),
			mcp.WithString("summary", mcp.DefaultString("Out of office")),
			mcp.WithString("auto_decline", mcp.Enum("none", "all", "new"), mcp.DefaultString("all")),
			mcp.WithString("decline_message", mcp.DefaultString("I am out of office and will respond when I return.")),
		},
		BuildArgs: func(req mcp.CallToolRequest) ([]string, error) {
			from, err := requireMCPString(req, "from")
			if err != nil {
				return nil, err
			}
			to, err := requireMCPString(req, "to")
			if err != nil {
				return nil, err
			}
			if validationErr := validateMCPRFC3339("from", from); validationErr != nil {
				return nil, validationErr
			}
			if validationErr := validateMCPRFC3339("to", to); validationErr != nil {
				return nil, validationErr
			}
			id, err := mcpDefaultNonEmptyString(req, "calendar_id", "primary")
			if err != nil {
				return nil, err
			}
			summary, err := mcpDefaultNonEmptyString(req, "summary", "Out of office")
			if err != nil {
				return nil, err
			}
			message, err := mcpDefaultNonEmptyString(req, "decline_message", "I am out of office and will respond when I return.")
			if err != nil {
				return nil, err
			}
			autoDecline := strings.TrimSpace(req.GetString("auto_decline", "all"))
			if validationErr := validateMCPEnum("auto_decline", autoDecline, "none", "all", "new"); validationErr != nil {
				return nil, validationErr
			}
			return []string{"calendar", "out-of-office", "--summary", summary, "--from", from, "--to", to, "--auto-decline", autoDecline, "--decline-message", message, "--", id}, nil
		},
	}
}

func mcpCalendarWorkingLocationTool() mcpToolSpec {
	return mcpToolSpec{
		Name: "calendar_working_location", Service: "calendar", Risk: mcpRiskWrite,
		Description: "Create a working-location event. Requires date-only bounds and --allow-write.",
		Options: []mcp.ToolOption{
			mcp.WithString("from", mcp.Required()), mcp.WithString("to", mcp.Required()),
			mcp.WithString("type", mcp.Enum("home", "office", "custom"), mcp.Required()),
			mcp.WithString("calendar_id", mcp.DefaultString("primary")),
			mcp.WithString("office_label"), mcp.WithString("building_id"), mcp.WithString("floor_id"), mcp.WithString("desk_id"), mcp.WithString("custom_label"),
		},
		BuildArgs: func(req mcp.CallToolRequest) ([]string, error) {
			from, err := requireMCPString(req, "from")
			if err != nil {
				return nil, err
			}
			to, err := requireMCPString(req, "to")
			if err != nil {
				return nil, err
			}
			locationType, err := requireMCPString(req, "type")
			if err != nil {
				return nil, err
			}
			if !isMCPDateOnly(from) || !isMCPDateOnly(to) {
				return nil, fmt.Errorf("working location requires YYYY-MM-DD from and to")
			}
			locationType = strings.ToLower(locationType)
			if validationErr := validateMCPEnum("type", locationType, "home", "office", "custom"); validationErr != nil {
				return nil, validationErr
			}
			officeValues := []string{
				strings.TrimSpace(req.GetString("office_label", "")),
				strings.TrimSpace(req.GetString("building_id", "")),
				strings.TrimSpace(req.GetString("floor_id", "")),
				strings.TrimSpace(req.GetString("desk_id", "")),
			}
			hasOfficeValue := false
			for _, value := range officeValues {
				hasOfficeValue = hasOfficeValue || value != ""
			}
			customLabel := strings.TrimSpace(req.GetString("custom_label", ""))
			switch locationType {
			case "home":
				if hasOfficeValue || customLabel != "" {
					return nil, fmt.Errorf("home working location does not accept office or custom fields")
				}
			case "office":
				if customLabel != "" {
					return nil, fmt.Errorf("office working location does not accept custom_label")
				}
			case "custom":
				if customLabel == "" {
					return nil, fmt.Errorf("custom_label is required for custom working location")
				}
				if hasOfficeValue {
					return nil, fmt.Errorf("custom working location does not accept office fields")
				}
			}
			id, err := mcpDefaultNonEmptyString(req, "calendar_id", "primary")
			if err != nil {
				return nil, err
			}
			args := []string{"calendar", "working-location", "--type", locationType, "--from", from, "--to", to}
			for _, pair := range [][2]string{{"office_label", "--office-label"}, {"building_id", "--building-id"}, {"floor_id", "--floor-id"}, {"desk_id", "--desk-id"}, {"custom_label", "--custom-label"}} {
				if value := strings.TrimSpace(req.GetString(pair[0], "")); value != "" {
					args = append(args, pair[1], value)
				}
			}
			return append(args, "--", id), nil
		},
	}
}

func mcpDrivePermissionsTool() mcpToolSpec {
	return mcpToolSpec{
		Name: "drive_permissions", Service: "drive", Risk: mcpRiskRead,
		Description: "List permissions on one Google Drive file with bounded paging.",
		Options: []mcp.ToolOption{
			mcp.WithString("file_id", mcp.Description("Drive file ID"), mcp.Required()),
			mcp.WithInteger("max", mcp.Description("Maximum permissions"), mcp.DefaultNumber(100), mcp.Min(1), mcp.Max(100)),
			mcp.WithString("page_token", mcp.Description("Opaque page token")),
		},
		BuildArgs: func(req mcp.CallToolRequest) ([]string, error) {
			fileID, err := requireMCPString(req, "file_id")
			if err != nil {
				return nil, err
			}
			args := []string{"drive", "permissions", "--max", strconv.Itoa(clampMCPInt(req.GetInt("max", 100), 1, 100))}
			if token := strings.TrimSpace(req.GetString("page_token", "")); token != "" {
				args = append(args, "--page", token)
			}
			return append(args, "--", fileID), nil
		},
	}
}

func mcpDriveCreateFolderTool() mcpToolSpec {
	return mcpToolSpec{
		Name: "drive_create_folder", Service: "drive", Risk: mcpRiskWrite,
		Description: "Create a Google Drive folder; creation is not idempotent. Requires --allow-write.",
		Options: []mcp.ToolOption{
			mcp.WithString("name", mcp.Description("Folder name"), mcp.Required()),
			mcp.WithString("parent", mcp.Description("Parent folder ID")),
		},
		BuildArgs: func(req mcp.CallToolRequest) ([]string, error) {
			name, err := requireMCPString(req, "name")
			if err != nil {
				return nil, err
			}
			args := []string{"drive", "mkdir", name}
			if parent := strings.TrimSpace(req.GetString("parent", "")); parent != "" {
				args = append(args, "--parent", parent)
			}
			return args, nil
		},
	}
}

func mcpDriveRenameTool() mcpToolSpec {
	return mcpToolSpec{
		Name: "drive_rename", Service: "drive", Risk: mcpRiskWrite,
		Description: "Rename a Google Drive file or folder. Requires --allow-write.",
		Options: []mcp.ToolOption{
			mcp.WithString("file_id", mcp.Required()), mcp.WithString("new_name", mcp.Required()),
		},
		BuildArgs: func(req mcp.CallToolRequest) ([]string, error) {
			fileID, err := requireMCPString(req, "file_id")
			if err != nil {
				return nil, err
			}
			name, err := requireMCPString(req, "new_name")
			if err != nil {
				return nil, err
			}
			return []string{"drive", "rename", "--", fileID, name}, nil
		},
	}
}

func mcpDriveMoveTool() mcpToolSpec {
	return mcpToolSpec{
		Name: "drive_move", Service: "drive", Risk: mcpRiskWrite,
		Description: "Move a Drive file and replace its existing parents. Requires --allow-write.",
		Options: []mcp.ToolOption{
			mcp.WithString("file_id", mcp.Required()),
			mcp.WithString("destination_parent", mcp.Required()),
		},
		BuildArgs: func(req mcp.CallToolRequest) ([]string, error) {
			fileID, err := requireMCPString(req, "file_id")
			if err != nil {
				return nil, err
			}
			parent, err := requireMCPString(req, "destination_parent")
			if err != nil {
				return nil, err
			}
			return []string{"drive", "move", "--parent", parent, "--", fileID}, nil
		},
	}
}

func mcpDriveCopyTool() mcpToolSpec {
	return mcpToolSpec{
		Name: "drive_copy", Service: "drive", Risk: mcpRiskWrite,
		Description: "Copy a Drive file to a new name and optional parent. Requires --allow-write.",
		Options: []mcp.ToolOption{
			mcp.WithString("source_id", mcp.Required()),
			mcp.WithString("new_name", mcp.Required()),
			mcp.WithString("parent"),
		},
		BuildArgs: func(req mcp.CallToolRequest) ([]string, error) {
			sourceID, err := requireMCPString(req, "source_id")
			if err != nil {
				return nil, err
			}
			name, err := requireMCPString(req, "new_name")
			if err != nil {
				return nil, err
			}
			args := []string{"drive", "copy"}
			if parent := strings.TrimSpace(req.GetString("parent", "")); parent != "" {
				args = append(args, "--parent", parent)
			}
			return append(args, "--", sourceID, name), nil
		},
	}
}

func mcpDriveCreateShortcutTool() mcpToolSpec {
	return mcpToolSpec{
		Name: "drive_create_shortcut", Service: "drive", Risk: mcpRiskWrite,
		Description: "Create a Drive shortcut. Requires --allow-write.",
		Options: []mcp.ToolOption{
			mcp.WithString("target_id", mcp.Required()),
			mcp.WithString("parent_id", mcp.Required()),
			mcp.WithString("name"),
		},
		BuildArgs: func(req mcp.CallToolRequest) ([]string, error) {
			target, err := requireMCPString(req, "target_id")
			if err != nil {
				return nil, err
			}
			parent, err := requireMCPString(req, "parent_id")
			if err != nil {
				return nil, err
			}
			args := []string{"drive", "shortcut", "create", "--parent", parent}
			if name := strings.TrimSpace(req.GetString("name", "")); name != "" {
				args = append(args, "--name", name)
			}
			return append(args, "--", target), nil
		},
	}
}

func mcpDriveCreateCommentTool() mcpToolSpec {
	return mcpToolSpec{
		Name: "drive_create_comment", Service: "drive", Risk: mcpRiskWrite,
		Description: "Create a Drive comment from inline text. Requires --allow-write.",
		Options: []mcp.ToolOption{
			mcp.WithString("file_id", mcp.Required()),
			mcp.WithString("content", mcp.Required()),
			mcp.WithString("quoted_text"),
		},
		BuildArgs: func(req mcp.CallToolRequest) ([]string, error) {
			fileID, err := requireMCPString(req, "file_id")
			if err != nil {
				return nil, err
			}
			content, err := requireMCPText(req, "content")
			if err != nil {
				return nil, err
			}
			args := []string{"drive", "comments", "create", fileID, content}
			if quoted := strings.TrimSpace(req.GetString("quoted_text", "")); quoted != "" {
				args = append(args, "--quoted", quoted)
			}
			return args, nil
		},
	}
}

func mcpDocsCreateTool() mcpToolSpec {
	return mcpToolSpec{
		Name: "docs_create", Service: "docs", Risk: mcpRiskWrite,
		Description: "Create an empty Google Doc; use docs_write to fill it. If the post-create pageless update fails, the created Doc remains and may require cleanup. Requires --allow-write.",
		Options: []mcp.ToolOption{
			mcp.WithString("title", mcp.Required()), mcp.WithString("parent"), mcp.WithBoolean("pageless", mcp.DefaultBool(false)),
		},
		BuildArgs: func(req mcp.CallToolRequest) ([]string, error) {
			title, err := requireMCPString(req, "title")
			if err != nil {
				return nil, err
			}
			args := []string{"docs", "create"}
			if parent := strings.TrimSpace(req.GetString("parent", "")); parent != "" {
				args = append(args, "--parent", parent)
			}
			if req.GetBool("pageless", false) {
				args = append(args, "--pageless")
			}
			return append(args, "--", title), nil
		},
	}
}

func mcpSheetsCreateTool() mcpToolSpec {
	return mcpToolSpec{
		Name: "sheets_create", Service: "sheets", Risk: mcpRiskWrite,
		Description: "Create a Google spreadsheet. Parent placement is advisory and reported if it fails. Requires --allow-write.",
		Options: []mcp.ToolOption{
			mcp.WithString("title", mcp.Required()),
			mcp.WithArray("sheet_names", mcp.WithStringItems(), mcp.MinItems(1), mcp.MaxItems(100)),
			mcp.WithString("parent"),
		},
		BuildArgs: func(req mcp.CallToolRequest) ([]string, error) {
			title, err := requireMCPString(req, "title")
			if err != nil {
				return nil, err
			}
			args := []string{"sheets", "create", title}
			if _, supplied := req.GetArguments()["sheet_names"]; supplied {
				names, arrayErr := requireMCPStringArray(req, "sheet_names", 100)
				if arrayErr != nil {
					return nil, arrayErr
				}
				args = append(args, "--sheets", strings.Join(names, ","))
			}
			if parent := strings.TrimSpace(req.GetString("parent", "")); parent != "" {
				args = append(args, "--parent", parent)
			}
			return args, nil
		},
	}
}

func mcpSlidesCreateFromTemplateTool() mcpToolSpec {
	return mcpToolSpec{
		Name: "slides_create_from_template", Service: "slides", Risk: mcpRiskWrite,
		Description: "Create Slides by copying a template and applying inline replacements. Copy then replace is non-atomic. Requires --allow-write.",
		Options: []mcp.ToolOption{
			mcp.WithString("template_id", mcp.Required()),
			mcp.WithString("title", mcp.Required()),
			mcp.WithArray("replacements", mcp.WithStringItems(), mcp.Required(), mcp.MinItems(1), mcp.MaxItems(100)),
			mcp.WithString("parent"),
			mcp.WithBoolean("exact", mcp.DefaultBool(false)),
		},
		BuildArgs: func(req mcp.CallToolRequest) ([]string, error) {
			templateID, err := requireMCPString(req, "template_id")
			if err != nil {
				return nil, err
			}
			title, err := requireMCPString(req, "title")
			if err != nil {
				return nil, err
			}
			args := []string{"slides", "create-from-template", templateID, title}
			replacements, arrayErr := requireMCPStringArray(req, "replacements", 100)
			if arrayErr != nil {
				return nil, arrayErr
			}
			for _, replacement := range replacements {
				key, _, found := strings.Cut(replacement, "=")
				if !found || strings.TrimSpace(key) == "" {
					return nil, fmt.Errorf("replacements must use non-empty key=value strings")
				}
				args = append(args, "--replace", replacement)
			}
			if parent := strings.TrimSpace(req.GetString("parent", "")); parent != "" {
				args = append(args, "--parent", parent)
			}
			if req.GetBool("exact", false) {
				args = append(args, "--exact")
			}
			return args, nil
		},
	}
}

func requireMCPText(req mcp.CallToolRequest, key string) (string, error) {
	value, err := req.RequireString(key)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("empty %s", key)
	}
	return value, nil
}

func requireMCPLiteralValuesJSON(req mcp.CallToolRequest, key string) (string, error) {
	value, err := requireMCPText(req, key)
	if err != nil {
		return "", err
	}
	trimmed := strings.TrimSpace(value)
	if trimmed == "-" || strings.HasPrefix(trimmed, "@") {
		return "", fmt.Errorf("%s must be literal JSON, not stdin or @file input", key)
	}
	rangeSpec := strings.TrimSpace(req.GetString("range", ""))
	rows, err := sheetsvalues.DecodeStrictForRange([]byte(trimmed), rangeSpec)
	if err != nil {
		return "", fmt.Errorf("invalid %s JSON 2D array: %w", key, err)
	}
	canonical, err := json.Marshal(rows)
	if err != nil {
		return "", fmt.Errorf("canonicalize %s: %w", key, err)
	}
	return string(canonical), nil
}
