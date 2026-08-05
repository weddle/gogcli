package cmd

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"

	"google.golang.org/api/docs/v1"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/sheets/v4"
	"google.golang.org/api/slides/v1"

	"github.com/steipete/gogcli/internal/app"
)

func TestMCPWaveANativeDriveBuildArgs(t *testing.T) {
	tests := []struct {
		name string
		tool string
		args map[string]any
		want []string
	}{
		{
			name: "list defaults",
			tool: "drive_list_folder",
			args: map[string]any{},
			want: []string{"drive", "ls", "--max", "20"},
		},
		{
			name: "list clamps and pages with shared drives disabled",
			tool: "drive_list_folder",
			args: map[string]any{
				"folder_id":             " folder-1 ",
				"max":                   0,
				"page_token":            " page-2 ",
				"include_shared_drives": false,
			},
			want: []string{"drive", "ls", "--max", "1", "--page", "page-2", "--no-all-drives", "--parent", "folder-1"},
		},
		{
			name: "list upper bound",
			tool: "drive_list_folder",
			args: map[string]any{"max": 1000},
			want: []string{"drive", "ls", "--max", "100"},
		},
		{
			name: "permissions defaults",
			tool: "drive_permissions",
			args: map[string]any{"file_id": " file-1 "},
			want: []string{"drive", "permissions", "--max", "100", "--", "file-1"},
		},
		{
			name: "permissions page",
			tool: "drive_permissions",
			args: map[string]any{"file_id": "file-1", "max": 0, "page_token": " token-2 "},
			want: []string{"drive", "permissions", "--max", "1", "--page", "token-2", "--", "file-1"},
		},
		{
			name: "create folder root",
			tool: "drive_create_folder",
			args: map[string]any{"name": "Folder"},
			want: []string{"drive", "mkdir", "Folder"},
		},
		{
			name: "create folder parent",
			tool: "drive_create_folder",
			args: map[string]any{"name": " Folder ", "parent": " parent-1 "},
			want: []string{"drive", "mkdir", "Folder", "--parent", "parent-1"},
		},
		{
			name: "rename",
			tool: "drive_rename",
			args: map[string]any{"file_id": " file-1 ", "new_name": " New name "},
			want: []string{"drive", "rename", "--", "file-1", "New name"},
		},
		{
			name: "move",
			tool: "drive_move",
			args: map[string]any{"file_id": "file-1", "destination_parent": " parent-2 "},
			want: []string{"drive", "move", "--parent", "parent-2", "--", "file-1"},
		},
		{
			name: "copy without parent",
			tool: "drive_copy",
			args: map[string]any{"source_id": "file-1", "new_name": "Copy"},
			want: []string{"drive", "copy", "--", "file-1", "Copy"},
		},
		{
			name: "copy with parent",
			tool: "drive_copy",
			args: map[string]any{"source_id": "file-1", "new_name": "Copy", "parent": "parent-2"},
			want: []string{"drive", "copy", "--parent", "parent-2", "--", "file-1", "Copy"},
		},
		{
			name: "shortcut with optional name",
			tool: "drive_create_shortcut",
			args: map[string]any{"target_id": "file-1", "parent_id": "parent-2", "name": "Shortcut"},
			want: []string{"drive", "shortcut", "create", "--parent", "parent-2", "--name", "Shortcut", "--", "file-1"},
		},
		{
			name: "shortcut omits empty name",
			tool: "drive_create_shortcut",
			args: map[string]any{"target_id": "file-1", "parent_id": "parent-2"},
			want: []string{"drive", "shortcut", "create", "--parent", "parent-2", "--", "file-1"},
		},
		{
			name: "comment preserves literal content whitespace",
			tool: "drive_create_comment",
			args: map[string]any{"file_id": "file-1", "content": "  keep spaces  ", "quoted_text": " quoted "},
			want: []string{"drive", "comments", "create", "file-1", "  keep spaces  ", "--quoted", "quoted"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args, err := findMCPTool(t, tt.tool).BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: tt.args}})
			if err != nil {
				t.Fatalf("BuildArgs: %v", err)
			}
			assertMCPWaveANativeArgv(t, args, tt.want)
		})
	}
}

func TestMCPWaveANativeDocsSheetsSlidesBuildArgs(t *testing.T) {
	tests := []struct {
		name string
		tool string
		args map[string]any
		want []string
	}{
		{
			name: "docs create defaults",
			tool: "docs_create",
			args: map[string]any{"title": "Doc"},
			want: []string{"docs", "create", "--", "Doc"},
		},
		{
			name: "docs create parent and pageless",
			tool: "docs_create",
			args: map[string]any{"title": " Doc ", "parent": " parent-1 ", "pageless": true},
			want: []string{"docs", "create", "--parent", "parent-1", "--pageless", "--", "Doc"},
		},
		{
			name: "sheets create title only",
			tool: "sheets_create",
			args: map[string]any{"title": "Budget"},
			want: []string{"sheets", "create", "Budget"},
		},
		{
			name: "sheets create tabs and parent",
			tool: "sheets_create",
			args: map[string]any{"title": " Budget ", "sheet_names": []string{"One", " Two "}, "parent": " parent-1 "},
			want: []string{"sheets", "create", "Budget", "--sheets", "One,Two", "--parent", "parent-1"},
		},
		{
			name: "slides create default exact mode",
			tool: "slides_create_from_template",
			args: map[string]any{
				"template_id": " template-1 ",
				"title":       " Deck ",
				"replacements": []string{
					"name=Ryan",
				},
			},
			want: []string{"slides", "create-from-template", "template-1", "Deck", "--replace", "name=Ryan"},
		},
		{
			name: "slides create repeated replacements parent exact",
			tool: "slides_create_from_template",
			args: map[string]any{
				"template_id":  "template-1",
				"title":        "Deck",
				"replacements": []string{"name=Ryan", "company=Gog"},
				"parent":       " parent-1 ",
				"exact":        true,
			},
			want: []string{"slides", "create-from-template", "template-1", "Deck", "--replace", "name=Ryan", "--replace", "company=Gog", "--parent", "parent-1", "--exact"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args, err := findMCPTool(t, tt.tool).BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: tt.args}})
			if err != nil {
				t.Fatalf("BuildArgs: %v", err)
			}
			assertMCPWaveANativeArgv(t, args, tt.want)
		})
	}
}

func TestMCPWaveANativePagingAndArrayBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name string
		tool string
		args map[string]any
		want []string
	}{
		{
			name: "folder max lower bound",
			tool: "drive_list_folder",
			args: map[string]any{"max": -7},
			want: []string{"drive", "ls", "--max", "1"},
		},
		{
			name: "folder max upper bound",
			tool: "drive_list_folder",
			args: map[string]any{"max": 101},
			want: []string{"drive", "ls", "--max", "100"},
		},
		{
			name: "permissions max lower bound",
			tool: "drive_permissions",
			args: map[string]any{"file_id": "file-1", "max": -1},
			want: []string{"drive", "permissions", "--max", "1", "--", "file-1"},
		},
		{
			name: "permissions max upper bound",
			tool: "drive_permissions",
			args: map[string]any{"file_id": "file-1", "max": 101},
			want: []string{"drive", "permissions", "--max", "100", "--", "file-1"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args, err := findMCPTool(t, tc.tool).BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: tc.args}})
			if err != nil {
				t.Fatalf("BuildArgs: %v", err)
			}
			assertMCPWaveANativeArgv(t, args, tc.want)
		})
	}

	oneHundredNames := make([]string, 100)
	for i := range oneHundredNames {
		oneHundredNames[i] = "tab"
	}
	oneHundredReplacements := make([]string, 100)
	for i := range oneHundredReplacements {
		oneHundredReplacements[i] = "key=value"
	}

	t.Run("sheets create accepts one hundred tabs", func(t *testing.T) {
		args, err := findMCPTool(t, "sheets_create").BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: map[string]any{
			"title": "Budget", "sheet_names": oneHundredNames,
		}}})
		if err != nil {
			t.Fatalf("BuildArgs: %v", err)
		}
		if len(args) != 5 || args[3] != "--sheets" || strings.Count(args[4], "tab") != 100 {
			t.Fatalf("argv = %#v, want one --sheets value with 100 tabs", args)
		}
	})
	t.Run("sheets create rejects one hundred one tabs", func(t *testing.T) {
		tooMany := append(append([]string(nil), oneHundredNames...), "overflow")
		if _, err := findMCPTool(t, "sheets_create").BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: map[string]any{
			"title": "Budget", "sheet_names": tooMany,
		}}}); err == nil {
			t.Fatal("expected 101 sheet names to be rejected")
		}
	})
	t.Run("slides create accepts one hundred replacements", func(t *testing.T) {
		args, err := findMCPTool(t, "slides_create_from_template").BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: map[string]any{
			"template_id": "template-1", "title": "Deck", "replacements": oneHundredReplacements,
		}}})
		if err != nil {
			t.Fatalf("BuildArgs: %v", err)
		}
		if got := strings.Count(strings.Join(args, "\x00"), "--replace"); got != 100 {
			t.Fatalf("replacement flag count = %d, want 100; argv=%#v", got, args)
		}
	})
	t.Run("slides create rejects one hundred one replacements", func(t *testing.T) {
		tooMany := append(append([]string(nil), oneHundredReplacements...), "overflow=value")
		if _, err := findMCPTool(t, "slides_create_from_template").BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: map[string]any{
			"template_id": "template-1", "title": "Deck", "replacements": tooMany,
		}}}); err == nil {
			t.Fatal("expected 101 replacements to be rejected")
		}
	})
}

