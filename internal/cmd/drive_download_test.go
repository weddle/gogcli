package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
)

func TestDownloadDriveFile_RequiresOverwrite(t *testing.T) {
	download := func(context.Context, *drive.Service, string) (*http.Response, error) {
		return &http.Response{
			Status:     "200 OK",
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("replacement")),
		}, nil
	}
	ctx := withDriveTestOperations(context.Background(), &drive.Service{}, download, nil)
	dest := filepath.Join(t.TempDir(), "file.bin")
	if err := os.WriteFile(dest, []byte("original"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if _, _, err := downloadDriveFile(ctx, &drive.Service{}, &drive.File{Id: "id1", MimeType: "application/pdf"}, dest, "", false); !errors.Is(err, os.ErrExist) {
		t.Fatalf("expected existing-file error, got %v", err)
	}
	if got, err := os.ReadFile(dest); err != nil || string(got) != "original" {
		t.Fatalf("existing file changed: data=%q err=%v", got, err)
	}

	outPath, size, err := downloadDriveFile(ctx, &drive.Service{}, &drive.File{Id: "id1", MimeType: "application/pdf"}, dest, "", true)
	if err != nil {
		t.Fatalf("downloadDriveFile overwrite: %v", err)
	}
	if outPath != dest || size != int64(len("replacement")) {
		t.Fatalf("outPath=%q size=%d", outPath, size)
	}
	if got, err := os.ReadFile(dest); err != nil || string(got) != "replacement" {
		t.Fatalf("overwrite failed: data=%q err=%v", got, err)
	}
}

func TestDownloadDriveFile_NonGoogleDoc(t *testing.T) {
	body := "hello"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Files.Get(...).Download hits /drive/v3/files/{id}?alt=media
		if !(strings.Contains(r.URL.Path, "/files/") && r.URL.Query().Get("alt") == "media") {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, body)
	}))
	defer srv.Close()

	svc, err := drive.NewService(context.Background(),
		option.WithoutAuthentication(),
		option.WithHTTPClient(srv.Client()),
		option.WithEndpoint(srv.URL+"/"),
	)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	tmp := t.TempDir()
	dest := filepath.Join(tmp, "file.bin")
	ctx := withDriveTestOperations(context.Background(), svc, driveDownload, driveExportDownload)
	outPath, n, err := downloadDriveFile(ctx, svc, &drive.File{Id: "id1", MimeType: "application/pdf"}, dest, "", false)
	if err != nil {
		t.Fatalf("downloadDriveFile: %v", err)
	}
	if outPath != dest {
		t.Fatalf("unexpected outPath: %q", outPath)
	}
	if n != int64(len(body)) {
		t.Fatalf("unexpected n: %d", n)
	}
	b, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(b) != body {
		t.Fatalf("unexpected body: %q", string(b))
	}
}

func TestDownloadDriveFile_NonGoogleDocFormatRejected(t *testing.T) {
	called := false
	download := func(context.Context, *drive.Service, string) (*http.Response, error) {
		called = true
		return &http.Response{
			Status:     "200 OK",
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("ok")),
		}, nil
	}

	dest := filepath.Join(t.TempDir(), "file.html")
	ctx := withDriveTestOperations(context.Background(), &drive.Service{}, download, nil)
	_, _, err := downloadDriveFile(ctx, &drive.Service{}, &drive.File{Id: "id1", MimeType: "application/pdf"}, dest, "html", false)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "non-Google Workspace") {
		t.Fatalf("unexpected error: %v", err)
	}
	if called {
		t.Fatalf("download should not be called on format error")
	}
	if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
		t.Fatalf("expected no file written, stat=%v", statErr)
	}
}

func TestDownloadDriveFile_GoogleDocExport(t *testing.T) {
	body := "exported"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Files.Export(...).Download hits /drive/v3/files/{id}/export?mimeType=...
		if !(strings.Contains(r.URL.Path, "/export") && strings.Contains(r.URL.Path, "/files/")) {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, body)
	}))
	defer srv.Close()

	svc, err := drive.NewService(context.Background(),
		option.WithoutAuthentication(),
		option.WithHTTPClient(srv.Client()),
		option.WithEndpoint(srv.URL+"/"),
	)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	tmp := t.TempDir()
	dest := filepath.Join(tmp, "doc.txt")
	ctx := withDriveTestOperations(context.Background(), svc, driveDownload, driveExportDownload)
	outPath, n, err := downloadDriveFile(ctx, svc, &drive.File{Id: "id1", MimeType: "application/vnd.google-apps.document"}, dest, "", false)
	if err != nil {
		t.Fatalf("downloadDriveFile: %v", err)
	}
	if !strings.HasSuffix(outPath, ".pdf") {
		t.Fatalf("expected pdf outPath, got: %q", outPath)
	}
	if n != int64(len(body)) {
		t.Fatalf("unexpected n: %d", n)
	}
	b, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(b) != body {
		t.Fatalf("unexpected body: %q", string(b))
	}
}

