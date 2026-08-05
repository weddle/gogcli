package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"google.golang.org/api/drive/v3"

	"github.com/steipete/gogcli/internal/config"
	"github.com/steipete/gogcli/internal/outfmt"
	"github.com/steipete/gogcli/internal/ui"
)

// DriveDownloadMaxBytes is the B01 raw-content ceiling used by bounded callers.
const DriveDownloadMaxBytes int64 = 64 * 1024

// ErrDriveDownloadSizeLimit identifies a bounded download that exceeded its cap.
var ErrDriveDownloadSizeLimit = errors.New("drive download exceeds max-bytes limit")

// DriveDownloadSizeLimitError reports a bounded download cap violation.
type DriveDownloadSizeLimitError struct {
	LimitBytes int64
}

func (e DriveDownloadSizeLimitError) Error() string {
	return fmt.Sprintf("binary_size_limit: download exceeds --max-bytes limit of %d bytes", e.LimitBytes)
}

func (e DriveDownloadSizeLimitError) Is(target error) bool {
	return target == ErrDriveDownloadSizeLimit
}

type DriveDownloadCmd struct {
	FileID    string         `arg:"" name:"fileId" help:"File ID"`
	Output    OutputPathFlag `embed:""`
	Format    string         `name:"format" help:"Export format for Google Docs files: pdf|csv|xlsx|pptx|txt|png|docx|md (default: inferred)"`
	Tab       string         `name:"tab" help:"(experimental) Export a specific tab by title or ID (Google Docs only; see 'gog docs list-tabs')"`
	Overwrite bool           `name:"overwrite" help:"Overwrite an existing output file"`
	MaxBytes  int64          `name:"max-bytes" help:"Maximum raw bytes to download (0 = unlimited)" default:"0"`
}

func (c *DriveDownloadCmd) Run(ctx context.Context, flags *RootFlags) error {
	fileID := normalizeGoogleID(strings.TrimSpace(c.FileID))
	if fileID == "" {
		return usage("empty fileId")
	}
	if c.MaxBytes < 0 {
		return usage("--max-bytes must be >= 0")
	}

	if tab := strings.TrimSpace(c.Tab); tab != "" {
		if f := c.Format; f != "" && f != formatAuto {
			if _, fmtErr := tabExportFormatParam(f); fmtErr != nil {
				return usagef("--tab limits export formats (pdf|docx|txt|md|html); %q is not supported with --tab", f)
			}
		}
		return runDocsTabExport(ctx, flags, tabExportParams{
			DocID:     fileID,
			OutFlag:   c.Output.Path,
			Format:    c.Format,
			TabQuery:  tab,
			Overwrite: c.Overwrite,
			MaxBytes:  c.MaxBytes,
		})
	}

	u := ui.FromContext(ctx)
	if formatErr := validateDriveDownloadFormatFlag(c.Format); formatErr != nil {
		return formatErr
	}

	outPathFlag := strings.TrimSpace(c.Output.Path)
	if outPathFlag != "" {
		expanded, expandErr := config.ExpandPath(outPathFlag)
		if expandErr != nil {
			return expandErr
		}
		outPathFlag = expanded
	}
	if outfmt.IsJSON(ctx) && isStdoutPath(outPathFlag) {
		return usage("can't combine --json with --out -")
	}
	defaultDir := ""
	if outPathFlag == "" {
		layout, layoutErr := commandLayout(ctx, config.PathKindConfig)
		if layoutErr != nil {
			return layoutErr
		}
		defaultDir = layout.DriveDownloadsDir()
	}
	downloadRequest := map[string]any{
		"file_id":               fileID,
		"out":                   outPathFlag,
		"default_downloads_dir": defaultDir,
		"format":                strings.ToLower(strings.TrimSpace(c.Format)),
		"overwrite":             c.Overwrite,
	}
	if c.MaxBytes > 0 {
		downloadRequest["max_bytes"] = c.MaxBytes
	}
	if dryRunErr := dryRunExit(ctx, flags, "drive.download", downloadRequest); dryRunErr != nil {
		return dryRunErr
	}

	account, err := requireAccount(flags)
	if err != nil {
		return err
	}

	svc, err := driveService(ctx, account)
	if err != nil {
		return err
	}

	meta, err := svc.Files.Get(fileID).
		SupportsAllDrives(true).
		Fields("id, name, mimeType").
		Context(ctx).
		Do()
	if err != nil {
		return err
	}
	if meta.Name == "" {
		return errors.New("file has no name")
	}
	if fileFormatErr := validateDriveDownloadFormatForFile(meta, c.Format); fileFormatErr != nil {
		return fileFormatErr
	}

	destPath, err := resolveDriveDownloadDestPath(meta, outPathFlag, defaultDir)
	if err != nil {
		return err
	}

	downloadedPath, size, err := downloadDriveFileWithMaxBytes(ctx, svc, meta, destPath, c.Format, c.Overwrite, c.MaxBytes)
	if err != nil {
		return err
	}

	if outfmt.IsJSON(ctx) {
		return outfmt.WriteJSON(ctx, stdoutWriter(ctx), map[string]any{
			"path": downloadedPath,
			"size": size,
		})
	}
	if isStdoutPath(downloadedPath) {
		return nil
	}

	u.Out().Linef("path\t%s", downloadedPath)
	u.Out().Linef("size\t%s", formatDriveSize(size))
	return nil
}

