package cmd

import "github.com/mark3labs/mcp-go/mcp"

func mcpDriveTrashTool() mcpToolSpec {
	return mcpToolSpec{
		Name:        "drive_trash",
		Service:     "drive",
		Risk:        mcpRiskDestructive,
		Description: "Move one Google Drive file to trash. Requires ordinary write authorization and explicit destructive authorization; permanent deletion is not available.",
		Options: []mcp.ToolOption{
			mcp.WithString("file_id", mcp.Description("Google Drive file ID"), mcp.Required()),
		},
		BuildArgs: func(req mcp.CallToolRequest) ([]string, error) {
			fileID, err := requireMCPString(req, "file_id")
			if err != nil {
				return nil, err
			}
			// --force is fixed by the server so the child cannot prompt or depend
			// on model-supplied confirmation controls. The MCP schema exposes no
			// permanent-delete or filesystem input.
			return []string{"drive", "delete", "--force", "--", fileID}, nil
		},
	}
}