func TestMCPWaveANativeSchemaRejectsTaskFields(t *testing.T) {
	tests := []struct {
		name  string
		tool  string
		valid map[string]any
		bad   []struct {
			name string
			args map[string]any
			want string
		}
	}{
		{
			name:  "drive list folder",
			tool:  "drive_list_folder",
			valid: map[string]any{},
			bad: []struct {
				name string
				args map[string]any
				want string
			}{
				{name: "unknown query", args: map[string]any{"query": "text"}, want: "query"},
				{name: "wrong max", args: map[string]any{"max": "20"}, want: "max"},
				{name: "excluded all", args: map[string]any{"all": true}, want: "all"},
				{name: "excluded path", args: map[string]any{"path": "/tmp/file"}, want: "path"},
			},
		},
		{
			name:  "drive permissions",
			tool:  "drive_permissions",
			valid: map[string]any{"file_id": "file-1"},
			bad: []struct {
				name string
				args map[string]any
				want string
			}{
				{name: "missing file id", args: map[string]any{}, want: "file_id"},
				{name: "wrong max", args: map[string]any{"file_id": "file-1", "max": "100"}, want: "max"},
				{name: "unknown permission id", args: map[string]any{"file_id": "file-1", "permission_id": "perm-1"}, want: "permission_id"},
				{name: "excluded stdin", args: map[string]any{"file_id": "file-1", "stdin": true}, want: "stdin"},
			},
		},
		{
			name:  "drive create folder",
			tool:  "drive_create_folder",
			valid: map[string]any{"name": "Folder"},
			bad: []struct {
				name string
				args map[string]any
				want string
			}{
				{name: "missing name", args: map[string]any{}, want: "name"},
				{name: "wrong parent", args: map[string]any{"name": "Folder", "parent": false}, want: "parent"},
				{name: "unknown argv", args: map[string]any{"name": "Folder", "argv": []any{"drive", "mkdir"}}, want: "argv"},
			},
		},
		{
			name:  "drive rename",
			tool:  "drive_rename",
			valid: map[string]any{"file_id": "file-1", "new_name": "Name"},
			bad: []struct {
				name string
				args map[string]any
				want string
			}{
				{name: "missing file id", args: map[string]any{"new_name": "Name"}, want: "file_id"},
				{name: "wrong new name", args: map[string]any{"file_id": "file-1", "new_name": 3}, want: "new_name"},
				{name: "unknown parent", args: map[string]any{"file_id": "file-1", "new_name": "Name", "parent": "p"}, want: "parent"},
			},
		},
		{
			name:  "drive move",
			tool:  "drive_move",
			valid: map[string]any{"file_id": "file-1", "destination_parent": "parent-1"},
			bad: []struct {
				name string
				args map[string]any
				want string
			}{
				{name: "missing destination", args: map[string]any{"file_id": "file-1"}, want: "destination_parent"},
				{name: "wrong destination", args: map[string]any{"file_id": "file-1", "destination_parent": 3}, want: "destination_parent"},
				{name: "noncanonical parent", args: map[string]any{"file_id": "file-1", "parent": "parent-1"}, want: "parent"},
			},
		},
		{
			name:  "drive copy",
			tool:  "drive_copy",
			valid: map[string]any{"source_id": "file-1", "new_name": "Copy"},
			bad: []struct {
				name string
				args map[string]any
				want string
			}{
				{name: "missing source", args: map[string]any{"new_name": "Copy"}, want: "source_id"},
				{name: "wrong parent", args: map[string]any{"source_id": "file-1", "new_name": "Copy", "parent": 1}, want: "parent"},
				{name: "noncanonical file id", args: map[string]any{"source_id": "file-1", "new_name": "Copy", "file_id": "other"}, want: "file_id"},
			},
		},
		{
			name:  "drive shortcut",
			tool:  "drive_create_shortcut",
			valid: map[string]any{"target_id": "file-1", "parent_id": "parent-1"},
			bad: []struct {
				name string
				args map[string]any
				want string
			}{
				{name: "missing target", args: map[string]any{"parent_id": "parent-1"}, want: "target_id"},
				{name: "wrong optional name", args: map[string]any{"target_id": "file-1", "parent_id": "parent-1", "name": true}, want: "name"},
				{name: "noncanonical parent", args: map[string]any{"target_id": "file-1", "parent": "parent-1"}, want: "parent"},
			},
		},
		{
			name:  "drive comment",
			tool:  "drive_create_comment",
			valid: map[string]any{"file_id": "file-1", "content": "Comment"},
			bad: []struct {
				name string
				args map[string]any
				want string
			}{
				{name: "missing content", args: map[string]any{"file_id": "file-1"}, want: "content"},
				{name: "wrong quoted text", args: map[string]any{"file_id": "file-1", "content": "Comment", "quoted_text": false}, want: "quoted_text"},
				{name: "excluded anchor", args: map[string]any{"file_id": "file-1", "content": "Comment", "anchor": "text"}, want: "anchor"},
			},
		},
		{
			name:  "docs get",
			tool:  "docs_get",
			valid: map[string]any{"document_id": "doc-1"},
			bad: []struct {
				name string
				args map[string]any
				want string
			}{
				{name: "missing document id", args: map[string]any{}, want: "document_id"},
				{name: "wrong document id", args: map[string]any{"document_id": 7}, want: "document_id"},
				{name: "wrong tab", args: map[string]any{"document_id": "doc-1", "tab": true}, want: "tab"},
				{name: "wrong all tabs", args: map[string]any{"document_id": "doc-1", "all_tabs": "true"}, want: "all_tabs"},
				{name: "wrong max bytes", args: map[string]any{"document_id": "doc-1", "max_bytes": "200"}, want: "max_bytes"},
				{name: "negative max bytes", args: map[string]any{"document_id": "doc-1", "max_bytes": -1}, want: "max_bytes"},
				{name: "excluded raw", args: map[string]any{"document_id": "doc-1", "raw": true}, want: "raw"},
				{name: "excluded path", args: map[string]any{"document_id": "doc-1", "path": "/tmp/doc"}, want: "path"},
				{name: "generic argv", args: map[string]any{"document_id": "doc-1", "argv": []any{"docs", "cat"}}, want: "argv"},
			},
		},
		{
			name:  "docs create",
			tool:  "docs_create",
			valid: map[string]any{"title": "Doc"},
			bad: []struct {
				name string
				args map[string]any
				want string
			}{
				{name: "missing title", args: map[string]any{}, want: "title"},
				{name: "wrong pageless", args: map[string]any{"title": "Doc", "pageless": "true"}, want: "pageless"},
				{name: "excluded file", args: map[string]any{"title": "Doc", "file": "/tmp/doc.md"}, want: "file"},
				{name: "excluded content", args: map[string]any{"title": "Doc", "content": "text"}, want: "content"},
			},
		},
		{
			name:  "sheets create",
			tool:  "sheets_create",
			valid: map[string]any{"title": "Sheet"},
			bad: []struct {
				name string
				args map[string]any
				want string
			}{
				{name: "missing title", args: map[string]any{}, want: "title"},
				{name: "wrong tabs", args: map[string]any{"title": "Sheet", "sheet_names": "One"}, want: "sheet_names"},
				{name: "unknown sheets alias", args: map[string]any{"title": "Sheet", "sheets": "One"}, want: "sheets"},
				{name: "excluded path", args: map[string]any{"title": "Sheet", "path": "/tmp/file"}, want: "path"},
			},
		},
		{
			name:  "slides create from template",
			tool:  "slides_create_from_template",
			valid: map[string]any{"template_id": "template-1", "title": "Deck", "replacements": []any{"name=Ryan"}},
			bad: []struct {
				name string
				args map[string]any
				want string
			}{
				{name: "missing template", args: map[string]any{"title": "Deck", "replacements": []any{"name=Ryan"}}, want: "template_id"},
				{name: "missing replacements", args: map[string]any{"template_id": "template-1", "title": "Deck"}, want: "replacements"},
				{name: "wrong replacement array", args: map[string]any{"template_id": "template-1", "title": "Deck", "replacements": "name=Ryan"}, want: "replacements"},
				{name: "excluded replacement file", args: map[string]any{"template_id": "template-1", "title": "Deck", "replacements": []any{"name=Ryan"}, "replacements_file": "/tmp/replacements.json"}, want: "replacements_file"},
				{name: "excluded markdown", args: map[string]any{"template_id": "template-1", "title": "Deck", "replacements": []any{"name=Ryan"}, "markdown": "# Deck"}, want: "markdown"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, bad := range tt.bad {
				t.Run(bad.name, func(t *testing.T) {
					result, calls := callMCPWaveANativeSchema(t, tt.tool, bad.args)
					if !result.IsError {
						t.Fatalf("schema accepted invalid input: %#v", bad.args)
					}
					if !strings.Contains(mcpResultText(result), bad.want) {
						t.Fatalf("error = %q, want %q", mcpResultText(result), bad.want)
					}
					if calls != 0 {
						t.Fatalf("invalid input reached handler %d times", calls)
					}
				})
			}

			result, calls := callMCPWaveANativeSchema(t, tt.tool, tt.valid)
			if result.IsError {
				t.Fatalf("valid input rejected: %q", mcpResultText(result))
			}
			if calls != 1 {
				t.Fatalf("valid input reached handler %d times, want 1", calls)
			}
		})
	}

	for _, tc := range []struct {
		name string
		tool string
		args map[string]any
		want string
	}{
		{name: "slides replacement format", tool: "slides_create_from_template", args: map[string]any{"template_id": "template-1", "title": "Deck", "replacements": []string{"missing-equals"}}, want: "key=value"},
		{name: "drive rename empty id", tool: "drive_rename", args: map[string]any{"file_id": " ", "new_name": "Name"}, want: "file_id"},
		{name: "drive comment empty content", tool: "drive_create_comment", args: map[string]any{"file_id": "file-1", "content": " \t "}, want: "content"},
		{name: "docs create empty title", tool: "docs_create", args: map[string]any{"title": " \t "}, want: "title"},
		{name: "sheets create empty title", tool: "sheets_create", args: map[string]any{"title": " \t "}, want: "title"},
	} {
		t.Run("BuildArgs/"+tc.name, func(t *testing.T) {
			_, err := findMCPTool(t, tc.tool).BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: tc.args}})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("BuildArgs error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestMCPWaveANativeDocsGetConflictsRejectBeforeHandler(t *testing.T) {
	tests := []struct {
		name string
		args map[string]any
		want string
	}{
		{
			name: "tab and all tabs",
			args: map[string]any{"document_id": "doc-1", "tab": "Overview", "all_tabs": true},
			want: "mutually exclusive",
		},
		{
			name: "explicit empty tab",
			args: map[string]any{"document_id": "doc-1", "tab": " \t "},
			want: "tab cannot be empty",
		},
		{
			name: "explicit empty document id",
			args: map[string]any{"document_id": " \t "},
			want: "document_id",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, calls := callMCPWaveANativeBuildArgs(t, "docs_get", tt.args)
			if !result.IsError {
				t.Fatalf("conflicting/empty input was accepted: %#v", tt.args)
			}
			if calls != 0 {
				t.Fatalf("invalid input reached task handler %d times", calls)
			}
			if !strings.Contains(mcpResultText(result), tt.want) {
				t.Fatalf("error = %q, want %q", mcpResultText(result), tt.want)
			}
		})
	}
}

func TestMCPM06DocsGetStructuredResultsThroughRunner(t *testing.T) {
	tests := []struct {
		name      string
		mode      string
		arguments map[string]any
		rawArgs   []string
		check     func(*testing.T, any)
	}{
		{
			name:      "default text envelope",
			mode:      "default",
			arguments: map[string]any{"document_id": "doc-1"},
			check: func(t *testing.T, value any) {
				t.Helper()
				stdout := mcpNativeObject(t, value, "docs default stdout")
				if stdout["text"] != "default text" {
					t.Fatalf("default stdout = %#v", stdout)
				}
			},
		},
		{
			name:      "selected tab envelope",
			mode:      "selected-tab",
			arguments: map[string]any{"document_id": "doc-1", "tab": "Overview"},
			check: func(t *testing.T, value any) {
				t.Helper()
				stdout := mcpNativeObject(t, value, "docs selected-tab stdout")
				tab := mcpNativeObject(t, stdout["tab"], "docs selected-tab result")
				if tab["id"] != "tab-overview" || tab["title"] != "Overview" || tab["text"] != "overview text" {
					t.Fatalf("selected-tab result = %#v", tab)
				}
			},
		},
		{
			name:      "all tabs envelope",
			mode:      "all-tabs",
			arguments: map[string]any{"document_id": "doc-1", "all_tabs": true},
			check: func(t *testing.T, value any) {
				t.Helper()
				stdout := mcpNativeObject(t, value, "docs all-tabs stdout")
				tabs := mcpNativeObjects(t, stdout["tabs"], "docs all-tabs result")
				if len(tabs) != 2 {
					t.Fatalf("all-tabs result = %#v", stdout)
				}
				if tabs[0]["id"] != "tab-overview" || tabs[0]["title"] != "Overview" ||
					tabs[1]["id"] != "tab-details" || tabs[1]["title"] != "Details" {
					t.Fatalf("all-tabs tabs = %#v", tabs)
				}
			},
		},
		{
			name:      "max bytes envelope",
			mode:      "max-bytes",
			arguments: map[string]any{"document_id": "doc-1", "max_bytes": 5},
			check: func(t *testing.T, value any) {
				t.Helper()
				stdout := mcpNativeObject(t, value, "docs max-bytes stdout")
				if stdout["text"] != "01234" {
					t.Fatalf("max-bytes stdout = %#v", stdout)
				}
			},
		},
		{
			name:    "raw envelope",
			mode:    "raw",
			rawArgs: []string{"docs", "cat", "--raw", "--", "doc-1"},
			check: func(t *testing.T, value any) {
				t.Helper()
				stdout := mcpNativeObject(t, value, "docs raw stdout")
				if stdout["documentId"] != "doc-1" || stdout["title"] != "Raw document" {
					t.Fatalf("raw stdout = %#v", stdout)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("GOG_MCP_NATIVE_DOCS_GET_HELPER", "1")
			t.Setenv("GOG_MCP_NATIVE_DOCS_GET_MODE", tt.mode)
			if tt.rawArgs == nil {
				if _, err := findMCPTool(t, "docs_get").BuildArgs(mcp.CallToolRequest{
					Params: mcp.CallToolParams{Arguments: tt.arguments},
				}); err != nil {
					t.Fatalf("BuildArgs: %v", err)
				}
			}
			result := mcpRunGogTool(t.Context(), mcpRunOptions{
				self:           os.Args[0],
				tool:           findMCPTool(t, "docs_get"),
				commandArgs:    []string{"-test.run=TestMCPNativeDocsGetRunnerHelper$"},
				timeout:        5 * time.Second,
				maxOutputBytes: 4096,
			})
			got := requireMCPNativeCommandResult(t, result)
			if result.IsError || got.ExitCode != 0 {
				t.Fatalf("docs_get result = %#v", got)
			}
			if got.Tool != "docs_get" || got.Service != "docs" || got.Risk != string(mcpRiskRead) {
				t.Fatalf("docs_get metadata = %#v", got)
			}
			if got.Stderr != "" {
				t.Fatalf("docs_get stderr = %q", got.Stderr)
			}
			tt.check(t, got.Stdout)
		})
	}
}

func TestMCPM06DocsGetRawEnvelopeHonorsOutputCap(t *testing.T) {
	t.Setenv("GOG_MCP_NATIVE_DOCS_GET_HELPER", "1")
	t.Setenv("GOG_MCP_NATIVE_DOCS_GET_MODE", "raw")
	result := mcpRunGogTool(t.Context(), mcpRunOptions{
		self:           os.Args[0],
		tool:           findMCPTool(t, "docs_get"),
		commandArgs:    []string{"-test.run=TestMCPNativeDocsGetRunnerHelper$"},
		timeout:        5 * time.Second,
		maxOutputBytes: 64,
	})
	got := requireMCPNativeCommandResult(t, result)
	if result.IsError || got.ExitCode != 0 {
		t.Fatalf("raw capped result = %#v", got)
	}
	raw, ok := got.Stdout.(string)
	if !ok {
		t.Fatalf("raw capped stdout type = %T, value=%#v", got.Stdout, got.Stdout)
	}
	if !strings.Contains(raw, "... [output truncated]") {
		t.Fatalf("raw capped stdout = %q, want truncation marker", raw)
	}
	if got.Stderr != "" {
		t.Fatalf("raw capped stderr = %q", got.Stderr)
	}
}

func TestMCPWaveANativePolicySelectors(t *testing.T) {
	readTools := []string{"drive_list_folder", "drive_permissions", "docs_get", "sheets_read_range"}
	writeTools := []string{
		"drive_create_folder",
		"drive_rename",
		"drive_move",
		"drive_copy",
		"drive_create_shortcut",
		"drive_create_comment",
		"docs_create",
		"sheets_create",
		"slides_create_from_template",
	}

	for _, toolName := range readTools {
		t.Run("read/"+toolName, func(t *testing.T) {
			if !hasMCPTool(mcpEnabledTools(McpCmd{}), toolName) {
				t.Fatalf("default read tools omit %q", toolName)
			}
			if !hasMCPTool(mcpEnabledTools(McpCmd{AllowTool: []string{"read"}}), toolName) {
				t.Fatalf("explicit read selector omits %q", toolName)
			}
		})
	}

	for _, toolName := range writeTools {
		t.Run("write/"+toolName, func(t *testing.T) {
			if hasMCPTool(mcpEnabledTools(McpCmd{}), toolName) {
				t.Fatalf("write tool %q exposed by default", toolName)
			}
			if hasMCPTool(mcpEnabledTools(McpCmd{AllowWrite: true, AllowTool: []string{toolName}}), toolName) == false {
				t.Fatalf("exact selector omitted %q", toolName)
			}

			service := findMCPTool(t, toolName).Service
			for _, selector := range []string{service, service + ".*", "write", "all"} {
				if !hasMCPTool(mcpEnabledTools(McpCmd{AllowWrite: true, AllowTool: []string{selector}}), toolName) {
					t.Fatalf("selector %q omitted %q", selector, toolName)
				}
			}
			if hasMCPTool(mcpEnabledTools(McpCmd{AllowWrite: true, AllowTool: []string{toolName}}), "drive_delete") {
				t.Fatal("forbidden destructive Drive tool unexpectedly registered")
			}
		})
	}
}

func TestMCPE03NativeExclusionsIncludeExplicitReadSelector(t *testing.T) {
	selectors := []McpCmd{
		{},
		{AllowTool: []string{"read"}},
		{AllowWrite: true, AllowTool: []string{"write"}},
		{AllowWrite: true, AllowTool: []string{"drive"}},
		{AllowWrite: true, AllowTool: []string{"drive.*"}},
		{AllowWrite: true, AllowTool: []string{"destructive"}},
		{AllowWrite: true, AllowTool: []string{"all"}},
	}
	for _, selector := range selectors {
		tools := mcpEnabledTools(selector)
		for _, forbidden := range []string{"drive_upload", "drive_download", "drive_delete", "drive_trash", "drive_share", "drive_unshare"} {
			if hasMCPTool(tools, forbidden) {
				t.Fatalf("selector %#v exposed forbidden tool %q", selector.AllowTool, forbidden)
			}
		}
	}

	for _, spec := range mcpAllTools() {
		if spec.Service != "drive" && spec.Service != "docs" && spec.Service != "sheets" && spec.Service != "slides" {
			continue
		}
		properties := newMCPTool(spec).InputSchema.Properties
		for _, field := range []string{
			"local_path", "path", "stdin", "argv", "output_path", "permanent", "file", "input_file",
			"replacements_file", "markdown_file", "content_file",
		} {
			if _, ok := properties[field]; ok {
				t.Fatalf("tool %q exposes excluded field %q", spec.Name, field)
			}
		}
	}

	calls := []struct {
		tool string
		args map[string]any
	}{
		{tool: "drive_list_folder", args: map[string]any{}},
		{tool: "drive_permissions", args: map[string]any{"file_id": "file-1"}},
		{tool: "drive_search", args: map[string]any{"query": "text"}},
		{tool: "drive_get", args: map[string]any{"file_id": "file-1"}},
		{tool: "drive_create_folder", args: map[string]any{"name": "Folder"}},
		{tool: "drive_rename", args: map[string]any{"file_id": "file-1", "new_name": "Name"}},
		{tool: "drive_move", args: map[string]any{"file_id": "file-1", "destination_parent": "parent-1"}},
		{tool: "drive_copy", args: map[string]any{"source_id": "file-1", "new_name": "Copy"}},
		{tool: "drive_create_shortcut", args: map[string]any{"target_id": "file-1", "parent_id": "parent-1"}},
		{tool: "drive_create_comment", args: map[string]any{"file_id": "file-1", "content": "Comment"}},
		{tool: "docs_get", args: map[string]any{"document_id": "doc-1"}},
		{tool: "docs_write", args: map[string]any{"document_id": "doc-1", "text": "Text"}},
		{tool: "docs_create", args: map[string]any{"title": "Doc"}},
		{tool: "sheets_read_range", args: map[string]any{"spreadsheet_id": "sheet-1", "range": "Sheet1!A1"}},
		{tool: "sheets_update_range", args: map[string]any{"spreadsheet_id": "sheet-1", "range": "Sheet1!A1", "values_json": `[[1]]`}},
		{tool: "sheets_create", args: map[string]any{"title": "Sheet"}},
		{tool: "slides_create_from_template", args: map[string]any{"template_id": "template-1", "title": "Deck", "replacements": []string{"key=value"}}},
	}
	for _, call := range calls {
		t.Run("argv/"+call.tool, func(t *testing.T) {
			args, err := findMCPTool(t, call.tool).BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: call.args}})
			if err != nil {
				t.Fatalf("BuildArgs: %v", err)
			}
			joined := strings.Join(args, "\x00")
			for _, forbidden := range []string{
				"drive\x00upload", "drive\x00download", "drive\x00delete", "--permanent", "--file", "@file", "stdin",
			} {
				if strings.Contains(joined, forbidden) {
					t.Fatalf("argv contains excluded operation/input %q: %#v", forbidden, args)
				}
			}
		})
	}
}