func TestDownloadDriveFile_HTTPError(t *testing.T) {
	download := func(context.Context, *drive.Service, string) (*http.Response, error) {
		return &http.Response{
			Status:     "403 Forbidden",
			StatusCode: http.StatusForbidden,
			Body:       io.NopCloser(strings.NewReader("nope\n")),
		}, nil
	}

	tmp := t.TempDir()
	dest := filepath.Join(tmp, "file.bin")
	ctx := withDriveTestOperations(context.Background(), &drive.Service{}, download, nil)
	_, _, err := downloadDriveFile(ctx, &drive.Service{}, &drive.File{Id: "id1", MimeType: "application/pdf"}, dest, "", false)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "download failed") || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDownloadDriveFile_CreatesMissingParentDirs(t *testing.T) {
	download := func(context.Context, *drive.Service, string) (*http.Response, error) {
		return &http.Response{
			Status:     "200 OK",
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("x")),
		}, nil
	}

	tmp := t.TempDir()
	dest := filepath.Join(tmp, "no-such-dir", "file.bin")
	ctx := withDriveTestOperations(context.Background(), &drive.Service{}, download, nil)
	outPath, size, err := downloadDriveFile(ctx, &drive.Service{}, &drive.File{Id: "id1", MimeType: "application/pdf"}, dest, "", false)
	if err != nil {
		t.Fatalf("downloadDriveFile: %v", err)
	}
	if outPath != dest {
		t.Fatalf("outPath=%q, want %q", outPath, dest)
	}
	if size != 1 {
		t.Fatalf("size=%d, want 1", size)
	}
	data, readErr := os.ReadFile(dest)
	if readErr != nil {
		t.Fatalf("read: %v", readErr)
	}
	if string(data) != "x" {
		t.Fatalf("data=%q, want %q", string(data), "x")
	}
}

func TestDownloadDriveFile_MaxBytesBoundaries(t *testing.T) {
	t.Parallel()

	const limit = DriveDownloadMaxBytes
	for _, size := range []int64{limit - 1, limit, limit + 1} {
		size := size
		t.Run(fmt.Sprintf("size_%d", size), func(t *testing.T) {
			t.Parallel()

			payload := bytes.Repeat([]byte("x"), int(size))
			download := func(context.Context, *drive.Service, string) (*http.Response, error) {
				return &http.Response{
					Status:     "200 OK",
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewReader(payload)),
				}, nil
			}
			dest := filepath.Join(t.TempDir(), "nested", "file.bin")
			ctx := withDriveTestOperations(context.Background(), &drive.Service{}, download, nil)
			outPath, gotSize, err := downloadDriveFileWithMaxBytes(ctx, &drive.Service{}, &drive.File{Id: "id1", MimeType: "application/pdf"}, dest, "", false, limit)
			if size > limit {
				if !errors.Is(err, ErrDriveDownloadSizeLimit) {
					t.Fatalf("error = %v, want size-limit error", err)
				}
				if outPath != "" || gotSize != 0 {
					t.Fatalf("over-limit result = (%q, %d), want empty path and zero size", outPath, gotSize)
				}
				if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
					t.Fatalf("over-limit destination exists, stat error = %v", statErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("bounded download: %v", err)
			}
			if outPath != dest || gotSize != size {
				t.Fatalf("result = (%q, %d), want (%q, %d)", outPath, gotSize, dest, size)
			}
			got, readErr := os.ReadFile(dest)
			if readErr != nil {
				t.Fatalf("read bounded output: %v", readErr)
			}
			if !bytes.Equal(got, payload) {
				t.Fatalf("output length = %d, want %d", len(got), len(payload))
			}
		})
	}
}

