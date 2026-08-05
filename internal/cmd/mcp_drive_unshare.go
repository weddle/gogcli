package cmd

import (
	"github.com/mark3labs/mcp-go/mcp"
)

func mcpDriveUnshareTool() mcpToolSpec {
	return mcpToolSpec{
		Name:        "drive_unshare",
		Service:     "drive",
		Risk:        mcpRiskDestructive,
		Description: "Remove exactly one permission from one Google Drive file. Requires explicit destructive authorization; the server supplies --force after the V02 permission read-before-mutation workflow.",
		Options: []mcp.ToolOption{
			mcp.WithString("file_id", mcp.Description("Drive file ID"), mcp.Required()),
			mcp.WithString("permission_id", mcp.Description("Drive permission ID to remove"), mcp.Required()),
		},
		BuildArgs: func(req mcp.CallToolRequest) ([]string, error) {
			fileID, err := requireMCPString(req, "file_id")
			if err != nil {
				return nil, err
			}
			permissionID, err := requireMCPString(req, "permission_id")
			if err != nil {
				return nil, err
			}
			// --force is server-controlled: it is never a model-supplied schema field.
			return []string{"drive", "unshare", "--force", "--", fileID, permissionID}, nil
		},
	}
}
