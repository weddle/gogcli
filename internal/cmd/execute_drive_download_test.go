package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
)

func TestExecute_DriveDownload_WithOutFile_JSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !(strings.Contains(r.URL.Path, "/files/id1") && r.Method == http.MethodGet) {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":       "id1",
			"name":     "Doc",
			"mimeType": "text/plain",
		})
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

	download := func(context.Context, *drive.Service, string) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Body:       io.NopCloser(strings.NewReader("abc")),
		}, nil
	}

	outPath := filepath.Join(t.TempDir(), "out.bin")

	result := executeWithDriveTestOperations(t, []string{
		"--json",
		"--account", "a@b.com",
		"drive", "download", "id1",
		"--out", outPath,
	}, svc, download, nil)
	if result.err != nil {
		t.Fatalf("Execute: %v", result.err)
	}

	var parsed struct {
		Path string `json:"path"`
		Size int64  `json:"size"`
	}
	if unmarshalErr := json.Unmarshal([]byte(result.stdout), &parsed); unmarshalErr != nil {
		t.Fatalf("json parse: %v\nout=%q", unmarshalErr, result.stdout)
	}
	if parsed.Path != outPath || parsed.Size != 3 {
		t.Fatalf("unexpected: %#v", parsed)
	}
	b, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(b) != "abc" {
		t.Fatalf("unexpected file contents: %q", string(b))
	}
}

func TestExecute_DriveDownload_WithOutDir_JSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !(strings.Contains(r.URL.Path, "/files/id1") && r.Method == http.MethodGet) {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":       "id1",
			"name":     "Doc",
			"mimeType": "text/plain",
		})
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

	download := func(context.Context, *drive.Service, string) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Body:       io.NopCloser(strings.NewReader("abc")),
		}, nil
	}

	outDir := t.TempDir()
	wantPath := filepath.Join(outDir, "id1_Doc")

	result := executeWithDriveTestOperations(t, []string{
		"--json",
		"--account", "a@b.com",
		"drive", "download", "id1",
		"--out", outDir,
	}, svc, download, nil)
	if result.err != nil {
		t.Fatalf("Execute: %v", result.err)
	}

	var parsed struct {
		Path string `json:"path"`
		Size int64  `json:"size"`
	}
	if unmarshalErr := json.Unmarshal([]byte(result.stdout), &parsed); unmarshalErr != nil {
		t.Fatalf("json parse: %v\nout=%q", unmarshalErr, result.stdout)
	}
	if parsed.Path != wantPath || parsed.Size != 3 {
		t.Fatalf("unexpected: %#v", parsed)
	}
	if _, statErr := os.Stat(wantPath); statErr != nil {
		t.Fatalf("expected file at %s: %v", wantPath, statErr)
	}
}

func TestExecute_DriveDownload_FormatRejected_NonGoogle(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !(strings.Contains(r.URL.Path, "/files/id1") && r.Method == http.MethodGet) {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":       "id1",
			"name":     "Doc",
			"mimeType": "text/plain",
		})
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

	called := false
	download := func(context.Context, *drive.Service, string) (*http.Response, error) {
		called = true
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Body:       io.NopCloser(strings.NewReader("abc")),
		}, nil
	}

	outPath := filepath.Join(t.TempDir(), "out.pdf")
	result := executeWithDriveTestOperations(t, []string{
		"--account", "a@b.com",
		"drive", "download", "id1",
		"--format", "pdf",
		"--out", outPath,
	}, svc, download, nil)
	if result.err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(result.err.Error(), "non-Google Workspace") {
		t.Fatalf("unexpected error: %v", result.err)
	}
	if got := ExitCode(result.err); got != 2 {
		t.Fatalf("expected usage exit code 2, got %d (err=%v)", got, result.err)
	}
	if called {
		t.Fatalf("download should not be called on format error")
	}
	if _, statErr := os.Stat(outPath); !os.IsNotExist(statErr) {
		t.Fatalf("expected no file written, stat=%v", statErr)
	}
}

