package file_gateway

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"frostleaves/pkg/policy"
)

func fpBool(v bool) *bool    { return &v }
func fpInt64(v int64) *int64 { return &v }

func newTestGateway(t *testing.T) (*FileGateway, *policy.Store, *Authenticator, string) {
	t.Helper()
	root := t.TempDir()
	storage := filepath.Join(root, "storage")
	for _, d := range []string{"devices/dev1/private", "shared", "public"} {
		if err := os.MkdirAll(filepath.Join(storage, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	logs := filepath.Join(root, "logs")
	if err := os.MkdirAll(logs, 0o755); err != nil {
		t.Fatal(err)
	}
	audit, err := NewAuditLogger(logs)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { audit.Close() })

	cfg := GatewayConfig{
		StorageRoot:     storage,
		PublicSharedDir: filepath.Join(storage, "shared"),
	}
	quota := NewQuotaManager(QuotaConfig{})
	auth := NewAuthenticator("test-secret")
	gw := NewFileGateway(cfg, quota, audit, auth)
	pol := policy.NewStore()
	gw.SetPolicyStore(pol)
	token := auth.GenerateToken("dev1", "PC", "windows", "client")
	return gw, pol, auth, token
}

func doUpload(t *testing.T, base, token, target, filename string, content []byte) int {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if err := mw.WriteField("path", target); err != nil {
		t.Fatal(err)
	}
	part, err := mw.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatal(err)
	}
	mw.Close()

	req, err := http.NewRequest(http.MethodPost, base+"/api/v1/files/upload", &buf)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("X-Device-Id", "dev1")
	req.Header.Set("X-Device-Token", token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

func TestUploadAllowedByDefault(t *testing.T) {
	gw, _, _, token := newTestGateway(t)
	srv := httptest.NewServer(gw.mux)
	defer srv.Close()
	code := doUpload(t, srv.URL, token, "private/hello.txt", "hello.txt", []byte("hello world"))
	if code >= 300 {
		t.Fatalf("default upload should succeed, got %d", code)
	}
}

func TestUploadDeniedWhenPolicyDisablesIt(t *testing.T) {
	gw, pol, _, token := newTestGateway(t)
	if err := pol.SetDeviceOverride("dev1", &policy.Override{AllowUpload: fpBool(false)}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(gw.mux)
	defer srv.Close()
	code := doUpload(t, srv.URL, token, "private/a.txt", "a.txt", []byte("x"))
	if code != http.StatusForbidden {
		t.Fatalf("disabled upload must return 403, got %d", code)
	}
}

func TestUploadDeniedForExecutableAndDisguisedImage(t *testing.T) {
	gw, _, _, token := newTestGateway(t)
	srv := httptest.NewServer(gw.mux)
	defer srv.Close()
	if code := doUpload(t, srv.URL, token, "private/setup.exe", "setup.exe", []byte("MZ")); code != http.StatusForbidden {
		t.Fatalf("exe must be blocked, got %d", code)
	}
	mz := append([]byte{0x4D, 0x5A, 0x90, 0x00}, make([]byte, 200)...)
	if code := doUpload(t, srv.URL, token, "private/photo.png", "photo.png", mz); code != http.StatusForbidden {
		t.Fatalf("MZ payload disguised as .png must be blocked, got %d", code)
	}
}

func TestUploadDeniedWhenQuotaExceeded(t *testing.T) {
	gw, pol, _, token := newTestGateway(t)
	if err := pol.SetDeviceOverride("dev1", &policy.Override{StorageQuotaBytes: fpInt64(4)}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(gw.mux)
	defer srv.Close()
	code := doUpload(t, srv.URL, token, "private/big.txt", "big.txt", []byte("this is way more than four bytes"))
	if code != http.StatusForbidden {
		t.Fatalf("over-quota upload must return 403, got %d", code)
	}
}

func TestDeviceDeveloperCanUploadExecutable(t *testing.T) {
	gw, pol, _, token := newTestGateway(t)
	if err := pol.SetDeviceOverride("dev1", &policy.Override{AllowExecutable: fpBool(true)}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(gw.mux)
	defer srv.Close()
	code := doUpload(t, srv.URL, token, "private/tool.exe", "tool.exe", []byte("MZfake"))
	if code >= 300 {
		t.Fatalf("developer device should be allowed to upload exe, got %d", code)
	}
}