func TestMCPM10SheetsUpdateConcreteRangeBounds(t *testing.T) {
	tool := findMCPTool(t, "sheets_update_range")
	tests := []struct {
		name      string
		rangeSpec string
		values    string
		wantErr   string
		wantArg   string
	}{
		{name: "exact concrete matrix", rangeSpec: "Sheet1!A1:B2", values: `[[1,2],[3,4]]`, wantArg: `[[1,2],[3,4]]`},
		{name: "one row within concrete range", rangeSpec: "Sheet1!A1:B2", values: `[[1,2]]`, wantArg: `[[1,2]]`},
		{name: "one column within concrete range", rangeSpec: "Sheet1!A1:B2", values: `[[1],[2]]`, wantArg: `[[1],[2]]`},
		{name: "row overflow", rangeSpec: "Sheet1!A1:B2", values: `[[1,2],[3,4],[5,6]]`, wantErr: "exceeds the requested range maximum of 2 rows"},
		{name: "column overflow", rangeSpec: "Sheet1!A1:B2", values: `[[1,2,3]]`, wantErr: "exceeds the requested range maximum of 2 columns"},
		{name: "open columns remain API-resolved", rangeSpec: "Sheet1!A:B", values: `[[1,2,3],[4,5,6],[7,8,9]]`, wantArg: `[[1,2,3],[4,5,6],[7,8,9]]`},
		{name: "open rows remain API-resolved", rangeSpec: "Sheet1!1:2", values: `[[1],[2],[3]]`, wantArg: `[[1],[2],[3]]`},
		{name: "named range remains API-resolved", rangeSpec: "NamedRange", values: `[[1,2,3],[4,5,6]]`, wantArg: `[[1,2,3],[4,5,6]]`},
		{name: "trailing JSON rejected", rangeSpec: "Sheet1!A1", values: `[[1]] trailing`, wantErr: "trailing content"},
		{name: "stdin marker rejected", rangeSpec: "Sheet1!A1", values: `-`, wantErr: "literal JSON"},
		{name: "at file marker rejected", rangeSpec: "Sheet1!A1", values: `@values.json`, wantErr: "literal JSON"},
		{name: "large number preserved", rangeSpec: "Sheet1!A1", values: `[[1234567890123456789]]`, wantArg: `[[1234567890123456789]]`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args, err := tool.BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: map[string]any{
				"spreadsheet_id": "sheet-1", "range": tt.rangeSpec, "values_json": tt.values,
			}}})
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("BuildArgs error = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("BuildArgs: %v", err)
			}
			if len(args) != 9 || args[2] != "--values-json" || args[3] != tt.wantArg {
				t.Fatalf("argv = %#v, want canonical values %q at index 3", args, tt.wantArg)
			}
		})
	}

	if _, ok := newMCPTool(tool).InputSchema.Properties["dimension"]; ok {
		t.Fatal("sheets_update_range must not expose deferred dimension input")
	}
}

