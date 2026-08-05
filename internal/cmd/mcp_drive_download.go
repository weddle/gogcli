package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"google.golang.org/api/drive/v3"
)

// mcpDriveDownloadFormats is deliberately the CLI's supported export-format
// set. The MCP schema is closed to this set; format inference remains owned by
// the Drive download command when the field is omitted.
var mcpDriveDownloadFormats = []string{"pdf", "csv", "xlsx", "pptx", "txt", "png", "docx", "md", "html"}

func mcpDriveDownloadTool() mcpToolSpec {
	return mcpToolSpec{
		Name:        "drive_download",
		Service:     "drive",
		Risk:        mcpRiskRead,
		Description: "Download one Google Drive file or supported Workspace export as bounded inline base64 content. The 64 KiB raw limit is fixed; no host file is created.",
		Options: []mcp.ToolOption{
			mcp.WithString("file_id", mcp.Description("Google Drive file ID"), mcp.Required()),
			mcp.WithString("format", mcp.Description("Optional export format; omitted uses Drive's inferred format"), mcp.Enum(mcpDriveDownloadFormats...)),
		},
		BuildArgs: func(req mcp.CallToolRequest) ([]string, error) {
			fileID, err := requireMCPString(req, "file_id")
			if err != nil {
				return nil, err
			}
			format, err := mcpDriveDownloadFormat(req)
			if err != nil {
				return nil, err
			}

			// The raw-output and cap flags are server-controlled. They are not
			// schema inputs and cannot be replaced with a host path or stdin.
			args := []string{
				"drive", "download",
				"--max-bytes", strconv.FormatInt(DriveDownloadMaxBytes, 10),
				"--out", "-",
			}
			if format != "" {
				args = append(args, "--format", format)
			}
			return append(args, "--", fileID), nil
		},
	}
}

func mcpDriveDownloadFormat(req mcp.CallToolRequest) (string, error) {
	raw, supplied := req.GetArguments()["format"]
	if !supplied {
		return "", nil
	}
	format, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("format must be a string")
	}
	format = strings.ToLower(strings.TrimSpace(format))
	if format == "" {
		return "", fmt.Errorf("format cannot be empty")
	}
	if err := validateDriveDownloadFormatFlag(format); err != nil {
		return "", err
	}
	return format, nil
}

// mcpRunDriveDownload first obtains only the metadata needed for the B03
// envelope, then runs the bounded CLI download with a raw server capture. The
// raw child intentionally omits only --json; all other parent and safety args
// continue to come from the MCP runner.
func mcpRunDriveDownload(reqCtx context.Context, opts mcpRunOptions) *mcp.CallToolResult {
	ctx, cancel := context.WithTimeout(reqCtx, opts.timeout)
	defer cancel()

	fileID, ok := mcpDriveDownloadFileID(opts.commandArgs)
	if !ok {
		return mcpDriveDownloadCommandResult(opts, 2, nil, "drive_download: missing fixed file ID")
	}
	format := mcpDriveDownloadFormatFromArgs(opts.commandArgs)

	metadataOpts := opts
	metadataOpts.baseArgs = mcpDriveDownloadChildRootArgs(opts.baseArgs, true)
	metadataOpts.tool = mcpDriveGetTool()
	metadataOpts.commandArgs = []string{
		"drive", "get", "--fields", "id,name,mimeType", "--", fileID,
	}
	metadataResult := mcpRunGogTool(ctx, metadataOpts)
	metadata, metadataErr := mcpDriveDownloadMetadata(metadataResult, fileID)
	if metadataErr != nil {
		if metadataResult != nil {
			if commandResult, ok := metadataResult.StructuredContent.(mcpCommandResult); ok {
				return mcpDriveDownloadCommandResult(opts, commandResult.ExitCode, commandResult.Stdout, mcpFirstNonEmpty(commandResult.Stderr, metadataErr.Error()))
			}
		}
		return mcpDriveDownloadCommandResult(opts, 1, nil, metadataErr.Error())
	}

	name, mimeType, err := mcpDriveDownloadOutputMetadata(metadata, format)
	if err != nil {
		return mcpDriveDownloadCommandResult(opts, 2, nil, err.Error())
	}

	raw, rawResult := mcpRunDriveDownloadRaw(ctx, opts)
	if rawResult != nil {
		return rawResult
	}
	return mcpInlineBinaryToolResult(raw, name, mimeType, opts.maxOutputBytes)
}

func mcpDriveDownloadMetadata(result *mcp.CallToolResult, requestedID string) (*drive.File, error) {
	if result == nil {
		return nil, errors.New("drive_download: metadata child returned no result")
	}
	commandResult, ok := result.StructuredContent.(mcpCommandResult)
	if !ok {
		return nil, fmt.Errorf("drive_download: metadata child returned %T", result.StructuredContent)
	}
	if commandResult.ExitCode != 0 || result.IsError {
		return nil, fmt.Errorf("drive_download: metadata lookup failed")
	}

	object, ok := commandResult.Stdout.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("drive_download: metadata child returned %T", commandResult.Stdout)
	}
	if nested, ok := object["file"].(map[string]any); ok {
		object = nested
	}
	metadata := &drive.File{Id: requestedID}
	if value, ok := object["id"].(string); ok && strings.TrimSpace(value) != "" {
		metadata.Id = value
	}
	if value, ok := object["name"].(string); ok {
		metadata.Name = value
	}
	if value, ok := object["mimeType"].(string); ok {
		metadata.MimeType = value
	}
	return metadata, nil
}

