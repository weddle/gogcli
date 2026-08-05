package cmd

import (
	"encoding/base64"
	"encoding/json"
	"mime"
	"strings"
	"unicode/utf8"

	"github.com/mark3labs/mcp-go/mcp"
)

// mcpInlineBinaryMaxBytes is the fixed raw-content ceiling for an inline MCP
// binary result. The limit is inclusive: a payload of exactly this size is
// valid, while the next byte fails closed before it is encoded.
const mcpInlineBinaryMaxBytes = 65_536

const mcpInlineBinaryMaxNameBytes = 255

type mcpInlineBinaryContent struct {
	Name          string `json:"name"`
	MimeType      string `json:"mimeType"`
	Size          int    `json:"size"`
	ContentBase64 string `json:"contentBase64"`
}

type mcpInlineBinaryError struct {
	Error      string `json:"error"`
	LimitBytes int    `json:"limit_bytes,omitempty"`
}

// mcpInlineBinaryToolResult encodes raw bytes as one bounded, ephemeral
// tools/call result. It deliberately does not create a resource URI, a temp
// file, or a host path. The structured content and its JSON text fallback are
// the same four-field object so stdio clients with either capability observe
// the same payload.
func mcpInlineBinaryToolResult(raw []byte, name, mimeType string, maxOutputBytes int) *mcp.CallToolResult {
	if len(raw) > mcpInlineBinaryMaxBytes {
		return mcpInlineBinaryErrorResult("binary_size_limit", mcpInlineBinaryMaxBytes)
	}

	mimeType, ok := validMCPInlineBinaryMimeType(mimeType)
	if !ok {
		return mcpInlineBinaryErrorResult("invalid_mime_type", 0)
	}

	content := mcpInlineBinaryContent{
		Name:          normalizeMCPInlineBinaryName(name),
		MimeType:      mimeType,
		Size:          len(raw),
		ContentBase64: base64.StdEncoding.EncodeToString(raw),
	}

	// maxOutputBytes is the child stdout cap. Check the exact JSON object that
	// would be emitted before constructing the MCP result, and never return a
	// partially encoded contentBase64 value.
	encoded, err := json.Marshal(content)
	if err != nil || maxOutputBytes <= 0 || len(encoded) > maxOutputBytes {
		return mcpInlineBinaryErrorResult("binary_output_limit", maxOutputBytes)
	}

	return mcp.NewToolResultStructuredOnly(content)
}

func mcpInlineBinaryErrorResult(code string, limit int) *mcp.CallToolResult {
	result := mcp.NewToolResultStructuredOnly(mcpInlineBinaryError{
		Error:      code,
		LimitBytes: limit,
	})
	result.IsError = true
	return result
}

func validMCPInlineBinaryMimeType(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false
	}
	mediaType, _, err := mime.ParseMediaType(value)
	if err != nil || mediaType == "" || !strings.Contains(mediaType, "/") || strings.Contains(mediaType, "*") {
		return "", false
	}
	return value, true
}

func normalizeMCPInlineBinaryName(value string) string {
	value = strings.ToValidUTF8(value, "\uFFFD")
	if index := strings.LastIndexAny(value, `/\\`); index >= 0 {
		value = value[index+1:]
	}
	if value == "" || value == "." || value == ".." {
		value = "download"
	}
	value = truncateMCPInlineBinaryUTF8(value, mcpInlineBinaryMaxNameBytes)
	if value == "" {
		return "download"
	}
	return value
}

func truncateMCPInlineBinaryUTF8(value string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	for len(value) > maxBytes {
		_, size := utf8.DecodeLastRuneInString(value)
		if size <= 0 {
			return ""
		}
		value = value[:len(value)-size]
	}
	return value
}
