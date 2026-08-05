package cmd

import (
	"github.com/mark3labs/mcp-go/mcp"
)

func mcpDriveShareUserTool() mcpToolSpec {
	return mcpToolSpec{
		Name:        "drive_share_user",
		Service:     "drive",
		Risk:        mcpRiskDestructive,
		Description: "Grant one Google Drive file permission to one user. The target is always a user and invitation notifications are disabled; requires explicit destructive authorization.",
		Options: []mcp.ToolOption{
			mcp.WithString("file_id", mcp.Description("Drive file ID"), mcp.Required()),
			mcp.WithString("email", mcp.Description("Plain user email address"), mcp.Required()),
			mcp.WithString("role", mcp.Description("Permission role; defaults to reader"), mcp.Enum("reader", "commenter", "writer"), mcp.DefaultString("reader")),
		},
		BuildArgs: func(req mcp.CallToolRequest) ([]string, error) {
			fileID, err := requireMCPString(req, "file_id")
			if err != nil {
				return nil, err
			}
			email, err := requireMCPString(req, "email")
			if err != nil {
				return nil, err
			}
			if validationErr := validateDriveShareEmail(email); validationErr != nil {
				return nil, validationErr
			}
			role, err := normalizeDrivePermissionRole(req.GetString("role", "reader"))
			if err != nil {
				return nil, err
			}
			return []string{"drive", "share", "--to", driveShareToUser, "--email", email, "--role", role, "--", fileID}, nil
		},
	}
}