func mcpDriveDownloadOutputMetadata(metadata *drive.File, format string) (string, string, error) {
	if metadata == nil {
		return "", "", errors.New("drive_download: missing metadata")
	}
	if err := validateDriveDownloadFormatFlag(format); err != nil {
		return "", "", err
	}
	if err := validateDriveDownloadFormatForFile(metadata, format); err != nil {
		return "", "", err
	}

	format = strings.ToLower(strings.TrimSpace(format))
	if format == formatAuto {
		format = ""
	}
	if !strings.HasPrefix(metadata.MimeType, "application/vnd.google-apps.") {
		return metadata.Name, metadata.MimeType, nil
	}

	exportMimeType, err := driveExportMimeTypeForFormat(metadata.MimeType, format)
	if err != nil {
		return "", "", err
	}
	return replaceExt(metadata.Name, driveExportExtension(exportMimeType)), exportMimeType, nil
}

func mcpDriveDownloadFileID(args []string) (string, bool) {
	for index := len(args) - 2; index >= 0; index-- {
		if args[index] == "--" && index+1 < len(args) {
			value := strings.TrimSpace(args[index+1])
			return value, value != ""
		}
	}
	return "", false
}

func mcpDriveDownloadFormatFromArgs(args []string) string {
	for index := 0; index+1 < len(args); index++ {
		if args[index] == "--format" {
			return args[index+1]
		}
	}
	return ""
}

func mcpRunDriveDownloadRaw(ctx context.Context, opts mcpRunOptions) ([]byte, *mcp.CallToolResult) {
	args := make([]string, 0, len(opts.baseArgs)+len(opts.safetySuffix)+len(opts.commandArgs))
	args = append(args, mcpDriveDownloadChildRootArgs(opts.baseArgs, false)...)
	args = append(args, opts.safetySuffix...)
	args = append(args, opts.commandArgs...)

	//nolint:gosec // argv is assembled only from the closed MCP schema and fixed server flags.
	cmd := exec.CommandContext(ctx, opts.self, args...)
	if strings.TrimSpace(opts.accessToken) != "" {
		cmd.Env = append(os.Environ(), "GOG_ACCESS_TOKEN="+opts.accessToken)
	}
	capture := mcpDriveDownloadRawCapture{limit: int(DriveDownloadMaxBytes) + 1}
	stderr := newMCPLimitedBuffer(opts.maxOutputBytes)
	cmd.Stdout = &capture
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	exitCode := 0
	if runErr != nil {
		exitCode = 1
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			exitCode = exitErr.ExitCode()
		}
	}
	if ctx.Err() == context.DeadlineExceeded {
		exitCode = 124
	}

	if capture.overflow || capture.Len() > int(DriveDownloadMaxBytes) || strings.Contains(stderr.String(), "binary_size_limit") {
		return nil, mcpInlineBinaryErrorResult("binary_size_limit", int(DriveDownloadMaxBytes))
	}
	if runErr != nil || exitCode != 0 {
		return nil, mcpDriveDownloadCommandResult(opts, exitCode, nil, stderr.String())
	}
	return capture.Bytes(), nil
}

func mcpDriveDownloadChildRootArgs(args []string, keepJSON bool) []string {
	out := make([]string, 0, len(args))
	for index := 0; index < len(args); index++ {
		arg := args[index]
		switch {
		case arg == "--select":
			index++
			continue
		case strings.HasPrefix(arg, "--select="),
			arg == "--wrap-untrusted",
			arg == "--results-only",
			arg == "--dry-run":
			continue
		case !keepJSON && (arg == "--json" || strings.HasPrefix(arg, "--json=")):
			continue
		default:
			out = append(out, arg)
		}
	}
	return out
}

type mcpDriveDownloadRawCapture struct {
	bytes.Buffer
	limit    int
	overflow bool
}

func (c *mcpDriveDownloadRawCapture) Write(p []byte) (int, error) {
	remaining := c.limit - c.Len()
	if remaining <= 0 {
		c.overflow = true
		return len(p), nil
	}
	if len(p) > remaining {
		_, _ = c.Buffer.Write(p[:remaining])
		c.overflow = true
		return len(p), nil
	}
	return c.Buffer.Write(p)
}

func mcpDriveDownloadCommandResult(opts mcpRunOptions, exitCode int, stdout any, stderr string) *mcp.CallToolResult {
	result := mcp.NewToolResultStructuredOnly(mcpCommandResult{
		Tool:     opts.tool.Name,
		Service:  opts.tool.Service,
		Risk:     string(opts.tool.Risk),
		ExitCode: exitCode,
		Stdout:   stdout,
		Stderr:   stderr,
	})
	result.IsError = true
	return result
}

func mcpFirstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return "child command failed"
}