func downloadDriveFile(ctx context.Context, svc *drive.Service, meta *drive.File, destPath string, format string, overwrite bool) (string, int64, error) {
	return downloadDriveFileWithMaxBytes(ctx, svc, meta, destPath, format, overwrite, 0)
}

func downloadDriveFileWithMaxBytes(ctx context.Context, svc *drive.Service, meta *drive.File, destPath string, format string, overwrite bool, maxBytes int64) (string, int64, error) {
	if maxBytes < 0 {
		return "", 0, usage("--max-bytes must be >= 0")
	}
	isGoogleDoc := strings.HasPrefix(meta.MimeType, "application/vnd.google-apps.")
	normalizedFormat := strings.ToLower(strings.TrimSpace(format))
	if normalizedFormat == formatAuto {
		normalizedFormat = ""
	}

	if !isGoogleDoc && normalizedFormat != "" {
		return "", 0, fmt.Errorf("--format %q not supported for non-Google Workspace files (mimeType=%q); file can only be downloaded as-is", format, meta.MimeType)
	}
	if fileFormatErr := validateDriveDownloadFormatForFile(meta, format); fileFormatErr != nil {
		return "", 0, fileFormatErr
	}

	var (
		resp    *http.Response
		outPath string
		err     error
	)

	if isGoogleDoc {
		var exportMimeType string
		if normalizedFormat == "" {
			exportMimeType = driveExportMimeType(meta.MimeType)
		} else {
			var mimeErr error
			exportMimeType, mimeErr = driveExportMimeTypeForFormat(meta.MimeType, normalizedFormat)
			if mimeErr != nil {
				return "", 0, mimeErr
			}
		}
		if isStdoutPath(destPath) {
			outPath = stdoutPath
		} else {
			outPath = replaceExt(destPath, driveExportExtension(exportMimeType))
		}
		resp, err = driveExportRequest(ctx, svc, meta.Id, exportMimeType)
	} else {
		outPath = destPath
		resp, err = driveDownloadRequest(ctx, svc, meta.Id)
	}
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return "", 0, fmt.Errorf("download failed: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	return writeDriveDownloadResponse(ctx, resp.Body, outPath, overwrite, maxBytes)
}

func writeDriveDownloadResponse(ctx context.Context, body io.Reader, destPath string, overwrite bool, maxBytes int64) (string, int64, error) {
	if maxBytes <= 0 {
		if isStdoutPath(destPath) {
			n, copyErr := io.Copy(stdoutWriter(ctx), body)
			return stdoutPath, n, copyErr
		}

		f, expandedPath, err := openUserOutputFile(destPath, outputFileOptions{
			Overwrite: overwrite,
			FileMode:  0o600,
			DirMode:   0o700,
		})
		if err != nil {
			return "", 0, err
		}
		defer f.Close()

		n, copyErr := io.Copy(f, body)
		if copyErr != nil {
			return "", 0, copyErr
		}
		return expandedPath, n, nil
	}

	if isStdoutPath(destPath) {
		var buf bytes.Buffer
		n, overLimit, copyErr := copyDriveDownloadBounded(ctx, &buf, body, maxBytes)
		if copyErr != nil {
			return "", 0, copyErr
		}
		if overLimit {
			return "", 0, DriveDownloadSizeLimitError{LimitBytes: maxBytes}
		}
		if _, writeErr := stdoutWriter(ctx).Write(buf.Bytes()); writeErr != nil {
			return "", 0, writeErr
		}
		return stdoutPath, n, nil
	}

	f, tempPath, expandedPath, err := openBoundedDriveDownloadFile(destPath, overwrite)
	if err != nil {
		return "", 0, err
	}
	defer os.Remove(tempPath)

	n, overLimit, copyErr := copyDriveDownloadBounded(ctx, f, body, maxBytes)
	closeErr := f.Close()
	if copyErr != nil {
		return "", 0, copyErr
	}
	if closeErr != nil {
		return "", 0, closeErr
	}
	if overLimit {
		return "", 0, DriveDownloadSizeLimitError{LimitBytes: maxBytes}
	}
	if commitErr := commitBoundedDriveDownload(tempPath, expandedPath, overwrite); commitErr != nil {
		return "", 0, commitErr
	}
	return expandedPath, n, nil
}

func copyDriveDownloadBounded(ctx context.Context, dst io.Writer, src io.Reader, maxBytes int64) (int64, bool, error) {
	reader := driveDownloadContextReader{contextErr: ctx.Err, reader: src}
	n, err := io.Copy(dst, io.LimitReader(reader, maxBytes))
	if err != nil {
		return n, false, err
	}

	var probe [1]byte
	probeN, probeErr := reader.Read(probe[:])
	if probeN > 0 {
		return n + int64(probeN), true, nil
	}
	if probeErr != nil && !errors.Is(probeErr, io.EOF) {
		return n, false, probeErr
	}
	return n, false, nil
}

type driveDownloadContextReader struct {
	contextErr func() error
	reader     io.Reader
}

func (r driveDownloadContextReader) Read(p []byte) (int, error) {
	if err := r.contextErr(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

func openBoundedDriveDownloadFile(path string, overwrite bool) (*os.File, string, string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, "", "", errors.New("output path required")
	}
	expanded, err := config.ExpandPath(path)
	if err != nil {
		return nil, "", "", err
	}
	dir := filepath.Dir(expanded)
	if dir != "." {
		// #nosec G301,G703 -- destination directory is explicitly chosen by the caller.
		if mkdirErr := os.MkdirAll(dir, 0o700); mkdirErr != nil {
			return nil, "", "", mkdirErr
		}
	}
	if !overwrite {
		if _, statErr := os.Lstat(expanded); statErr == nil {
			return nil, "", "", &os.PathError{Op: "open", Path: expanded, Err: os.ErrExist}
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return nil, "", "", statErr
		}
	}

	f, err := os.CreateTemp(dir, ".gog-download-*")
	if err != nil {
		return nil, "", "", err
	}
	if chmodErr := f.Chmod(0o600); chmodErr != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return nil, "", "", chmodErr
	}
	return f, f.Name(), expanded, nil
}

func commitBoundedDriveDownload(tempPath, destPath string, overwrite bool) error {
	if overwrite {
		return replaceBoundedDriveDownload(tempPath, destPath)
	}
	if err := os.Link(tempPath, destPath); err != nil {
		return err
	}
	return os.Remove(tempPath)
}

func validateDriveDownloadFormatFlag(format string) error {
	format = strings.ToLower(strings.TrimSpace(format))
	if format == "" {
		return nil
	}
	switch format {
	case "pdf", "csv", "xlsx", "pptx", "txt", "png", "docx", "md", "html":
		return nil
	default:
		return usagef("invalid --format %q (use pdf|csv|xlsx|pptx|txt|png|docx|md|html)", format)
	}
}

func validateDriveDownloadFormatForFile(meta *drive.File, format string) error {
	if meta == nil {
		return errors.New("missing file metadata")
	}
	isGoogleDoc := strings.HasPrefix(meta.MimeType, "application/vnd.google-apps.")
	if isGoogleDoc {
		return nil
	}
	if strings.TrimSpace(format) == "" {
		return nil
	}
	return usagef("--format %q not supported for non-Google Workspace files (mimeType=%q); file can only be downloaded as-is", format, meta.MimeType)
}

func driveDownload(ctx context.Context, svc *drive.Service, fileID string) (*http.Response, error) {
	return svc.Files.Get(fileID).SupportsAllDrives(true).Context(ctx).Download()
}

func driveExportDownload(ctx context.Context, svc *drive.Service, fileID string, mimeType string) (*http.Response, error) {
	return svc.Files.Export(fileID, mimeType).Context(ctx).Download()
}

func replaceExt(path string, ext string) string {
	base := strings.TrimSuffix(path, filepath.Ext(path))
	return base + ext
}

func driveExportMimeType(googleMimeType string) string {
	switch googleMimeType {
	case driveMimeGoogleDoc:
		return mimePDF
	case driveMimeGoogleSheet:
		return mimeCSV
	case driveMimeGoogleSlides:
		return mimePDF
	case driveMimeGoogleDrawing:
		return mimePNG
	case driveMimeGoogleSite:
		return mimeHTML
	default:
		return mimePDF
	}
}

func driveExportMimeTypeForFormat(googleMimeType string, format string) (string, error) {
	format = strings.ToLower(strings.TrimSpace(format))
	if format == "" || format == formatAuto {
		return driveExportMimeType(googleMimeType), nil
	}

	switch googleMimeType {
	case driveMimeGoogleDoc:
		switch format {
		case defaultExportFormat:
			return mimePDF, nil
		case "docx":
			return mimeDocx, nil
		case "txt":
			return mimeTextPlain, nil
		case "md":
			return mimeTextMarkdown, nil
		case "html":
			return mimeHTML, nil
		default:
			return "", fmt.Errorf("invalid --format %q for Google Doc (use pdf|docx|txt|md|html)", format)
		}
	case driveMimeGoogleSheet:
		switch format {
		case defaultExportFormat:
			return mimePDF, nil
		case "csv":
			return mimeCSV, nil
		case "xlsx":
			return mimeXlsx, nil
		default:
			return "", fmt.Errorf("invalid --format %q for Google Sheet (use pdf|csv|xlsx)", format)
		}
	case driveMimeGoogleSlides:
		switch format {
		case defaultExportFormat:
			return mimePDF, nil
		case "pptx":
			return mimePptx, nil
		default:
			return "", fmt.Errorf("invalid --format %q for Google Slides (use pdf|pptx)", format)
		}
	case driveMimeGoogleDrawing:
		switch format {
		case "png":
			return mimePNG, nil
		case defaultExportFormat:
			return mimePDF, nil
		default:
			return "", fmt.Errorf("invalid --format %q for Google Drawing (use png|pdf)", format)
		}
	case driveMimeGoogleSite:
		return "", errors.New("google sites cannot be exported through Drive; use 'gog sites url <siteId>' to open the site")
	default:
		if format == defaultExportFormat {
			return mimePDF, nil
		}
		return "", fmt.Errorf("invalid --format %q for file type %q (use pdf)", format, googleMimeType)
	}
}

func driveExportExtension(mimeType string) string {
	switch mimeType {
	case mimePDF:
		return extPDF
	case mimeCSV:
		return extCSV
	case mimeXlsx:
		return extXlsx
	case mimeDocx:
		return extDocx
	case mimePptx:
		return extPptx
	case mimePNG:
		return extPNG
	case mimeTextPlain:
		return extTXT
	case mimeTextMarkdown:
		return extMD
	case mimeHTML:
		return extHTML
	default:
		return extPDF
	}
}
