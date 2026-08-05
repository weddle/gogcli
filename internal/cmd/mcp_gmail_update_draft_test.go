package cmd

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"google.golang.org/api/gmail/v1"
)

func mcpGmailUpdateDraftArgs(t *testing.T, args map[string]any) ([]string, error) {
	t.Helper()
	return mcpGmailUpdateDraftTool().BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{
		Arguments: args,
	}})
}

func TestMCPGmailUpdateDraftBuildArgsFull(t *testing.T) {
	args, err := mcpGmailUpdateDraftArgs(t, map[string]any{
		"draft_id":                  "draft_123",
		"to":                        " a@example.com, b@example.com ",
		"cc":                        " c@example.com ",
		"bcc":                       " d@example.com ",
		"subject":                   "  About the plan  ",
		"body":                      "  Line one\n\n  Line two  ",
		"body_html":                 "<p>hi</p>",
		"reply_to_message_id":       "msg_9",
		"reply_to":                  " reply@example.com ",
		"reply_all":                 true,
		"auto_from_addressed_alias": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"gmail", "drafts", "update",
		"--to=a@example.com, b@example.com",
		"--cc=c@example.com",
		"--bcc=d@example.com",
		"--subject=About the plan",
		"--body=  Line one\n\n  Line two  ",
		"--body-html=<p>hi</p>",
		"--reply-to-message-id=msg_9",
		"--reply-to=reply@example.com",
		"--reply-all",
		"--auto-from-addressed-alias",
		"--", "draft_123",
	}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("args = %#v\nwant = %#v", args, want)
	}
}

// Headers are trimmed; body whitespace is preserved verbatim so MIME carries it.
func TestMCPGmailUpdateDraftTrimsHeadersPreservesBody(t *testing.T) {
	args, err := mcpGmailUpdateDraftArgs(t, map[string]any{
		"draft_id": "d1",
		"subject":  "  --subj  ",
		"body":     "---\n b  ",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"gmail", "drafts", "update", "--subject=--subj", "--body=---\n b  ", "--", "d1"}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("args = %#v, want %#v", args, want)
	}
}

