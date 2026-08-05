package cmd

import (
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestMCPE06SlidesFilesystemLockoutSelectors(t *testing.T) {
	selectors := []struct {
		name string
		cmd  McpCmd
	}{
		{name: "default", cmd: McpCmd{}},
		{name: "read", cmd: McpCmd{AllowTool: []string{"read"}}},
		{name: "write", cmd: McpCmd{AllowWrite: true, AllowTool: []string{"write"}}},
		{name: "slides", cmd: McpCmd{AllowWrite: true, AllowTool: []string{"slides"}}},
		{name: "slides wildcard", cmd: McpCmd{AllowWrite: true, AllowTool: []string{"slides.*"}}},
		{name: "template exact", cmd: McpCmd{AllowWrite: true, AllowTool: []string{"slides_create_from_template"}}},
		{name: "markdown exact", cmd: McpCmd{AllowWrite: true, AllowTool: []string{"slides_create_from_markdown"}}},
		{name: "destructive", cmd: McpCmd{AllowWrite: true, AllowTool: []string{"destructive"}}},
		{name: "star", cmd: McpCmd{AllowWrite: true, AllowTool: []string{"*"}}},
		{name: "all", cmd: McpCmd{AllowWrite: true, AllowTool: []string{"all"}}},
	}

	for _, spec := range mcpAllTools() {
		if strings.Contains(strings.ToLower(spec.Name), "markdown") {
			t.Fatalf("create-from-markdown-like tool is registered: %q", spec.Name)
		}
	}
	for _, selector := range selectors {
		t.Run(selector.name, func(t *testing.T) {
			for _, tool := range mcpEnabledTools(selector.cmd) {
				if strings.Contains(strings.ToLower(tool.Name), "markdown") {
					t.Fatalf("selector %#v exposed filesystem-backed Slides tool %q", selector.cmd.AllowTool, tool.Name)
				}
			}
			if hasMCPTool(mcpEnabledTools(selector.cmd), "slides_create_from_markdown") {
				t.Fatalf("selector %#v exposed slides_create_from_markdown", selector.cmd.AllowTool)
			}
		})
	}
}

func TestMCPE06SlidesTemplateSchemaExcludesFilesystemAndGenericFields(t *testing.T) {
	tool := findMCPTool(t, "slides_create_from_template")
	mcpTool := newMCPTool(tool)
	if closed, ok := mcpTool.InputSchema.AdditionalProperties.(bool); !ok || closed {
		t.Fatalf("Slides template schema is not closed: %#v", mcpTool.InputSchema.AdditionalProperties)
	}

	properties := mcpTool.InputSchema.Properties
	for _, field := range []string{
		"replacements_file", "replacement_file", "replacements_path", "replacement_path",
		"markdown", "markdown_file", "markdown_path",
		"source", "source_file", "source_path",
		"path", "local_path", "content", "content_file", "content_path",
		"file", "input_file", "output_path",
		"stdin", "@file", "at_file",
		"argv", "args", "command", "generic", "raw",
	} {
		if _, exposed := properties[field]; exposed {
			t.Errorf("Slides template schema exposes forbidden field %q", field)
		}
	}

	valid := map[string]any{
		"template_id":  "template-1",
		"title":        "Deck",
		"replacements": []any{"name=Ryan"},
	}
	for _, field := range []string{
		"replacements_file", "replacement_file", "replacements_path", "replacement_path",
		"markdown", "markdown_file", "markdown_path",
		"source", "source_file", "source_path",
		"path", "local_path", "content", "content_file", "content_path",
		"file", "input_file", "output_path",
		"stdin", "@file", "at_file",
		"argv", "args", "command", "generic", "raw",
	} {
		arguments := make(map[string]any, len(valid)+1)
		for key, value := range valid {
			arguments[key] = value
		}
		switch field {
		case "argv", "args":
			arguments[field] = []any{"arbitrary", "command"}
		case "stdin":
			arguments[field] = true
		default:
			arguments[field] = "/tmp/forbidden"
		}
		result, calls := callMCPWaveANativeSchema(t, tool.Name, arguments)
		if !result.IsError {
			t.Errorf("schema accepted forbidden field %q: %#v", field, arguments)
		}
		if calls != 0 {
			t.Errorf("forbidden field %q reached handler %d times", field, calls)
		}
		if !strings.Contains(mcpResultText(result), field) {
			t.Errorf("error for field %q = %q", field, mcpResultText(result))
		}
	}
}

func TestMCPE06SlidesTemplateBuildsOnlyInlineReplacementArgv(t *testing.T) {
	tests := []struct {
		name string
		args map[string]any
		want []string
	}{
		{
			name: "inline replacement",
			args: map[string]any{
				"template_id":  "template-1",
				"title":        "Deck",
				"replacements": []string{"name=Ryan"},
			},
			want: []string{"slides", "create-from-template", "template-1", "Deck", "--replace", "name=Ryan"},
		},
		{
			name: "inline replacements with parent and exact",
			args: map[string]any{
				"template_id":  "template-1",
				"title":        "Deck",
				"replacements": []string{"name=Ryan", "company=Gog"},
				"parent":       "parent-1",
				"exact":        true,
			},
			want: []string{
				"slides", "create-from-template", "template-1", "Deck",
				"--replace", "name=Ryan", "--replace", "company=Gog",
				"--parent", "parent-1", "--exact",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args, err := findMCPTool(t, "slides_create_from_template").BuildArgs(mcp.CallToolRequest{
				Params: mcp.CallToolParams{Arguments: tt.args},
			})
			if err != nil {
				t.Fatalf("BuildArgs: %v", err)
			}
			assertMCPWaveANativeArgv(t, args, tt.want)
			for _, forbidden := range []string{
				"--replacements", "--replacements-file", "--content", "--content-file",
				"--markdown", "--source", "--path", "--stdin", "--file", "--mmdc",
				"@file", "@-", " stdin ", "argv", "args",
			} {
				if strings.Contains("\x00"+strings.Join(args, "\x00")+"\x00", "\x00"+forbidden+"\x00") {
					t.Fatalf("argv contains forbidden filesystem/generic input %q: %#v", forbidden, args)
				}
			}
		})
	}
}
