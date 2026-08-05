package cmd

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"google.golang.org/api/gmail/v1"
)

func TestMCPWaveBGmailUpdateDraftSchemaAndArgv(t *testing.T) {
	assertMCPGmailSchema(t, "gmail_update_draft", []string{"draft_id"}, []string{
		"draft_id", "to", "cc", "bcc", "subject", "body", "body_html",
		"reply_to_message_id", "thread_id", "reply_all", "reply_to", "auto_from_addressed_alias",
	})
	for _, excluded := range []string{
		"body_file", "body_html_file", "attach", "attachments", "clear_attachments",
		"from", "quote", "clear_reply_context", "send", "path", "local_path",
		"stdin", "argv", "args", "account", "home", "client",
	} {
		result, calls := callMCPGmailSchema(t, "gmail_update_draft", map[string]any{
			"draft_id": "d1", "subject": "S", "body": "B", excluded: true,
		})
		if !result.IsError || calls != 0 || !strings.Contains(mcpResultText(result), excluded) {
			t.Fatalf("excluded field %q result = %#v, handler calls = %d", excluded, result.Content, calls)
		}
	}

	body := "  plain body\n\n"
	html := " <p>html body</p>\n"
	assertMCPGmailArgv(t, "gmail_update_draft", map[string]any{
		"draft_id":                  " d1 ",
		"to":                        " to@example.com ",
		"cc":                        " cc@example.com ",
		"bcc":                       " bcc@example.com ",
		"subject":                   " Subject ",
		"body":                      body,
		"body_html":                 html,
		"reply_to_message_id":       " m1 ",
		"reply_to":                  " reply@example.com ",
		"reply_all":                 true,
		"auto_from_addressed_alias": true,
	}, []string{
		"gmail", "drafts", "update",
		"--to", "to@example.com", "--cc", "cc@example.com", "--bcc", "bcc@example.com",
		"--subject", "Subject", "--body", body, "--body-html", html,
		"--reply-to-message-id", "m1", "--reply-to", "reply@example.com",
		"--reply-all", "--auto-from-addressed-alias", "--", "d1",
	})
	assertMCPGmailArgv(t, "gmail_update_draft", map[string]any{
		"draft_id": "d1", "thread_id": "thread-1", "body": "reply body",
	}, []string{
		"gmail", "drafts", "update", "--body", "reply body", "--thread-id", "thread-1", "--", "d1",
	})
	assertMCPGmailArgv(t, "gmail_update_draft", map[string]any{
		"draft_id": "d1", "subject": "S", "body": "B",
	}, []string{
		"gmail", "drafts", "update", "--subject", "S", "--body", "B", "--", "d1",
	})

	tool := findMCPTool(t, "gmail_update_draft")
	for _, tc := range []struct {
		name string
		args map[string]any
		text string
	}{
		{name: "missing draft ID", args: map[string]any{"subject": "S", "body": "B"}, text: "draft_id"},
		{name: "missing body", args: map[string]any{"draft_id": "d1", "subject": "S"}, text: "body or body_html"},
		{name: "missing subject", args: map[string]any{"draft_id": "d1", "body": "B"}, text: "subject required"},
		{name: "both reply targets", args: map[string]any{"draft_id": "d1", "body": "B", "reply_to_message_id": "m1", "thread_id": "t1"}, text: "mutually exclusive"},
		{name: "reply all without target", args: map[string]any{"draft_id": "d1", "subject": "S", "body": "B", "reply_all": true}, text: "reply_all"},
		{name: "explicit empty recipient", args: map[string]any{"draft_id": "d1", "subject": "S", "body": "B", "to": " \t"}, text: "empty to"},
		{name: "explicit empty body", args: map[string]any{"draft_id": "d1", "subject": "S", "body": ""}, text: "empty body"},
	} {
		t.Run("invalid/"+tc.name, func(t *testing.T) {
			_, err := tool.BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: tc.args}})
			if err == nil || !strings.Contains(err.Error(), tc.text) {
				t.Fatalf("BuildArgs error = %v, want text %q", err, tc.text)
			}
		})
	}
	for _, tc := range []struct {
		name string
		args map[string]any
		text string
	}{
		{name: "wrong body type", args: map[string]any{"draft_id": "d1", "subject": "S", "body": 12}, text: "body"},
		{name: "wrong reply-all type", args: map[string]any{"draft_id": "d1", "subject": "S", "body": "B", "reply_all": "true"}, text: "reply_all"},
		{name: "unknown field", args: map[string]any{"draft_id": "d1", "subject": "S", "body": "B", "unknown": true}, text: "unknown"},
	} {
		t.Run("schema/"+tc.name, func(t *testing.T) {
			result, calls := callMCPGmailSchema(t, "gmail_update_draft", tc.args)
			if !result.IsError || calls != 0 || !strings.Contains(mcpResultText(result), tc.text) {
				t.Fatalf("result = %#v, handler calls = %d, want schema rejection containing %q", result.Content, calls, tc.text)
			}
		})
	}
}