func TestMCPV03V08DriveStructuredResultsThroughRunner(t *testing.T) {
	tests := []struct {
		name        string
		mode        string
		toolName    string
		arguments   map[string]any
		wantFailure bool
		check       func(*testing.T, mcpCommandResult)
	}{
		{
			name:      "create folder is non-idempotent",
			mode:      "folder",
			toolName:  "drive_create_folder",
			arguments: map[string]any{"name": "Archive", "parent": "parent-1"},
			check: func(t *testing.T, result mcpCommandResult) {
				t.Helper()
				stdout := mcpNativeObject(t, result.Stdout, "folder stdout")
				folder := mcpNativeObject(t, stdout["folder"], "folder result")
				if folder["id"] != "folder-2" || folder["name"] != "Archive" ||
					folder["webViewLink"] != "https://example.test/folders/folder-2" {
					t.Fatalf("folder result = %#v", folder)
				}
				if parents := mcpNativeStrings(t, folder["parents"], "folder parents"); len(parents) != 1 || parents[0] != "parent-1" {
					t.Fatalf("folder parents = %#v", parents)
				}
			},
		},
		{
			name:      "rename preserves canonical file",
			mode:      "rename",
			toolName:  "drive_rename",
			arguments: map[string]any{"file_id": "file-1", "new_name": "Renamed"},
			check: func(t *testing.T, result mcpCommandResult) {
				t.Helper()
				stdout := mcpNativeObject(t, result.Stdout, "rename stdout")
				file := mcpNativeObject(t, stdout["file"], "rename result")
				if file["id"] != "file-1" || file["name"] != "Renamed" ||
					file["webViewLink"] != "https://example.test/files/file-1" {
					t.Fatalf("rename result = %#v", file)
				}
			},
		},
		{
			name:        "rename failure is structured",
			mode:        "rename-failure",
			toolName:    "drive_rename",
			arguments:   map[string]any{"file_id": "file-1", "new_name": "Renamed"},
			wantFailure: true,
		},
		{
			name:      "move replaces every parent",
			mode:      "move",
			toolName:  "drive_move",
			arguments: map[string]any{"file_id": "file-1", "destination_parent": "parent-2"},
			check: func(t *testing.T, result mcpCommandResult) {
				t.Helper()
				stdout := mcpNativeObject(t, result.Stdout, "move stdout")
				file := mcpNativeObject(t, stdout["file"], "move result")
				if file["id"] != "file-1" || file["name"] != "Moved file" ||
					file["webViewLink"] != "https://example.test/files/file-1" {
					t.Fatalf("move result = %#v", file)
				}
				if parents := mcpNativeStrings(t, file["parents"], "move parents"); len(parents) != 1 || parents[0] != "parent-2" {
					t.Fatalf("move parents = %#v", parents)
				}
			},
		},
		{
			name:      "copy reads source and remains shallow",
			mode:      "copy",
			toolName:  "drive_copy",
			arguments: map[string]any{"source_id": "source-folder", "new_name": "Shallow Copy", "parent": "parent-2"},
			check: func(t *testing.T, result mcpCommandResult) {
				t.Helper()
				stdout := mcpNativeObject(t, result.Stdout, "copy stdout")
				file := mcpNativeObject(t, stdout["file"], "copy result")
				if file["id"] != "copy-1" || file["name"] != "Shallow Copy" ||
					file["mimeType"] != "application/vnd.google-apps.folder" ||
					file["webViewLink"] != "https://example.test/files/copy-1" {
					t.Fatalf("copy result = %#v", file)
				}
				if parents := mcpNativeStrings(t, file["parents"], "copy parents"); len(parents) != 1 || parents[0] != "parent-2" {
					t.Fatalf("copy parents = %#v", parents)
				}
			},
		},
		{
			name:      "shortcut preserves target and parent",
			mode:      "shortcut",
			toolName:  "drive_create_shortcut",
			arguments: map[string]any{"target_id": "target-folder", "parent_id": "parent-2"},
			check: func(t *testing.T, result mcpCommandResult) {
				t.Helper()
				stdout := mcpNativeObject(t, result.Stdout, "shortcut stdout")
				shortcut := mcpNativeObject(t, stdout["shortcut"], "shortcut result")
				if shortcut["id"] != "shortcut-1" || shortcut["name"] != "Target folder" ||
					shortcut["webViewLink"] != "https://example.test/files/shortcut-1" {
					t.Fatalf("shortcut result = %#v", shortcut)
				}
				if parents := mcpNativeStrings(t, shortcut["parents"], "shortcut parents"); len(parents) != 1 || parents[0] != "parent-2" {
					t.Fatalf("shortcut parents = %#v", parents)
				}
				details := mcpNativeObject(t, shortcut["shortcutDetails"], "shortcut details")
				if details["targetId"] != "target-folder" {
					t.Fatalf("shortcut target = %#v", details)
				}
			},
		},
		{
			name:      "plain comment",
			mode:      "comment-plain",
			toolName:  "drive_create_comment",
			arguments: map[string]any{"file_id": "file-1", "content": "Plain comment"},
			check: func(t *testing.T, result mcpCommandResult) {
				t.Helper()
				stdout := mcpNativeObject(t, result.Stdout, "plain comment stdout")
				comment := mcpNativeObject(t, stdout["comment"], "plain comment result")
				if comment["id"] != "comment-plain" || comment["content"] != "Plain comment" {
					t.Fatalf("plain comment result = %#v", comment)
				}
				if _, anchored := comment["quotedFileContent"]; anchored {
					t.Fatalf("plain comment unexpectedly anchored = %#v", comment)
				}
			},
		},
		{
			name:      "anchored comment",
			mode:      "comment-anchored",
			toolName:  "drive_create_comment",
			arguments: map[string]any{"file_id": "file-1", "content": "Anchored comment", "quoted_text": "Selected text"},
			check: func(t *testing.T, result mcpCommandResult) {
				t.Helper()
				stdout := mcpNativeObject(t, result.Stdout, "anchored comment stdout")
				comment := mcpNativeObject(t, stdout["comment"], "anchored comment result")
				if comment["id"] != "comment-anchored" || comment["content"] != "Anchored comment" {
					t.Fatalf("anchored comment result = %#v", comment)
				}
				quoted := mcpNativeObject(t, comment["quotedFileContent"], "anchored quoted text")
				if quoted["value"] != "Selected text" {
					t.Fatalf("anchored quoted text = %#v", quoted)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("GOG_MCP_NATIVE_DRIVE_WRITE_HELPER", "1")
			t.Setenv("GOG_MCP_NATIVE_DRIVE_WRITE_MODE", tt.mode)
			tool := findMCPTool(t, tt.toolName)
			if _, err := tool.BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: tt.arguments}}); err != nil {
				t.Fatalf("BuildArgs: %v", err)
			}
			result := mcpRunGogTool(t.Context(), mcpRunOptions{
				self:           os.Args[0],
				tool:           tool,
				commandArgs:    []string{"-test.run=TestMCPNativeDriveWriteRunnerHelper$"},
				timeout:        5 * time.Second,
				maxOutputBytes: 16 * 1024,
			})
			got := requireMCPNativeCommandResult(t, result)
			if got.Tool != tt.toolName || got.Service != "drive" || got.Risk != string(tool.Risk) {
				t.Fatalf("runner metadata = %#v", got)
			}
			if tt.wantFailure {
				if !result.IsError || got.ExitCode != exitCodeRetryable {
					t.Fatalf("failure result = %#v", got)
				}
				if got.Stdout != nil {
					t.Fatalf("failure stdout = %#v, want nil", got.Stdout)
				}
				if !strings.Contains(got.Stderr, "rename backend unavailable") {
					t.Fatalf("failure stderr = %q", got.Stderr)
				}
				return
			}
			if result.IsError || got.ExitCode != 0 {
				t.Fatalf("success result = %#v", got)
			}
			if got.Stderr != "" {
				t.Fatalf("success stderr = %q", got.Stderr)
			}
			tt.check(t, got)
		})
	}
}