func TestExecute_DriveDownload_MaxBytesBoundaries(t *testing.T) {
	const limit = DriveDownloadMaxBytes
	for _, size := range []int64{limit - 1, limit, limit + 1} {
		size := size
		t.Run("size_"+strconv.FormatInt(size, 10), func(t *testing.T) {
			payload := bytes.Repeat([]byte("x"), int(size))
			svc, cleanup := newDriveMetadataTestService(t, "text/plain")
			t.Cleanup(cleanup)
			download := func(context.Context, *drive.Service, string) (*http.Response, error) {
				return &http.Response{
					Status:     "200 OK",
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewReader(payload)),
				}, nil
			}
			dest := filepath.Join(t.TempDir(), "out.bin")
			result := executeWithDriveTestOperations(t, []string{
				"--json", "--account", "a@b.com",
				"drive", "download", "id1",
				"--out", dest,
				"--max-bytes", strconv.FormatInt(limit, 10),
			}, svc, download, nil)
			if size > limit {
				if !errors.Is(result.err, ErrDriveDownloadSizeLimit) {
					t.Fatalf("error = %v, want size-limit error", result.err)
				}
				if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
					t.Fatalf("over-limit destination exists, stat=%v", statErr)
				}
				return
			}
			if result.err != nil {
				t.Fatalf("Execute: %v", result.err)
			}
			var parsed struct {
				Path string `json:"path"`
				Size int64  `json:"size"`
			}
			if err := json.Unmarshal([]byte(result.stdout), &parsed); err != nil {
				t.Fatalf("json parse: %v\nout=%q", err, result.stdout)
			}
			if parsed.Path != dest || parsed.Size != size {
				t.Fatalf("result = %#v, want path=%q size=%d", parsed, dest, size)
			}
			got, err := os.ReadFile(dest)
			if err != nil {
				t.Fatalf("read output: %v", err)
			}
			if !bytes.Equal(got, payload) {
				t.Fatalf("output length = %d, want %d", len(got), len(payload))
			}
		})
	}
}

func TestExecute_DriveDownload_MaxBytesNoOverwriteCollision(t *testing.T) {
	const limit = DriveDownloadMaxBytes
	payload := bytes.Repeat([]byte("x"), int(limit))
	svc, cleanup := newDriveMetadataTestService(t, "text/plain")
	t.Cleanup(cleanup)
	download := func(context.Context, *drive.Service, string) (*http.Response, error) {
		return &http.Response{
			Status:     "200 OK",
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewReader(payload)),
		}, nil
	}
	dest := filepath.Join(t.TempDir(), "out.bin")
	if err := os.WriteFile(dest, []byte("original"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	result := executeWithDriveTestOperations(t, []string{
		"--account", "a@b.com",
		"drive", "download", "id1",
		"--out", dest,
		"--max-bytes", strconv.FormatInt(limit, 10),
	}, svc, download, nil)
	if !errors.Is(result.err, os.ErrExist) {
		t.Fatalf("error = %v, want existing-file error", result.err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read existing destination: %v", err)
	}
	if string(got) != "original" {
		t.Fatalf("existing destination changed: %q", got)
	}
}

func TestExecute_TopLevelDownloadAlias_MaxBytes(t *testing.T) {
	const limit = DriveDownloadMaxBytes
	payload := bytes.Repeat([]byte("x"), int(limit-1))
	svc, cleanup := newDriveMetadataTestService(t, "text/plain")
	t.Cleanup(cleanup)
	download := func(context.Context, *drive.Service, string) (*http.Response, error) {
		return &http.Response{
			Status:     "200 OK",
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewReader(payload)),
		}, nil
	}
	dest := filepath.Join(t.TempDir(), "alias.bin")
	result := executeWithDriveTestOperations(t, []string{
		"--json", "--account", "a@b.com",
		"download", "id1",
		"--out", dest,
		"--max-bytes", strconv.FormatInt(limit, 10),
	}, svc, download, nil)
	if result.err != nil {
		t.Fatalf("Execute top-level alias: %v", result.err)
	}
	var parsed struct {
		Path string `json:"path"`
		Size int64  `json:"size"`
	}
	if err := json.Unmarshal([]byte(result.stdout), &parsed); err != nil {
		t.Fatalf("json parse: %v\nout=%q", err, result.stdout)
	}
	if parsed.Path != dest || parsed.Size != int64(len(payload)) {
		t.Fatalf("result = %#v, want path=%q size=%d", parsed, dest, len(payload))
	}
}