func TestMCPWaveBGmailUpdateDraftPreservesMIMEAndStructuredResult(t *testing.T) {
	const body = "  updated body\nsecond line\n\n"
	const html = " <p>updated HTML</p>\n"
	var posted gmail.Draft
	var attachmentFetches, updateCalls, sendCalls int
	svc, closeService := newGmailServiceForTest(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/users/me/settings/sendAs"):
			_ = json.NewEncoder(w).Encode(map[string]any{"sendAs": []map[string]any{}})
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/users/me/drafts/d1"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "d1",
				"message": map[string]any{
					"id": "m-old", "threadId": "thread-old",
					"payload": map[string]any{
						"mimeType": "multipart/mixed",
						"headers": []map[string]any{
							{"name": "To", "value": "keep@example.com"},
							{"name": "Cc", "value": "clear-cc@example.com"},
							{"name": "Bcc", "value": "clear-bcc@example.com"},
							{"name": "In-Reply-To", "value": "<original@example.com>"},
							{"name": "References", "value": "<root@example.com> <original@example.com>"},
						},
						"parts": []map[string]any{
							{"mimeType": "text/plain", "body": map[string]any{"data": encodeBase64URL("old body")}},
							{"filename": "report.pdf", "mimeType": "application/pdf", "body": map[string]any{"attachmentId": "att1", "size": 5}},
						},
					},
				},
			})
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/users/me/messages/m-old/attachments/att1"):
			attachmentFetches++
			_ = json.NewEncoder(w).Encode(map[string]any{"data": encodeBase64URL("HELLO"), "size": 5})
		case r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/users/me/drafts/d1"):
			updateCalls++
			if err := json.NewDecoder(r.Body).Decode(&posted); err != nil {
				t.Fatalf("decode draft update: %v", err)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "d1", "message": map[string]any{"id": "m-new", "threadId": "thread-old"},
			})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/send"):
			sendCalls++
			http.Error(w, "draft update must not send", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	})
	defer closeService()

	result := runMCPGmailCLI(t, "gmail_update_draft", map[string]any{
		"draft_id": "d1", "subject": "Updated", "body": body, "body_html": html,
	}, svc)
	if result.err != nil {
		t.Fatalf("update draft: %v\nstderr=%s", result.err, result.stderr)
	}
	if attachmentFetches != 1 || updateCalls != 1 || sendCalls != 0 {
		t.Fatalf("API calls attachment=%d update=%d send=%d, want 1/1/0", attachmentFetches, updateCalls, sendCalls)
	}
	if posted.Id != "d1" || posted.Message == nil || posted.Message.Raw == "" {
		t.Fatalf("posted draft = %#v, want draft ID and raw MIME", posted)
	}
	raw, err := base64.RawURLEncoding.DecodeString(posted.Message.Raw)
	if err != nil {
		t.Fatalf("decode updated MIME: %v", err)
	}
	mime := string(raw)
	for _, want := range []string{
		"To: keep@example.com",
		"Subject: Updated",
		"In-Reply-To: <original@example.com>",
		"References: <root@example.com> <original@example.com>",
		strings.ReplaceAll(body, "\n", "\r\n"),
		strings.ReplaceAll(html, "\n", "\r\n"),
		`filename="report.pdf"`,
		"SEVMTE8=",
	} {
		if !strings.Contains(mime, want) {
			t.Fatalf("updated MIME missing %q:\n%s", want, mime)
		}
	}
	for _, cleared := range []string{"clear-cc@example.com", "clear-bcc@example.com"} {
		if strings.Contains(mime, cleared) {
			t.Fatalf("updated MIME retained omitted Cc/Bcc recipient %q:\n%s", cleared, mime)
		}
	}
	out := decodeMCPGmailJSON(t, result.stdout)
	if out["draftId"] != "d1" || out["threadId"] != "thread-old" {
		t.Fatalf("structured update result = %#v", out)
	}
	if out["inReplyTo"] != "<original@example.com>" || out["references"] != "<root@example.com> <original@example.com>" {
		t.Fatalf("structured reply lineage = %#v", out)
	}
	attachments := mapSlice(t, out["attachments"], "attachments")
	if len(attachments) != 1 || attachments[0]["filename"] != "report.pdf" || attachments[0]["size"] != float64(5) {
		t.Fatalf("structured attachment metadata = %#v", out["attachments"])
	}
	if !strings.Contains(result.stderr, "reply headers preserved") {
		t.Fatalf("expected preserved-reply diagnostic on stderr, got %q", result.stderr)
	}
}
