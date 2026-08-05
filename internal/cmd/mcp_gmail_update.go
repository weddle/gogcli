package cmd

import (
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
)

func mcpGmailUpdateDraftTool() mcpToolSpec {
	return mcpToolSpec{
		Name: "gmail_update_draft", Service: "gmail", Risk: mcpRiskWrite,
		Description: "Update one Gmail draft by rebuilding its full MIME message from inline text or HTML. An omitted to recipient, existing attachments, and reply lineage are preserved; the draft is never sent. Requires --allow-write.",
		Options: []mcp.ToolOption{
			mcp.WithString("draft_id", mcp.Description("Gmail draft ID"), mcp.Required()),
			mcp.WithString("to", mcp.Description("Recipients (comma-separated; omit to keep existing)")),
			mcp.WithString("cc", mcp.Description("CC recipients (comma-separated)")),
			mcp.WithString("bcc", mcp.Description("BCC recipients (comma-separated)")),
			mcp.WithString("subject", mcp.Description("Subject (required unless replying/threading)")),
			mcp.WithString("body", mcp.Description("Plain-text body")),
			mcp.WithString("body_html", mcp.Description("HTML body")),
			mcp.WithString("reply_to_message_id", mcp.Description("Reply-to Gmail message ID")),
			mcp.WithString("thread_id", mcp.Description("Existing Gmail thread ID")),
			mcp.WithBoolean("reply_all", mcp.Description("Reply to all; requires a reply target"), mcp.DefaultBool(false)),
			mcp.WithString("reply_to", mcp.Description("Reply-To header")),
			mcp.WithBoolean("auto_from_addressed_alias", mcp.Description("Use the verified alias addressed by the original message when replying"), mcp.DefaultBool(false)),
		},
		BuildArgs: func(req mcp.CallToolRequest) ([]string, error) {
			draftID, err := requireMCPString(req, "draft_id")
			if err != nil {
				return nil, err
			}
			reqArgs := req.GetArguments()
			optionalString := func(key string, preserveWhitespace bool) (string, bool, error) {
				raw, provided := reqArgs[key]
				if !provided {
					return "", false, nil
				}
				value, ok := raw.(string)
				if !ok {
					return "", true, fmt.Errorf("%s must be a string", key)
				}
				if strings.TrimSpace(value) == "" {
					return "", true, fmt.Errorf("empty %s", key)
				}
				if preserveWhitespace {
					return value, true, nil
				}
				return strings.TrimSpace(value), true, nil
			}
			optionalBool := func(key string) (bool, error) {
				raw, provided := reqArgs[key]
				if !provided {
					return false, nil
				}
				value, ok := raw.(bool)
				if !ok {
					return false, fmt.Errorf("%s must be a boolean", key)
				}
				return value, nil
			}

			to, toProvided, err := optionalString("to", false)
			if err != nil {
				return nil, err
			}
			cc, ccProvided, err := optionalString("cc", false)
			if err != nil {
				return nil, err
			}
			bcc, bccProvided, err := optionalString("bcc", false)
			if err != nil {
				return nil, err
			}
			subject, subjectProvided, err := optionalString("subject", false)
			if err != nil {
				return nil, err
			}
			body, bodyProvided, err := optionalString("body", true)
			if err != nil {
				return nil, err
			}
			bodyHTML, bodyHTMLProvided, err := optionalString("body_html", true)
			if err != nil {
				return nil, err
			}
			replyID, replyIDProvided, err := optionalString("reply_to_message_id", false)
			if err != nil {
				return nil, err
			}
			threadID, threadIDProvided, err := optionalString("thread_id", false)
			if err != nil {
				return nil, err
			}
			replyTo, replyToProvided, err := optionalString("reply_to", false)
			if err != nil {
				return nil, err
			}

			if !bodyProvided && !bodyHTMLProvided {
				return nil, fmt.Errorf("body or body_html is required")
			}
			if !subjectProvided && replyID == "" && threadID == "" {
				return nil, fmt.Errorf("subject required unless reply_to_message_id or thread_id is set")
			}
			if replyIDProvided && threadIDProvided {
				return nil, fmt.Errorf("reply_to_message_id and thread_id are mutually exclusive")
			}
			replyAll, err := optionalBool("reply_all")
			if err != nil {
				return nil, err
			}
			if replyAll && replyID == "" && threadID == "" {
				return nil, fmt.Errorf("reply_all requires reply_to_message_id or thread_id")
			}
			autoFromAddressedAlias, err := optionalBool("auto_from_addressed_alias")
			if err != nil {
				return nil, err
			}

			args := []string{"gmail", "drafts", "update"}
			appendOptional := func(flag, value string, provided bool) {
				if provided {
					args = append(args, flag+"="+value)
				}
			}
			appendOptional("--to", to, toProvided)
			appendOptional("--cc", cc, ccProvided)
			appendOptional("--bcc", bcc, bccProvided)
			appendOptional("--subject", subject, subjectProvided)
			if bodyProvided {
				args = append(args, "--body="+body)
			}
			if bodyHTMLProvided {
				args = append(args, "--body-html="+bodyHTML)
			}
			if replyIDProvided {
				args = append(args, "--reply-to-message-id="+replyID)
			}
			if threadIDProvided {
				args = append(args, "--thread-id="+threadID)
			}
			if replyToProvided {
				args = append(args, "--reply-to="+replyTo)
			}
			if replyAll {
				args = append(args, "--reply-all")
			}
			if autoFromAddressedAlias {
				args = append(args, "--auto-from-addressed-alias")
			}
			return append(args, "--", draftID), nil
		},
	}
}
