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

func TestExecute_DocsExport_JSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || !strings.Contains(r.URL.Path, "/files/id1") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":       "id1",
			"name":     "Doc",
			"mimeType": "application/vnd.google-apps.document",
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

	var gotExportMime string
	export := func(_ context.Context, _ *drive.Service, _ string, mimeType string) (*http.Response, error) {
		gotExportMime = mimeType
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Body:       io.NopCloser(strings.NewReader("abc")),
		}, nil
	}

	outBase := filepath.Join(t.TempDir(), "out")
	if writeErr := os.WriteFile(outBase+".docx", []byte("original"), 0o600); writeErr != nil {
		t.Fatalf("WriteFile: %v", writeErr)
	}

	result := executeWithDriveTestOperations(t, []string{
		"--json",
		"--account", "a@b.com",
		"docs", "export", "id1",
		"--out", outBase,
		"--format", "docx",
	}, svc, nil, export)
	if !errors.Is(result.err, os.ErrExist) {
		t.Fatalf("expected existing-file error, got %v", result.err)
	}
	if got, readErr := os.ReadFile(outBase + ".docx"); readErr != nil || string(got) != "original" {
		t.Fatalf("existing file changed: data=%q err=%v", got, readErr)
	}

	result = executeWithDriveTestOperations(t, []string{
		"--json",
		"--account", "a@b.com",
		"docs", "export", "id1",
		"--out", outBase,
		"--format", "docx",
		"--overwrite",
	}, svc, nil, export)
	if result.err != nil {
		t.Fatalf("Execute overwrite: %v", result.err)
	}

	var parsed struct {
		Path string `json:"path"`
		Size int64  `json:"size"`
	}
	if unmarshalErr := json.Unmarshal([]byte(result.stdout), &parsed); unmarshalErr != nil {
		t.Fatalf("json parse: %v\nout=%q", unmarshalErr, result.stdout)
	}
	if want := outBase + ".docx"; parsed.Path != want || parsed.Size != 3 {
		t.Fatalf("unexpected: %#v", parsed)
	}
	if gotExportMime != "application/vnd.openxmlformats-officedocument.wordprocessingml.document" {
		t.Fatalf("unexpected export mime type: %q", gotExportMime)
	}
	b, err := os.ReadFile(outBase + ".docx")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(b) != "abc" {
		t.Fatalf("unexpected file contents: %q", string(b))
	}
}

func TestExecute_DocsExport_Markdown(t *testing.T) {
	assertExecuteDocsExport(t, "md", "text/markdown", "# Doc\n")
}

func TestExecute_DocsExport_HTML(t *testing.T) {
	assertExecuteDocsExport(t, "html", "text/html", "<h1>Doc</h1>\n")
}

func assertExecuteDocsExport(t *testing.T, format, wantMime, body string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || !strings.Contains(r.URL.Path, "/files/id1") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":       "id1",
			"name":     "Doc",
			"mimeType": "application/vnd.google-apps.document",
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

	var gotExportMime string
	export := func(_ context.Context, _ *drive.Service, _ string, mimeType string) (*http.Response, error) {
		gotExportMime = mimeType
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Body:       io.NopCloser(strings.NewReader(body)),
		}, nil
	}

	outBase := filepath.Join(t.TempDir(), "out")

	result := executeWithDriveTestOperations(t, []string{
		"--json",
		"--account", "a@b.com",
		"docs", "export", "id1",
		"--out", outBase,
		"--format", format,
	}, svc, nil, export)
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
	if want := outBase + "." + format; parsed.Path != want || parsed.Size != int64(len(body)) {
		t.Fatalf("unexpected: %#v", parsed)
	}
	if gotExportMime != wantMime {
		t.Fatalf("unexpected export mime type: %q", gotExportMime)
	}
	b, err := os.ReadFile(outBase + "." + format)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(b) != body {
		t.Fatalf("unexpected file contents: %q", string(b))
	}
}

func TestExecute_DocsExport_TypeMismatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || !strings.Contains(r.URL.Path, "/files/id1") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":       "id1",
			"name":     "NotADoc",
			"mimeType": "application/pdf",
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

	result := executeWithDriveTestService(t, []string{"--account", "a@b.com", "docs", "export", "id1", "--out", filepath.Join(t.TempDir(), "out")}, svc)
	if result.err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(result.stderr, "file is not a Google Doc") {
		t.Fatalf("unexpected stderr=%q", result.stderr)
	}
}

func TestExecute_SheetsExport_DefaultFormat_XLSX(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || !strings.Contains(r.URL.Path, "/files/id1") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":       "id1",
			"name":     "Sheet",
			"mimeType": "application/vnd.google-apps.spreadsheet",
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

	var gotExportMime string
	export := func(_ context.Context, _ *drive.Service, _ string, mimeType string) (*http.Response, error) {
		gotExportMime = mimeType
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Body:       io.NopCloser(strings.NewReader("abc")),
		}, nil
	}

	outBase := filepath.Join(t.TempDir(), "out")

	result := executeWithDriveTestOperations(t, []string{
		"--json",
		"--account", "a@b.com",
		"sheets", "export", "id1",
		"--out", outBase,
	}, svc, nil, export)
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
	if want := outBase + ".xlsx"; parsed.Path != want || parsed.Size != 3 {
		t.Fatalf("unexpected: %#v", parsed)
	}
	if gotExportMime != "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" {
		t.Fatalf("unexpected export mime type: %q", gotExportMime)
	}
	if _, err := os.Stat(outBase + ".xlsx"); err != nil {
		t.Fatalf("expected file at %s: %v", outBase+".xlsx", err)
	}
}

