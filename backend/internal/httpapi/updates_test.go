package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"hsgram-admin/backend/internal/store"
)

func TestAndroidLatestManifestUsesPublicBaseURL(t *testing.T) {
	t.Parallel()

	releasesDir := t.TempDir()
	mustWriteFile(t, filepath.Join(releasesDir, "android", "latest.json"), `{
  "version": "1.2.3",
  "version_code": 123,
  "file_url": "/releases/android/HSgram-android-1.2.3.apk",
  "changelog": "bug fixes"
}`)

	handler := New(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, UpdateConfig{
		ReleasesDir:   releasesDir,
		PublicBaseURL: "https://admin.example.com",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/updates/android/latest", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rec.Code, rec.Body.String())
	}

	var manifest androidUpdateManifest
	if err := json.NewDecoder(rec.Body).Decode(&manifest); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if manifest.FileURL != "https://admin.example.com/releases/android/HSgram-android-1.2.3.apk" {
		t.Fatalf("unexpected file_url: %q", manifest.FileURL)
	}
}

func TestTDesktopCurrentUsesRequestHostForRelativeURL(t *testing.T) {
	t.Parallel()

	releasesDir := t.TempDir()
	mustWriteFile(t, filepath.Join(releasesDir, "pc", "latest.json"), `{
  "version": "2.0.0",
  "version_code": 2000,
  "download_url": "releases/pc/HSgram-pc-2.0.0.exe"
}`)

	handler := New(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, UpdateConfig{
		ReleasesDir: releasesDir,
	})

	req := httptest.NewRequest(http.MethodGet, "https://updates.example.com/td/current", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rec.Code, rec.Body.String())
	}

	got := rec.Body.String()
	want := "2000:https://updates.example.com/releases/pc/HSgram-pc-2.0.0.exe"
	if got != want {
		t.Fatalf("unexpected current body: got %q want %q", got, want)
	}
}

func TestWriteLatestUpdateManifestMatchesPublicLatestShape(t *testing.T) {
	t.Parallel()

	releasesDir := t.TempDir()
	handler := &Handler{
		updates: UpdateConfig{
			ReleasesDir:   releasesDir,
			PublicBaseURL: "https://admin.example.com",
		},
	}
	req := httptest.NewRequest(http.MethodPost, "/api/admin/releases/1/publish", nil)
	release := store.AppRelease{
		Platform:    store.ReleasePlatformAndroid,
		Version:     "3.2.1",
		VersionCode: 321,
		StoragePath: "android/HSgram-android-3.2.1.apk",
		Changelog:   "manifest changelog",
	}

	if err := handler.writeLatestUpdateManifest(req, release); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	var manifest androidUpdateManifest
	if err := loadManifestFile(filepath.Join(releasesDir, "android", "latest.json"), &manifest); err != nil {
		t.Fatalf("load manifest: %v", err)
	}
	if manifest.Version != "3.2.1" || manifest.VersionCode != 321 {
		t.Fatalf("unexpected version payload: %#v", manifest)
	}
	if manifest.FileURL != "https://admin.example.com/releases/android/HSgram-android-3.2.1.apk" {
		t.Fatalf("unexpected file_url: %q", manifest.FileURL)
	}
	if manifest.Changelog != "manifest changelog" {
		t.Fatalf("unexpected changelog: %q", manifest.Changelog)
	}
}

func mustWriteFile(t *testing.T, path string, contents string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
}
