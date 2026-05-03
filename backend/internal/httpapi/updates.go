package httpapi

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"hsgram-admin/backend/internal/store"
)

type UpdateConfig struct {
	ReleasesDir   string
	PublicBaseURL string
}

type androidUpdateManifest struct {
	Version     string `json:"version"`
	VersionCode int    `json:"version_code"`
	FileURL     string `json:"file_url"`
	Changelog   string `json:"changelog,omitempty"`
}

type pcUpdateManifest struct {
	Version     string `json:"version,omitempty"`
	VersionCode int    `json:"version_code"`
	DownloadURL string `json:"download_url"`
	Changelog   string `json:"changelog,omitempty"`
}

var errInvalidUpdateManifest = errors.New("invalid update manifest")

func (h *Handler) handleAndroidUpdateLatest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	manifest, err := h.loadAndroidUpdateManifest(r)
	if err != nil {
		writeUpdateLoadError(w, err)
		return
	}

	writeRawJSON(w, http.StatusOK, manifest)
}

func (h *Handler) handlePCUpdateLatest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	manifest, err := h.loadPCUpdateManifest(r)
	if err != nil {
		writeUpdateLoadError(w, err)
		return
	}

	writeRawJSON(w, http.StatusOK, manifest)
}

func (h *Handler) handleTDesktopCurrent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	manifest, err := h.loadPCUpdateManifest(r)
	if err != nil {
		writeUpdateLoadError(w, err)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = fmt.Fprintf(w, "%d:%s", manifest.VersionCode, manifest.DownloadURL)
}

func (h *Handler) loadAndroidUpdateManifest(r *http.Request) (androidUpdateManifest, error) {
	if h.store != nil {
		release, err := h.store.GetLatestAppRelease(r.Context(), store.ReleasePlatformAndroid)
		if err == nil {
			return androidUpdateManifest{
				Version:     release.Version,
				VersionCode: release.VersionCode,
				FileURL:     h.releaseDownloadURL(r, release.StoragePath),
				Changelog:   release.Changelog,
			}, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return androidUpdateManifest{}, err
		}
	}

	var manifest androidUpdateManifest
	if err := loadManifestFile(filepath.Join(h.updates.ReleasesDir, "android", "latest.json"), &manifest); err != nil {
		return androidUpdateManifest{}, err
	}

	if strings.TrimSpace(manifest.Version) == "" || manifest.VersionCode <= 0 || strings.TrimSpace(manifest.FileURL) == "" {
		return androidUpdateManifest{}, errInvalidUpdateManifest
	}

	manifest.FileURL = h.resolvePublicURL(r, manifest.FileURL)
	return manifest, nil
}

func (h *Handler) loadPCUpdateManifest(r *http.Request) (pcUpdateManifest, error) {
	if h.store != nil {
		release, err := h.store.GetLatestAppRelease(r.Context(), store.ReleasePlatformPC)
		if err == nil {
			return pcUpdateManifest{
				Version:     release.Version,
				VersionCode: release.VersionCode,
				DownloadURL: h.releaseDownloadURL(r, release.StoragePath),
				Changelog:   release.Changelog,
			}, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return pcUpdateManifest{}, err
		}
	}

	var manifest pcUpdateManifest
	if err := loadManifestFile(filepath.Join(h.updates.ReleasesDir, "pc", "latest.json"), &manifest); err != nil {
		return pcUpdateManifest{}, err
	}

	if manifest.VersionCode <= 0 || strings.TrimSpace(manifest.DownloadURL) == "" {
		return pcUpdateManifest{}, errInvalidUpdateManifest
	}

	manifest.DownloadURL = h.resolvePublicURL(r, manifest.DownloadURL)
	return manifest, nil
}

func loadManifestFile(path string, target any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("%w: %v", errInvalidUpdateManifest, err)
	}
	return nil
}

func (h *Handler) writeLatestUpdateManifest(r *http.Request, release store.AppRelease) error {
	platform, err := store.NormalizeReleasePlatform(release.Platform)
	if err != nil {
		return err
	}
	if h.updates.ReleasesDir == "" {
		return errors.New("releases dir is not configured")
	}

	downloadURL := h.releaseDownloadURL(r, release.StoragePath)
	var payload any
	switch platform {
	case store.ReleasePlatformAndroid:
		payload = androidUpdateManifest{
			Version:     release.Version,
			VersionCode: release.VersionCode,
			FileURL:     downloadURL,
			Changelog:   release.Changelog,
		}
	case store.ReleasePlatformPC:
		payload = pcUpdateManifest{
			Version:     release.Version,
			VersionCode: release.VersionCode,
			DownloadURL: downloadURL,
			Changelog:   release.Changelog,
		}
	default:
		return fmt.Errorf("unsupported platform: %s", platform)
	}

	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	dir := filepath.Join(h.updates.ReleasesDir, platform)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	target := filepath.Join(dir, "latest.json")
	tmp, err := os.CreateTemp(dir, ".latest-*.json")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, target); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}

func (h *Handler) resolvePublicURL(r *http.Request, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return raw
	}
	if parsed, err := url.Parse(raw); err == nil && parsed.IsAbs() {
		return raw
	}

	path := "/" + strings.TrimLeft(raw, "./")
	base := strings.TrimRight(h.updates.PublicBaseURL, "/")
	if base == "" && r != nil {
		scheme := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto"))
		if scheme == "" {
			if r.TLS != nil {
				scheme = "https"
			} else {
				scheme = "http"
			}
		}
		base = scheme + "://" + r.Host
	}
	if base == "" {
		return path
	}
	return base + path
}

func writeUpdateLoadError(w http.ResponseWriter, err error) {
	switch {
	case os.IsNotExist(err):
		writeError(w, http.StatusNotFound, "update manifest not found")
	case errors.Is(err, errInvalidUpdateManifest):
		writeError(w, http.StatusInternalServerError, "update manifest is invalid")
	default:
		writeError(w, http.StatusInternalServerError, "load update manifest failed")
	}
}

func writeRawJSON(w http.ResponseWriter, statusCode int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(payload)
}