// Omitted recipient and reply fields must produce no corresponding flag, so
// the CLI preserves the draft's existing recipients, reply lineage, and attachments.
func TestMCPGmailUpdateDraftOmittedFieldsPreserved(t *testing.T) {
	args, err := mcpGmailUpdateDraftArgs(t, map[string]any{
		"draft_id": "d1",
		"body":     "new body",
		"subject":  "Subject",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, absent := range []string{
		"--to", "--cc", "--bcc", "--reply-to-message-id",
		"--thread-id", "--reply-to", "--reply-all", "--auto-from-addressed-alias",
		"--from", "--attach", "--clear-attachments", "--body-file",
		"--body-html-file", "--clear-reply-context", "--quote",
	} {
		for _, a := range args {
			if a == absent || strings.HasPrefix(a, absent+"=") {
				t.Fatalf("omitted field produced %s in %#v", absent, args)
			}
		}
	}
	want := []string{"gmail", "drafts", "update", "--subject=Subject", "--body=new body", "--", "d1"}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("args = %#v, want %#v", args, want)
	}
}

func TestMCPGmailUpdateDraftPreservesRecipientsAndAttachments(t *testing.T) {
	var posted gmail.Draft
	attachmentFetched := false
	srv := draftUpdateAttachmentServer(t, &posted, &attachmentFetched)
	defer srv.Close()

	svc := newGmailServiceFromServer(t, srv)
	result := runMCPGmailCLI(t, "gmail_update_draft", map[string]any{
		"draft_id": "d1",
		"subject":  "Subject",
		"body":     "new body",
	}, svc)
	if result.err != nil {
		t.Fatalf("update draft: %v\nstderr=%s", result.err, result.stderr)
	}
	if !attachmentFetched {
		t.Fatal("existing attachment bytes were not fetched for preservation")
	}
	if posted.Message == nil {
		t.Fatal("update omitted rebuilt message")
	}
	raw, err := base64.RawURLEncoding.DecodeString(posted.Message.Raw)
	if err != nil {
		t.Fatalf("decode updated MIME: %v", err)
	}
	mime := string(raw)
	for _, preserved := range []string{"To: keep@example.com", `filename="report.pdf"`, base64.StdEncoding.EncodeToString([]byte("HELLO"))} {
		if !strings.Contains(mime, preserved) {
			t.Fatalf("updated MIME missing preserved %q:\n%s", preserved, mime)
		}
	}
	out := decodeMCPGmailJSON(t, result.stdout)
	if out["draftId"] != "d1" {
		t.Fatalf("structured update result = %#v", out)
	}
}

// Updating with only an HTML body must map to --body-html (delegating whole-message
// MIME rebuild and preserved attachment bytes/stored HTML handling to the CLI).
func TestMCPGmailUpdateDraftHTMLOnlyMapsToBodyHTML(t *testing.T) {
	args, err := mcpGmailUpdateDraftArgs(t, map[string]any{
		"draft_id":  "d1",
		"subject":   "s",
		"body_html": "<b>rich</b>",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"gmail", "drafts", "update", "--subject=s", "--body-html=<b>rich</b>", "--", "d1"}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("args = %#v, want %#v", args, want)
	}
}

func TestMCPGmailUpdateDraftRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name    string
		args    map[string]any
		wantErr string
	}{
		{"missing draft_id", map[string]any{"body": "x"}, "draft_id"},
		{"empty draft_id", map[string]any{"draft_id": "  ", "body": "x"}, "draft_id"},
		{"missing body", map[string]any{"draft_id": "d1", "subject": "s"}, "body or body_html is required"},
		{"missing subject without target", map[string]any{"draft_id": "d1", "body": "x"}, "subject required"},
		{"empty body", map[string]any{"draft_id": "d1", "subject": "s", "body": "   "}, "empty body"},
		{"empty body_html", map[string]any{"draft_id": "d1", "subject": "s", "body_html": "  "}, "empty body_html"},
		{"empty subject", map[string]any{"draft_id": "d1", "subject": " ", "body": "x"}, "empty subject"},
		{"target xor both", map[string]any{
			"draft_id": "d1", "subject": "s", "body": "x",
			"reply_to_message_id": "m1", "thread_id": "t1",
		}, "mutually exclusive"},
		{"reply_all without target", map[string]any{
			"draft_id": "d1", "subject": "s", "body": "x", "reply_all": true,
		}, "reply_all requires"},
		{"wrong body type", map[string]any{"draft_id": "d1", "subject": "s", "body": 42}, "body must be a string"},
		{"wrong boolean type", map[string]any{
			"draft_id": "d1", "subject": "s", "body": "x", "reply_all": "yes",
		}, "reply_all must be a boolean"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := mcpGmailUpdateDraftArgs(t, tt.args)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %q, want containing %q", err, tt.wantErr)
			}
		})
	}
}

// Subject is optional when a reply target is present.
func TestMCPGmailUpdateDraftSubjectOptionalWithTarget(t *testing.T) {
	for _, target := range []map[string]any{
		{"reply_to_message_id": "m1"},
		{"thread_id": "t1"},
	} {
		args := map[string]any{"draft_id": "d1", "body": "x"}
		for k, v := range target {
			args[k] = v
		}
		got, err := mcpGmailUpdateDraftArgs(t, args)
		if err != nil {
			t.Fatalf("expected subject optional with target: %v", err)
		}
		for _, a := range got {
			if a == "--subject" {
				t.Fatalf("subject emitted though optional with target: %#v", got)
			}
		}
	}
}

// The advertised schema is closed and exposes only the documented typed fields,
// with no send, filesystem, or attachment surface.
func TestMCPGmailUpdateDraftSchemaClosedNoSendFilesystemAttach(t *testing.T) {
	tool := mcpGmailUpdateDraftTool()
	if tool.Service != "gmail" || tool.Risk != mcpRiskWrite {
		t.Fatalf("service/risk = %q/%q, want gmail/write", tool.Service, tool.Risk)
	}
	if tool.Name != "gmail_update_draft" {
		t.Fatalf("tool name = %q", tool.Name)
	}
	schema := newMCPTool(tool).InputSchema
	if schema.AdditionalProperties != false {
		t.Fatalf("schema additionalProperties = %v, want false (closed)", schema.AdditionalProperties)
	}
	if !reflect.DeepEqual(schema.Required, []string{"draft_id"}) {
		t.Fatalf("required = %#v, want only draft_id", schema.Required)
	}
	wantFields := map[string]bool{
		"draft_id": true, "to": true, "cc": true, "bcc": true, "subject": true,
		"body": true, "body_html": true, "reply_to_message_id": true,
		"thread_id": true, "reply_to": true, "reply_all": true,
		"auto_from_addressed_alias": true,
	}
	if len(schema.Properties) != len(wantFields) {
		t.Fatalf("properties = %#v, want exactly %d fields", schema.Properties, len(wantFields))
	}
	for field := range schema.Properties {
		if !wantFields[field] {
			t.Fatalf("unexpected schema field %q (no send/filesystem/attachment allowed)", field)
		}
	}
	for field := range wantFields {
		if _, ok := schema.Properties[field]; !ok {
			t.Fatalf("missing schema field %q", field)
		}
	}
}