func TestExecute_SheetsExport_PDF(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || !strings.Contains(r.URL.Path, "/files/id1") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":       "id1",
			"name":     "Sheet",
			"mimeType": "application/vnd.google-apps.spreadsheet",
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

	var gotExportMime string
	export := func(_ context.Context, _ *drive.Service, _ string, mimeType string) (*http.Response, error) {
		gotExportMime = mimeType
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Body:       io.NopCloser(strings.NewReader("abc")),
		}, nil
	}

	outBase := filepath.Join(t.TempDir(), "out")

	result := executeWithDriveTestOperations(t, []string{
		"--json",
		"--account", "a@b.com",
		"sheets", "export", "id1",
		"--out", outBase,
		"--format", "pdf",
	}, svc, nil, export)
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
	if want := outBase + ".pdf"; parsed.Path != want || parsed.Size != 3 {
		t.Fatalf("unexpected: %#v", parsed)
	}
	if gotExportMime != "application/pdf" {
		t.Fatalf("unexpected export mime type: %q", gotExportMime)
	}
}

func TestExecute_SlidesExport_DefaultFormat_PPTX(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || !strings.Contains(r.URL.Path, "/files/id1") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":       "id1",
			"name":     "Deck",
			"mimeType": "application/vnd.google-apps.presentation",
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

	var gotExportMime string
	export := func(_ context.Context, _ *drive.Service, _ string, mimeType string) (*http.Response, error) {
		gotExportMime = mimeType
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Body:       io.NopCloser(strings.NewReader("abc")),
		}, nil
	}

	outBase := filepath.Join(t.TempDir(), "out")

	result := executeWithDriveTestOperations(t, []string{
		"--json",
		"--account", "a@b.com",
		"slides", "export", "id1",
		"--out", outBase,
	}, svc, nil, export)
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
	if want := outBase + ".pptx"; parsed.Path != want || parsed.Size != 3 {
		t.Fatalf("unexpected: %#v", parsed)
	}
	if gotExportMime != "application/vnd.openxmlformats-officedocument.presentationml.presentation" {
		t.Fatalf("unexpected export mime type: %q", gotExportMime)
	}
}

func TestExecute_SharedExport_MaxBytesBoundaries(t *testing.T) {
	const limit = DriveDownloadMaxBytes
	tests := []struct {
		name string
		args []string
		mime string
		ext  string
	}{
		{
			name: "docs",
			args: []string{"docs", "export", "id1"},
			mime: "application/vnd.google-apps.document",
			ext:  "pdf",
		},
		{
			name: "sheets",
			args: []string{"sheets", "export", "id1"},
			mime: "application/vnd.google-apps.spreadsheet",
			ext:  "xlsx",
		},
		{
			name: "slides",
			args: []string{"slides", "export", "id1"},
			mime: "application/vnd.google-apps.presentation",
			ext:  "pptx",
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			for _, size := range []int64{limit - 1, limit, limit + 1} {
				size := size
				t.Run("size_"+strconv.FormatInt(size, 10), func(t *testing.T) {
					payload := bytes.Repeat([]byte("x"), int(size))
					svc, cleanup := newDriveMetadataTestService(t, tt.mime)
					t.Cleanup(cleanup)
					export := func(context.Context, *drive.Service, string, string) (*http.Response, error) {
						return &http.Response{
							Status:     "200 OK",
							StatusCode: http.StatusOK,
							Body:       io.NopCloser(bytes.NewReader(payload)),
						}, nil
					}
					outBase := filepath.Join(t.TempDir(), "out")
					args := append([]string{"--json", "--account", "a@b.com"}, tt.args...)
					args = append(args,
						"--out", outBase,
						"--max-bytes", strconv.FormatInt(limit, 10),
					)
					result := executeWithDriveTestOperations(t, args, svc, nil, export)
					wantPath := outBase + "." + tt.ext
					if size > limit {
						if !errors.Is(result.err, ErrDriveDownloadSizeLimit) {
							t.Fatalf("error = %v, want size-limit error", result.err)
						}
						if _, statErr := os.Stat(wantPath); !os.IsNotExist(statErr) {
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
					if parsed.Path != wantPath || parsed.Size != size {
						t.Fatalf("result = %#v, want path=%q size=%d", parsed, wantPath, size)
					}
					got, err := os.ReadFile(wantPath)
					if err != nil {
						t.Fatalf("read output: %v", err)
					}
					if !bytes.Equal(got, payload) {
						t.Fatalf("output length = %d, want %d", len(got), len(payload))
					}
				})
			}
		})
	}
}
