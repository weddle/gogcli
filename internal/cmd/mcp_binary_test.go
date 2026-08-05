package cmd

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestMCPInlineBinaryToolResultUsesDirectStructuredContent(t *testing.T) {
	raw := []byte{0xfb, 0xef}
	result := mcpInlineBinaryToolResult(raw, `../report\\final.pdf`, "application/pdf", 1024)
	if result.IsError {
		t.Fatalf("unexpected error: %#v", result.StructuredContent)
	}
	content, ok := result.StructuredContent.(mcpInlineBinaryContent)
	if !ok {
		t.Fatalf("structured content type = %T", result.StructuredContent)
	}
	if content.Name != "final.pdf" || content.MimeType != "application/pdf" || content.Size != len(raw) {
		t.Fatalf("content metadata = %#v", content)
	}
	if content.ContentBase64 != "++8=" {
		t.Fatalf("unexpected standard base64 %q", content.ContentBase64)
	}
	if got, err := base64.StdEncoding.DecodeString(content.ContentBase64); err != nil || !bytes.Equal(got, raw) {
		t.Fatalf("decoded content = %x, err = %v", got, err)
	}
	if len(result.Content) != 1 {
		t.Fatalf("fallback content count = %d", len(result.Content))
	}
	text, ok := result.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("fallback content type = %T", result.Content[0])
	}
	var fallback map[string]any
	if err := json.Unmarshal([]byte(text.Text), &fallback); err != nil {
		t.Fatalf("fallback is not JSON: %v", err)
	}
	if len(fallback) != 4 || fallback["name"] != "final.pdf" || fallback["mimeType"] != "application/pdf" || fallback["size"] != float64(len(raw)) || fallback["contentBase64"] != content.ContentBase64 {
		t.Fatalf("fallback JSON = %#v", fallback)
	}
}

func TestMCPInlineBinaryToolResultRawLimitBoundaries(t *testing.T) {
	for _, tt := range []struct {
		name    string
		size    int
		isError bool
		code    string
	}{
		{name: "cap minus one", size: mcpInlineBinaryMaxBytes - 1},
		{name: "cap", size: mcpInlineBinaryMaxBytes},
		{name: "cap plus one", size: mcpInlineBinaryMaxBytes + 1, isError: true, code: "binary_size_limit"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			result := mcpInlineBinaryToolResult(bytes.Repeat([]byte{0x5a}, tt.size), "file.bin", "application/octet-stream", 102400)
			if result.IsError != tt.isError {
				t.Fatalf("IsError = %v, want %v; content = %#v", result.IsError, tt.isError, result.StructuredContent)
			}
			if !tt.isError {
				content, ok := result.StructuredContent.(mcpInlineBinaryContent)
				if !ok || content.Size != tt.size {
					t.Fatalf("content = %#v", result.StructuredContent)
				}
				return
			}
			errContent, ok := result.StructuredContent.(mcpInlineBinaryError)
			if !ok || errContent.Error != tt.code || errContent.LimitBytes != mcpInlineBinaryMaxBytes {
				t.Fatalf("error content = %#v", result.StructuredContent)
			}
			if strings.Contains(mcpResultText(result), "contentBase64") {
				t.Fatalf("over-limit result leaked content: %s", mcpResultText(result))
			}
		})
	}
}

func TestMCPInlineBinaryToolResultRejectsInvalidMIME(t *testing.T) {
	for _, mimeType := range []string{"", "application", "application/", "*/*", "application/pdf; bad"} {
		t.Run(mimeType, func(t *testing.T) {
			result := mcpInlineBinaryToolResult([]byte("data"), "file.bin", mimeType, 1024)
			if !result.IsError {
				t.Fatalf("invalid MIME accepted: %#v", result.StructuredContent)
			}
			errContent, ok := result.StructuredContent.(mcpInlineBinaryError)
			if !ok || errContent.Error != "invalid_mime_type" {
				t.Fatalf("error content = %#v", result.StructuredContent)
			}
			if strings.Contains(mcpResultText(result), "contentBase64") {
				t.Fatalf("invalid MIME result leaked content: %s", mcpResultText(result))
			}
		})
	}
}

func TestMCPInlineBinaryToolResultOutputCapIsAtomic(t *testing.T) {
	raw := []byte("bounded payload")
	want := mcpInlineBinaryContent{
		Name:          "file.bin",
		MimeType:      "application/octet-stream",
		Size:          len(raw),
		ContentBase64: base64.StdEncoding.EncodeToString(raw),
	}
	encoded, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name      string
		cap       int
		wantError bool
	}{
		{name: "exact payload cap", cap: len(encoded)},
		{name: "one byte under payload cap", cap: len(encoded) - 1, wantError: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			result := mcpInlineBinaryToolResult(raw, "file.bin", "application/octet-stream", tt.cap)
			if result.IsError != tt.wantError {
				t.Fatalf("IsError = %v, want %v; content = %#v", result.IsError, tt.wantError, result.StructuredContent)
			}
			if tt.wantError {
				if _, ok := result.StructuredContent.(mcpInlineBinaryError); !ok {
					t.Fatalf("structured content = %#v", result.StructuredContent)
				}
				if strings.Contains(mcpResultText(result), "contentBase64") {
					t.Fatalf("output-cap result leaked content: %s", mcpResultText(result))
				}
			}
		})
	}
}

func TestMCPInlineBinaryToolResultNormalizesLogicalName(t *testing.T) {
	for _, tt := range []struct {
		name string
		want string
	}{
		{name: "", want: "download"},
		{name: ".", want: "download"},
		{name: "..", want: "download"},
		{name: "/tmp/remote.bin", want: "remote.bin"},
		{name: `C:\\tmp\\remote.bin`, want: "remote.bin"},
		{name: strings.Repeat("é", 200), want: strings.Repeat("é", 127)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			result := mcpInlineBinaryToolResult(nil, tt.name, "application/octet-stream", 1024)
			if result.IsError {
				t.Fatalf("unexpected error: %#v", result.StructuredContent)
			}
			content := result.StructuredContent.(mcpInlineBinaryContent)
			if content.Name != tt.want {
				t.Fatalf("name = %q, want %q", content.Name, tt.want)
			}
			if len(content.Name) > mcpInlineBinaryMaxNameBytes || !utf8Valid(content.Name) {
				t.Fatalf("unsafe name %q", content.Name)
			}
		})
	}
}

func utf8Valid(value string) bool {
	return strings.ToValidUTF8(value, "") == value
}