func TestMCPV01DriveListStructuredResultThroughRunner(t *testing.T) {
	t.Setenv("GOG_MCP_NATIVE_DRIVE_LIST_HELPER", "1")
	result := mcpRunGogTool(t.Context(), mcpRunOptions{
		self:           os.Args[0],
		tool:           findMCPTool(t, "drive_list_folder"),
		commandArgs:    []string{"-test.run=TestMCPNativeDriveListRunnerHelper$"},
		timeout:        5 * time.Second,
		maxOutputBytes: 4096,
	})
	got := requireMCPNativeCommandResult(t, result)
	if result.IsError || got.ExitCode != 0 {
		t.Fatalf("drive list result = %#v", got)
	}
	if got.Stderr != "" {
		t.Fatalf("drive list stderr = %q", got.Stderr)
	}
	stdout, ok := got.Stdout.(map[string]any)
	if !ok {
		t.Fatalf("drive list stdout type = %T, value=%#v", got.Stdout, got.Stdout)
	}
	files, ok := stdout["files"].([]any)
	if !ok || len(files) != 1 {
		t.Fatalf("drive list files = %#v", stdout["files"])
	}
	if stdout["nextPageToken"] != "next-token" {
		t.Fatalf("drive list nextPageToken = %#v", stdout["nextPageToken"])
	}
}