func TestDownloadDriveFile_MaxBytesLeavesExistingDestination(t *testing.T) {
	payload := bytes.Repeat([]byte("x"), int(DriveDownloadMaxBytes)+1)
	download := func(context.Context, *drive.Service, string) (*http.Response, error) {
		return &http.Response{
			Status:     "200 OK",
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewReader(payload)),
		}, nil
	}
	dest := filepath.Join(t.TempDir(), "file.bin")
	if err := os.WriteFile(dest, []byte("original"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	ctx := withDriveTestOperations(context.Background(), &drive.Service{}, download, nil)
	_, _, err := downloadDriveFileWithMaxBytes(ctx, &drive.Service{}, &drive.File{Id: "id1", MimeType: "application/pdf"}, dest, "", true, DriveDownloadMaxBytes)
	if !errors.Is(err, ErrDriveDownloadSizeLimit) {
		t.Fatalf("error = %v, want size-limit error", err)
	}
	got, readErr := os.ReadFile(dest)
	if readErr != nil {
		t.Fatalf("read existing destination: %v", readErr)
	}
	if string(got) != "original" {
		t.Fatalf("existing destination changed: %q", got)
	}
}

func TestDownloadDriveFile_MaxBytesRequiresOverwrite(t *testing.T) {
	payload := bytes.Repeat([]byte("x"), int(DriveDownloadMaxBytes-1))
	download := func(context.Context, *drive.Service, string) (*http.Response, error) {
		return &http.Response{
			Status:     "200 OK",
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewReader(payload)),
		}, nil
	}
	dest := filepath.Join(t.TempDir(), "file.bin")
	if err := os.WriteFile(dest, []byte("original"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	ctx := withDriveTestOperations(context.Background(), &drive.Service{}, download, nil)

	_, _, err := downloadDriveFileWithMaxBytes(ctx, &drive.Service{}, &drive.File{Id: "id1", MimeType: "application/pdf"}, dest, "", false, DriveDownloadMaxBytes)
	if !errors.Is(err, os.ErrExist) {
		t.Fatalf("error = %v, want existing-file error", err)
	}
	got, readErr := os.ReadFile(dest)
	if readErr != nil {
		t.Fatalf("read existing destination: %v", readErr)
	}
	if string(got) != "original" {
		t.Fatalf("existing destination changed: %q", got)
	}
}

func TestDownloadDriveFile_MaxBytesOverwritesExistingDestination(t *testing.T) {
	payload := bytes.Repeat([]byte("x"), int(DriveDownloadMaxBytes))
	download := func(context.Context, *drive.Service, string) (*http.Response, error) {
		return &http.Response{
			Status:     "200 OK",
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewReader(payload)),
		}, nil
	}
	dest := filepath.Join(t.TempDir(), "file.bin")
	if err := os.WriteFile(dest, []byte("original"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	ctx := withDriveTestOperations(context.Background(), &drive.Service{}, download, nil)

	outPath, size, err := downloadDriveFileWithMaxBytes(ctx, &drive.Service{}, &drive.File{Id: "id1", MimeType: "application/pdf"}, dest, "", true, DriveDownloadMaxBytes)
	if err != nil {
		t.Fatalf("bounded overwrite: %v", err)
	}
	if outPath != dest || size != DriveDownloadMaxBytes {
		t.Fatalf("result = (%q, %d), want (%q, %d)", outPath, size, dest, DriveDownloadMaxBytes)
	}
	got, readErr := os.ReadFile(dest)
	if readErr != nil {
		t.Fatalf("read overwritten destination: %v", readErr)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("overwritten destination length = %d, want %d", len(got), len(payload))
	}
}

func TestDownloadDriveFile_MaxBytesPreservesPrivateModes(t *testing.T) {
	download := func(context.Context, *drive.Service, string) (*http.Response, error) {
		return &http.Response{
			Status:     "200 OK",
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("bounded")),
		}, nil
	}
	dir := filepath.Join(t.TempDir(), "private")
	dest := filepath.Join(dir, "file.bin")
	ctx := withDriveTestOperations(context.Background(), &drive.Service{}, download, nil)
	if _, _, err := downloadDriveFileWithMaxBytes(ctx, &drive.Service{}, &drive.File{Id: "id1", MimeType: "application/pdf"}, dest, "", false, DriveDownloadMaxBytes); err != nil {
		t.Fatalf("bounded download: %v", err)
	}
	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat output directory: %v", err)
	}
	if got := dirInfo.Mode().Perm(); got != 0o700 {
		t.Fatalf("directory mode = %o, want 700", got)
	}
	fileInfo, err := os.Stat(dest)
	if err != nil {
		t.Fatalf("stat output file: %v", err)
	}
	if got := fileInfo.Mode().Perm(); got != 0o600 {
		t.Fatalf("file mode = %o, want 600", got)
	}
}

func TestDownloadDriveFile_MaxBytesCancellationLeavesNoOutput(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	download := func(context.Context, *drive.Service, string) (*http.Response, error) {
		return &http.Response{
			Status:     "200 OK",
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("cancelled")),
		}, nil
	}
	dest := filepath.Join(t.TempDir(), "file.bin")
	ctx = withDriveTestOperations(ctx, &drive.Service{}, download, nil)
	_, _, err := downloadDriveFileWithMaxBytes(ctx, &drive.Service{}, &drive.File{Id: "id1", MimeType: "application/pdf"}, dest, "", false, DriveDownloadMaxBytes)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
		t.Fatalf("cancelled destination exists, stat error = %v", statErr)
	}
}

func TestDownloadDriveFile_MaxBytesStdoutHasNoPartialOutput(t *testing.T) {
	payload := bytes.Repeat([]byte("x"), int(DriveDownloadMaxBytes)+1)
	download := func(context.Context, *drive.Service, string) (*http.Response, error) {
		return &http.Response{
			Status:     "200 OK",
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewReader(payload)),
		}, nil
	}
	var stdout bytes.Buffer
	ctx := newCmdRuntimeOutputContext(t, &stdout, io.Discard)
	ctx = withDriveTestOperations(ctx, &drive.Service{}, download, nil)
	_, _, err := downloadDriveFileWithMaxBytes(ctx, &drive.Service{}, &drive.File{Id: "id1", MimeType: "application/pdf"}, stdoutPath, "", false, DriveDownloadMaxBytes)
	if !errors.Is(err, ErrDriveDownloadSizeLimit) {
		t.Fatalf("error = %v, want size-limit error", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout length = %d, want no partial output", stdout.Len())
	}
}