// Every accepted argv is exactly `gmail drafts update ... -- <draft_id>` with a
// positional draft id — never `gmail send`.
func TestMCPGmailUpdateDraftNeverSends(t *testing.T) {
	inputs := []map[string]any{
		{"draft_id": "d1", "subject": "s", "body": "x"},
		{"draft_id": "d1", "subject": "s", "body": "x", "reply_to_message_id": "m1", "reply_all": true},
		{"draft_id": "d1", "body_html": "<p>x</p>", "thread_id": "t1"},
		{"draft_id": "d1", "subject": "s", "body": "x", "auto_from_addressed_alias": true},
	}
	for i, args := range inputs {
		got, err := mcpGmailUpdateDraftArgs(t, args)
		if err != nil {
			t.Fatalf("case %d: %v", i, err)
		}
		joined := strings.Join(got, " ")
		if strings.Contains(joined, "gmail send") {
			t.Fatalf("case %d: produced a send command: %#v", i, got)
		}
		if !strings.HasPrefix(joined, "gmail drafts update") {
			t.Fatalf("case %d: command = %#v", i, got)
		}
		if len(got) < 2 || got[len(got)-2] != "--" || strings.TrimSpace(got[len(got)-1]) == "" {
			t.Fatalf("case %d: expected trailing `-- <draft_id>`: %#v", i, got)
		}
	}
}

// Upstream PR #957 landed the #955 contributor work. Exercise the real
// draft-update path against a rich existing draft. The advisory must remain
// on stderr while stdout stays structured JSON.
func TestMCPGmailUpdateDraftWarningStaysOnStderr(t *testing.T) {
	svc, closeService := newGmailServiceForTest(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/users/me/drafts/d1"):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "d1",
				"message": map[string]any{
					"id": "m1",
					"payload": map[string]any{
						"mimeType": "multipart/alternative",
						"headers":  []map[string]any{{"name": "To", "value": "recipient@example.com"}},
						"parts": []map[string]any{
							{"mimeType": "text/plain", "body": map[string]any{"data": base64.RawURLEncoding.EncodeToString([]byte("old plain"))}},
							{"mimeType": "text/html", "body": map[string]any{"data": base64.RawURLEncoding.EncodeToString([]byte("<p>old rich</p>"))}},
						},
					},
				},
			})
		case r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/users/me/drafts/d1"):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "d1", "message": map[string]any{"id": "m2"}})
		case strings.HasSuffix(r.URL.Path, "/messages/send") || strings.HasSuffix(r.URL.Path, "/drafts/send"):
			t.Fatalf("update contacted send endpoint: %s", r.URL.Path)
		default:
			http.NotFound(w, r)
		}
	})
	defer closeService()

	result := runMCPGmailCLI(t, "gmail_update_draft", map[string]any{
		"draft_id": "d1",
		"to":       "recipient@example.com",
		"subject":  "Subject",
		"body":     "replacement plain text",
	}, svc)
	if result.err != nil {
		t.Fatalf("update draft: %v\nstderr=%s", result.err, result.stderr)
	}
	if !strings.Contains(result.stderr, "Warning: draft has an HTML body") {
		t.Fatalf("rich-draft warning missing from stderr: %q", result.stderr)
	}
	out := decodeMCPGmailJSON(t, result.stdout)
	if out["draftId"] != "d1" || out["message"] == nil {
		t.Fatalf("structured stdout = %#v", out)
	}
}