func TestMCPV02DrivePermissionsStructuredResultThroughRunner(t *testing.T) {
	t.Setenv("GOG_MCP_NATIVE_DRIVE_PERMISSIONS_HELPER", "1")
	result := mcpRunGogTool(t.Context(), mcpRunOptions{
		self:           os.Args[0],
		tool:           findMCPTool(t, "drive_permissions"),
		commandArgs:    []string{"-test.run=TestMCPNativeDrivePermissionsRunnerHelper$"},
		timeout:        5 * time.Second,
		maxOutputBytes: 4096,
	})
	got := requireMCPNativeCommandResult(t, result)
	if result.IsError || got.ExitCode != 0 {
		t.Fatalf("drive permissions result = %#v", got)
	}
	stdout, ok := got.Stdout.(map[string]any)
	if !ok {
		t.Fatalf("drive permissions stdout type = %T, value=%#v", got.Stdout, got.Stdout)
	}
	if stdout["fileId"] != "file-1" || stdout["permissionCount"] != json.Number("1") || stdout["nextPageToken"] != "permission-next" {
		t.Fatalf("drive permissions stdout = %#v", stdout)
	}
	permissions, ok := stdout["permissions"].([]any)
	if !ok || len(permissions) != 1 {
		t.Fatalf("drive permissions list = %#v", stdout["permissions"])
	}
}

func TestMCPV09DocsPagelessFailureThroughRunner(t *testing.T) {
	t.Setenv("GOG_MCP_NATIVE_DOCS_PAGeless_HELPER", "1")
	result := mcpRunGogTool(t.Context(), mcpRunOptions{
		self:           os.Args[0],
		tool:           findMCPTool(t, "docs_create"),
		commandArgs:    []string{"-test.run=TestMCPNativeDocsPagelessFailureRunnerHelper$"},
		timeout:        5 * time.Second,
		maxOutputBytes: 4096,
	})
	got := requireMCPNativeCommandResult(t, result)
	if !result.IsError {
		t.Fatal("pageless failure must be an MCP error")
	}
	if got.ExitCode != exitCodeRetryable {
		t.Fatalf("pageless failure exit code = %d, want %d; stderr=%q", got.ExitCode, exitCodeRetryable, got.Stderr)
	}
	if got.Stdout != nil {
		t.Fatalf("pageless failure unexpectedly emitted success stdout: %#v", got.Stdout)
	}
	if !strings.Contains(got.Stderr, "pageless backend unavailable") {
		t.Fatalf("pageless failure stderr = %q", got.Stderr)
	}
}

func TestMCPV10SheetsAdvisoryParentFailureThroughRunner(t *testing.T) {
	t.Setenv("GOG_MCP_NATIVE_SHEETS_PARENT_HELPER", "1")
	result := mcpRunGogTool(t.Context(), mcpRunOptions{
		self:           os.Args[0],
		tool:           findMCPTool(t, "sheets_create"),
		commandArgs:    []string{"-test.run=TestMCPNativeSheetsAdvisoryParentFailureRunnerHelper$"},
		timeout:        5 * time.Second,
		maxOutputBytes: 4096,
	})
	got := requireMCPNativeCommandResult(t, result)
	if result.IsError || got.ExitCode != 0 {
		t.Fatalf("advisory parent failure must preserve success: %#v", got)
	}
	stdout, ok := got.Stdout.(map[string]any)
	if !ok {
		t.Fatalf("sheets create stdout type = %T, value=%#v", got.Stdout, got.Stdout)
	}
	if stdout["spreadsheetId"] != "id2" || stdout["movedToParent"] != false {
		t.Fatalf("sheets create stdout = %#v", stdout)
	}
	if moveError, _ := stdout["moveError"].(string); !strings.Contains(moveError, "forbidden") {
		t.Fatalf("sheets moveError = %q", moveError)
	}
	if !strings.Contains(got.Stderr, "failed to move spreadsheet to folder") || !strings.Contains(got.Stderr, "Spreadsheet created in Drive root") {
		t.Fatalf("sheets advisory stderr = %q", got.Stderr)
	}
}

func TestMCPV11SlidesCopyThenReplaceFailureThroughRunner(t *testing.T) {
	t.Setenv("GOG_MCP_NATIVE_SLIDES_FAILURE_HELPER", "1")
	result := mcpRunGogTool(t.Context(), mcpRunOptions{
		self:           os.Args[0],
		tool:           findMCPTool(t, "slides_create_from_template"),
		commandArgs:    []string{"-test.run=TestMCPNativeSlidesCopyThenReplaceFailureRunnerHelper$"},
		timeout:        5 * time.Second,
		maxOutputBytes: 4096,
	})
	got := requireMCPNativeCommandResult(t, result)
	if !result.IsError {
		t.Fatal("replacement failure must be an MCP error")
	}
	if got.ExitCode != exitCodeRetryable {
		t.Fatalf("replacement failure exit code = %d, want %d; stderr=%q", got.ExitCode, exitCodeRetryable, got.Stderr)
	}
	if got.Stdout != nil {
		t.Fatalf("replacement failure unexpectedly emitted success stdout: %#v", got.Stdout)
	}
	for _, want := range []string{"presentation created but text replacement failed", "Presentation ID: copied-1", "manually edit or delete"} {
		if !strings.Contains(got.Stderr, want) {
			t.Fatalf("replacement failure stderr = %q, want %q", got.Stderr, want)
		}
	}
}

func TestMCPNativeDriveListRunnerHelper(t *testing.T) {
	if os.Getenv("GOG_MCP_NATIVE_DRIVE_LIST_HELPER") != "1" {
		return
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/files" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"files":         []map[string]any{{"id": "file-1", "name": "Report", "mimeType": "text/plain"}},
			"nextPageToken": "next-token",
		})
	}))
	defer srv.Close()
	driveSvc := newGoogleTestServiceWithEndpoint(t, srv.Client(), srv.URL+"/", drive.NewService)
	result := executeWithTestRuntime(t, []string{
		"--json", "--account", "test@example.com", "drive", "ls", "--max", "2", "--page", "page-1", "--no-all-drives", "--parent", "folder-1",
	}, &app.Runtime{Services: app.Services{Drive: stubDriveService(driveSvc)}})
	mcpNativeEmitExecuteResult(result)
}

func TestMCPNativeDrivePermissionsRunnerHelper(t *testing.T) {
	if os.Getenv("GOG_MCP_NATIVE_DRIVE_PERMISSIONS_HELPER") != "1" {
		return
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/files/file-1/permissions" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"permissions":   []map[string]any{{"id": "perm-1", "type": "user", "role": "reader", "emailAddress": "reader@example.com"}},
			"nextPageToken": "permission-next",
		})
	}))
	defer srv.Close()
	driveSvc := newGoogleTestServiceWithEndpoint(t, srv.Client(), srv.URL+"/", drive.NewService)
	result := executeWithTestRuntime(t, []string{
		"--json", "--account", "test@example.com", "drive", "permissions", "file-1", "--max", "2", "--page", "page-1",
	}, &app.Runtime{Services: app.Services{Drive: stubDriveService(driveSvc)}})
	mcpNativeEmitExecuteResult(result)
}

func TestMCPNativeDocsPagelessFailureRunnerHelper(t *testing.T) {
	if os.Getenv("GOG_MCP_NATIVE_DOCS_PAGeless_HELPER") != "1" {
		return
	}
	created := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/files":
			created = true
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "doc-created", "name": "Doc", "mimeType": "application/vnd.google-apps.document", "webViewLink": "https://example.test/doc-created",
			})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/documents/doc-created:batchUpdate":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": 503, "message": "pageless backend unavailable"}})
		default:
			http.NotFound(w, r)
		}
	}))
	driveSvc := newGoogleTestServiceWithEndpoint(t, srv.Client(), srv.URL+"/", drive.NewService)
	docsSvc := newGoogleTestServiceWithEndpoint(t, srv.Client(), srv.URL+"/", docs.NewService)
	result := executeWithTestRuntime(t, []string{"--json", "--account", "test@example.com", "docs", "create", "Doc", "--pageless"}, &app.Runtime{Services: app.Services{
		Drive: stubDriveService(driveSvc),
		Docs:  func(context.Context, string) (*docs.Service, error) { return docsSvc, nil },
	}})
	if !created {
		_, _ = io.WriteString(os.Stderr, "docs fixture did not create the document before pageless failure\n")
	}
	mcpNativeEmitExecuteResult(result)
}

func TestMCPNativeSheetsAdvisoryParentFailureRunnerHelper(t *testing.T) {
	if os.Getenv("GOG_MCP_NATIVE_SHEETS_PARENT_HELPER") != "1" {
		return
	}
	sawCreate := false
	sawMove := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v4/spreadsheets":
			sawCreate = true
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"spreadsheetId": "id2", "spreadsheetUrl": "https://example.test/sheets/id2", "properties": map[string]any{"title": "Budget"},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/files/id2":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "id2", "parents": []string{"root"}})
		case r.Method == http.MethodPatch && r.URL.Path == "/files/id2":
			sawMove = true
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": 403, "message": "forbidden"}})
		default:
			http.NotFound(w, r)
		}
	}))
	sheetsSvc := newGoogleTestServiceWithEndpoint(t, srv.Client(), srv.URL+"/", sheets.NewService)
	driveSvc := newGoogleTestServiceWithEndpoint(t, srv.Client(), srv.URL+"/", drive.NewService)
	result := executeWithTestRuntime(t, []string{"--json", "--account", "test@example.com", "sheets", "create", "Budget", "--parent", "folder-1"}, &app.Runtime{Services: app.Services{
		Drive:  stubDriveService(driveSvc),
		Sheets: func(context.Context, string) (*sheets.Service, error) { return sheetsSvc, nil },
	}})
	if !sawCreate || !sawMove {
		_, _ = io.WriteString(os.Stderr, "sheets fixture did not exercise create and advisory move\n")
	}
	mcpNativeEmitExecuteResult(result)
}

func TestMCPNativeSlidesCopyThenReplaceFailureRunnerHelper(t *testing.T) {
	if os.Getenv("GOG_MCP_NATIVE_SLIDES_FAILURE_HELPER") != "1" {
		return
	}
	sawCopy := false
	sawReplace := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/files/template-1/copy":
			sawCopy = true
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(&drive.File{Id: "copied-1", Name: "Deck", MimeType: "application/vnd.google-apps.presentation", WebViewLink: "https://example.test/presentations/copied-1"})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/presentations/copied-1:batchUpdate":
			sawReplace = true
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": 503, "message": "replacement backend unavailable"}})
		default:
			http.NotFound(w, r)
		}
	}))
	driveSvc := newGoogleTestServiceWithEndpoint(t, srv.Client(), srv.URL+"/", drive.NewService)
	slidesSvc := newGoogleTestServiceWithEndpoint(t, srv.Client(), srv.URL+"/", slides.NewService)
	result := executeWithTestRuntime(t, []string{
		"--json", "--account", "test@example.com", "slides", "create-from-template", "template-1", "Deck", "--replace", "name=Ryan",
	}, &app.Runtime{Services: app.Services{
		Drive:  stubDriveService(driveSvc),
		Slides: func(context.Context, string) (*slides.Service, error) { return slidesSvc, nil },
	}})
	if !sawCopy || !sawReplace {
		_, _ = io.WriteString(os.Stderr, "slides fixture did not exercise copy and replacement\n")
	}
	mcpNativeEmitExecuteResult(result)
}

func TestMCPNativeDriveWriteRunnerHelper(t *testing.T) {
	if os.Getenv("GOG_MCP_NATIVE_DRIVE_WRITE_HELPER") != "1" {
		return
	}
	mode := os.Getenv("GOG_MCP_NATIVE_DRIVE_WRITE_MODE")
	var (
		folderCreates   int
		sawRename       bool
		sawMoveGet      bool
		sawMovePatch    bool
		sawCopyGet      bool
		sawCopyPost     bool
		sawShortcutGet  bool
		sawShortcutPost bool
		sawComment      bool
		invalidRequest  bool
		requestError    string
	)
	markInvalid := func(message string) {
		invalidRequest = true
		requestError += message + "; "
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case mode == "folder" && r.Method == http.MethodPost && r.URL.Path == "/files":
			folderCreates++
			var body drive.File
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				markInvalid("folder body decode")
			}
			if body.Name != "Archive" || body.MimeType != "application/vnd.google-apps.folder" ||
				len(body.Parents) != 1 || body.Parents[0] != "parent-1" {
				markInvalid("folder request fields")
			}
			id := "folder-1"
			if folderCreates > 1 {
				id = "folder-2"
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": id, "name": "Archive", "parents": []string{"parent-1"},
				"webViewLink": "https://example.test/folders/" + id,
			})
		case (mode == "rename" || mode == "rename-failure") && r.Method == http.MethodPatch && r.URL.Path == "/files/file-1":
			sawRename = true
			if mode == "rename-failure" {
				w.WriteHeader(http.StatusServiceUnavailable)
				_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": 503, "message": "rename backend unavailable"}})
				break
			}
			var body drive.File
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				markInvalid("rename body decode")
			}
			if body.Name != "Renamed" {
				markInvalid("rename request fields")
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "file-1", "name": "Renamed", "webViewLink": "https://example.test/files/file-1",
			})
		case mode == "move" && r.Method == http.MethodGet && r.URL.Path == "/files/file-1":
			sawMoveGet = true
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "file-1", "name": "Moved file", "parents": []string{"old-a", "old-b"}})
		case mode == "move" && r.Method == http.MethodPatch && r.URL.Path == "/files/file-1":
			sawMovePatch = true
			if r.URL.Query().Get("addParents") != "parent-2" || r.URL.Query().Get("removeParents") != "old-a,old-b" {
				markInvalid("move parent replacement")
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "file-1", "name": "Moved file", "parents": []string{"parent-2"},
				"webViewLink": "https://example.test/files/file-1",
			})
		case mode == "copy" && r.Method == http.MethodGet && r.URL.Path == "/files/source-folder":
			sawCopyGet = true
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "source-folder", "name": "Source folder", "mimeType": "application/vnd.google-apps.folder",
			})
		case mode == "copy" && r.Method == http.MethodPost && r.URL.Path == "/files/source-folder/copy":
			sawCopyPost = true
			var body drive.File
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				markInvalid("copy body decode")
			}
			if body.Name != "Shallow Copy" || len(body.Parents) != 1 || body.Parents[0] != "parent-2" {
				markInvalid("copy request fields")
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "copy-1", "name": "Shallow Copy", "mimeType": "application/vnd.google-apps.folder",
				"parents": []string{"parent-2"}, "webViewLink": "https://example.test/files/copy-1",
			})
		case mode == "shortcut" && r.Method == http.MethodGet && r.URL.Path == "/files/target-folder":
			sawShortcutGet = true
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "target-folder", "name": "Target folder"})
		case mode == "shortcut" && r.Method == http.MethodPost && r.URL.Path == "/files":
			sawShortcutPost = true
			var body drive.File
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				markInvalid("shortcut body decode")
			}
			if body.Name != "Target folder" || body.MimeType != driveMimeShortcut ||
				len(body.Parents) != 1 || body.Parents[0] != "parent-2" ||
				body.ShortcutDetails == nil || body.ShortcutDetails.TargetId != "target-folder" {
				markInvalid("shortcut request fields")
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "shortcut-1", "name": "Target folder", "mimeType": driveMimeShortcut,
				"parents": []string{"parent-2"}, "webViewLink": "https://example.test/files/shortcut-1",
				"shortcutDetails": map[string]any{"targetId": "target-folder", "targetMimeType": "application/vnd.google-apps.folder"},
			})
		case (mode == "comment-plain" || mode == "comment-anchored") &&
			r.Method == http.MethodPost && r.URL.Path == "/files/file-1/comments":
			sawComment = true
			var body drive.Comment
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				markInvalid("comment body decode")
			}
			if mode == "comment-plain" {
				if body.Content != "Plain comment" || body.QuotedFileContent != nil {
					markInvalid("plain comment request fields")
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"id": "comment-plain", "content": "Plain comment"})
			} else {
				if body.Content != "Anchored comment" || body.QuotedFileContent == nil ||
					body.QuotedFileContent.Value != "Selected text" {
					markInvalid("anchored comment request fields")
				}
				_ = json.NewEncoder(w).Encode(map[string]any{
					"id": "comment-anchored", "content": "Anchored comment",
					"quotedFileContent": map[string]any{"value": "Selected text"},
				})
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	driveSvc := newGoogleTestServiceWithEndpoint(t, srv.Client(), srv.URL+"/", drive.NewService)
	var commandArgs []string
	switch mode {
	case "folder":
		commandArgs = []string{"drive", "mkdir", "Archive", "--parent", "parent-1"}
	case "rename", "rename-failure":
		commandArgs = []string{"drive", "rename", "--", "file-1", "Renamed"}
	case "move":
		commandArgs = []string{"drive", "move", "--parent", "parent-2", "--", "file-1"}
	case "copy":
		commandArgs = []string{"drive", "copy", "--parent", "parent-2", "--", "source-folder", "Shallow Copy"}
	case "shortcut":
		commandArgs = []string{"drive", "shortcut", "create", "--parent", "parent-2", "--", "target-folder"}
	case "comment-plain":
		commandArgs = []string{"drive", "comments", "create", "file-1", "Plain comment"}
	case "comment-anchored":
		commandArgs = []string{"drive", "comments", "create", "file-1", "Anchored comment", "--quoted", "Selected text"}
	default:
		commandArgs = []string{"drive", "mkdir", "Archive", "--parent", "parent-1"}
	}
	args := append([]string{"--json", "--account", "test@example.com"}, commandArgs...)
	var result executeTestResult
	if mode == "folder" {
		_ = executeWithTestRuntime(t, args, &app.Runtime{Services: app.Services{Drive: stubDriveService(driveSvc)}})
		result = executeWithTestRuntime(t, args, &app.Runtime{Services: app.Services{Drive: stubDriveService(driveSvc)}})
	} else {
		result = executeWithTestRuntime(t, args, &app.Runtime{Services: app.Services{Drive: stubDriveService(driveSvc)}})
	}
	switch mode {
	case "folder":
		if folderCreates != 2 {
			requestError += "folder fixture did not prove duplicate creation; "
		}
	case "rename", "rename-failure":
		if !sawRename {
			requestError += "rename fixture did not reach API; "
		}
	case "move":
		if !sawMoveGet || !sawMovePatch {
			requestError += "move fixture did not pre-read and replace parents; "
		}
	case "copy":
		if !sawCopyGet || !sawCopyPost {
			requestError += "copy fixture did not pre-read and copy once; "
		}
	case "shortcut":
		if !sawShortcutGet || !sawShortcutPost {
			requestError += "shortcut fixture did not look up target and create; "
		}
	case "comment-plain", "comment-anchored":
		if !sawComment {
			requestError += "comment fixture did not reach API; "
		}
	}
	if invalidRequest || requestError != "" {
		result.stderr += "drive fixture request contract: " + requestError + "\n"
	}
	mcpNativeEmitExecuteResult(result)
}

func TestMCPNativeDocsGetRunnerHelper(t *testing.T) {
	if os.Getenv("GOG_MCP_NATIVE_DOCS_GET_HELPER") != "1" {
		return
	}
	mode := os.Getenv("GOG_MCP_NATIVE_DOCS_GET_MODE")
	var requestSeen bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/documents/doc-1" {
			http.NotFound(w, r)
			return
		}
		requestSeen = true
		w.Header().Set("Content-Type", "application/json")
		paragraph := func(text string) []any {
			return []any{map[string]any{
				"paragraph": map[string]any{
					"elements": []any{
						map[string]any{"textRun": map[string]any{"content": text}},
					},
				},
			}}
		}
		response := map[string]any{"documentId": "doc-1", "title": "Raw document"}
		switch mode {
		case "selected-tab", "all-tabs":
			response["tabs"] = []any{
				map[string]any{
					"tabProperties": map[string]any{"tabId": "tab-overview", "title": "Overview", "index": 0},
					"documentTab":   map[string]any{"body": map[string]any{"content": paragraph("overview text")}},
				},
				map[string]any{
					"tabProperties": map[string]any{"tabId": "tab-details", "title": "Details", "index": 1},
					"documentTab":   map[string]any{"body": map[string]any{"content": paragraph("details text")}},
				},
			}
		case "max-bytes":
			response["body"] = map[string]any{"content": paragraph("0123456789")}
		case "raw":
			response["body"] = map[string]any{"content": paragraph(strings.Repeat("raw payload ", 40))}
		default:
			response["body"] = map[string]any{"content": paragraph("default text")}
		}
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer srv.Close()

	docsSvc := newGoogleTestServiceWithEndpoint(t, srv.Client(), srv.URL+"/", docs.NewService)
	var commandArgs []string
	switch mode {
	case "selected-tab":
		commandArgs = []string{"docs", "cat", "--max-bytes", "2000000", "--tab", "Overview", "--", "doc-1"}
	case "all-tabs":
		commandArgs = []string{"docs", "cat", "--max-bytes", "2000000", "--all-tabs", "--", "doc-1"}
	case "max-bytes":
		commandArgs = []string{"docs", "cat", "--max-bytes", "5", "--", "doc-1"}
	case "raw":
		commandArgs = []string{"docs", "cat", "--raw", "--", "doc-1"}
	default:
		commandArgs = []string{"docs", "cat", "--max-bytes", "2000000", "--", "doc-1"}
	}
	result := executeWithTestRuntime(t, append([]string{"--json", "--account", "test@example.com"}, commandArgs...), &app.Runtime{Services: app.Services{
		Docs: func(context.Context, string) (*docs.Service, error) { return docsSvc, nil },
	}})
	if !requestSeen {
		result.stderr += "docs_get fixture did not reach the Docs API\n"
	}
	mcpNativeEmitExecuteResult(result)
}

func mcpNativeEmitExecuteResult(result executeTestResult) {
	if result.stdout != "" {
		_, _ = io.WriteString(os.Stdout, result.stdout)
	}
	if result.stderr != "" {
		_, _ = io.WriteString(os.Stderr, result.stderr)
	}
	if result.err == nil {
		os.Exit(0)
	}
	os.Exit(ExitCode(stableExitCode(result.err)))
}

func assertMCPWaveANativeArgv(t *testing.T, got, want []string) {
	t.Helper()
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("argv = %#v, want %#v", got, want)
	}
	for _, arg := range got {
		if arg == "--json" || arg == "--wrap-untrusted" || arg == "--no-input" || arg == "--color=never" {
			t.Fatalf("runner-owned root flag leaked into adapter argv: %#v", got)
		}
	}
}

func callMCPWaveANativeSchema(t *testing.T, toolName string, arguments map[string]any) (*mcp.CallToolResult, int) {
	t.Helper()
	s := newMCPServer()
	handlerCalls := 0
	s.AddTool(newMCPTool(findMCPTool(t, toolName)), func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		handlerCalls++
		return mcp.NewToolResultText("ok"), nil
	})

	client, err := mcpclient.NewInProcessClient(s)
	if err != nil {
		t.Fatalf("NewInProcessClient: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := client.Close(); closeErr != nil {
			t.Errorf("close client: %v", closeErr)
		}
	})
	if startErr := client.Start(t.Context()); startErr != nil {
		t.Fatalf("client.Start: %v", startErr)
	}
	initRequest := mcp.InitializeRequest{}
	initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initRequest.Params.ClientInfo = mcp.Implementation{Name: "gog-native-wave-a-test", Version: "1"}
	if _, initErr := client.Initialize(t.Context(), initRequest); initErr != nil {
		t.Fatalf("client.Initialize: %v", initErr)
	}
	result, err := client.CallTool(t.Context(), mcp.CallToolRequest{Params: mcp.CallToolParams{Name: toolName, Arguments: arguments}})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	return result, handlerCalls
}

func callMCPWaveANativeBuildArgs(t *testing.T, toolName string, arguments map[string]any) (*mcp.CallToolResult, int) {
	t.Helper()
	spec := findMCPTool(t, toolName)
	s := newMCPServer()
	handlerCalls := 0
	s.AddTool(newMCPTool(spec), func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if _, err := spec.BuildArgs(req); err != nil {
			result := mcp.NewToolResultError(err.Error())
			result.IsError = true
			return result, nil
		}
		handlerCalls++
		return mcp.NewToolResultText("ok"), nil
	})

	client, err := mcpclient.NewInProcessClient(s)
	if err != nil {
		t.Fatalf("NewInProcessClient: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := client.Close(); closeErr != nil {
			t.Errorf("close client: %v", closeErr)
		}
	})
	if startErr := client.Start(t.Context()); startErr != nil {
		t.Fatalf("client.Start: %v", startErr)
	}
	initRequest := mcp.InitializeRequest{}
	initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initRequest.Params.ClientInfo = mcp.Implementation{Name: "gog-native-wave-a-test", Version: "1"}
	if _, initErr := client.Initialize(t.Context(), initRequest); initErr != nil {
		t.Fatalf("client.Initialize: %v", initErr)
	}
	result, err := client.CallTool(t.Context(), mcp.CallToolRequest{Params: mcp.CallToolParams{Name: toolName, Arguments: arguments}})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	return result, handlerCalls
}

func requireMCPNativeCommandResult(t *testing.T, result *mcp.CallToolResult) mcpCommandResult {
	t.Helper()
	if result == nil {
		t.Fatal("nil MCP result")
	}
	got, ok := result.StructuredContent.(mcpCommandResult)
	if !ok {
		t.Fatalf("structured result type = %T, value=%#v", result.StructuredContent, result.StructuredContent)
	}
	return got
}

func mcpNativeObject(t *testing.T, value any, field string) map[string]any {
	t.Helper()
	object, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("%s type = %T, value=%#v; want object", field, value, value)
	}
	return object
}

func mcpNativeObjects(t *testing.T, value any, field string) []map[string]any {
	t.Helper()
	raw, ok := value.([]any)
	if !ok {
		t.Fatalf("%s type = %T, value=%#v; want array", field, value, value)
	}
	out := make([]map[string]any, 0, len(raw))
	for i, item := range raw {
		object, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("%s[%d] type = %T, value=%#v; want object", field, i, item, item)
		}
		out = append(out, object)
	}
	return out
}

func mcpNativeStrings(t *testing.T, value any, field string) []string {
	t.Helper()
	raw, ok := value.([]any)
	if !ok {
		t.Fatalf("%s type = %T, value=%#v; want array", field, value, value)
	}
	out := make([]string, 0, len(raw))
	for i, item := range raw {
		text, ok := item.(string)
		if !ok {
			t.Fatalf("%s[%d] type = %T, value=%#v; want string", field, i, item, item)
		}
		out = append(out, text)
	}
	return out
}

// These child fixtures exercise the real API clients and CLI paths rather than
// returning synthetic command-runner bytes.
