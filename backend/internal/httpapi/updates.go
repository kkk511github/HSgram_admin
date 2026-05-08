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
	"time"

	"hsgram-admin/backend/internal/store"
)

type UpdateConfig struct {
	ReleasesDir   string
	PublicBaseURL string
}

type updateManifest struct {
	Platform                string     `json:"platform,omitempty"`
	Channel                 string     `json:"channel,omitempty"`
	Arch                    string     `json:"arch,omitempty"`
	VersionName             string     `json:"version_name,omitempty"`
	Version                 string     `json:"version"`
	VersionCode             int        `json:"version_code"`
	MinSupportedVersionCode int        `json:"min_supported_version_code,omitempty"`
	UpdateLevel             string     `json:"update_level,omitempty"`
	Force                   bool       `json:"force,omitempty"`
	Title                   string     `json:"title,omitempty"`
	Changelog               string     `json:"changelog,omitempty"`
	FileName                string     `json:"file_name,omitempty"`
	FileSize                int64      `json:"file_size,omitempty"`
	SHA256                  string     `json:"sha256,omitempty"`
	MimeType                string     `json:"mime_type,omitempty"`
	StorageKey              string     `json:"storage_key,omitempty"`
	DownloadURL             string     `json:"download_url,omitempty"`
	PublicDownloadURL       string     `json:"public_download_url,omitempty"`
	FileURL                 string     `json:"file_url,omitempty"`
	PublishedAt             *time.Time `json:"published_at,omitempty"`
}

var errInvalidUpdateManifest = errors.New("invalid update manifest")

func (h *Handler) handleAndroidUpdateLatest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	manifest, err := h.loadUpdateManifest(r, store.ReleasePlatformAndroid)
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

	manifest, err := h.loadUpdateManifest(r, store.ReleasePlatformWindows)
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

	manifest, err := h.loadUpdateManifest(r, store.ReleasePlatformWindows)
	if err != nil {
		writeUpdateLoadError(w, err)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = fmt.Fprintf(w, "%d:%s", manifest.VersionCode, manifest.DownloadURL)
}

func (h *Handler) loadUpdateManifest(r *http.Request, platform string) (updateManifest, error) {
	platform, err := store.NormalizeReleasePlatform(platform)
	if err != nil {
		return updateManifest{}, err
	}
	channel, err := store.NormalizeReleaseChannel(r.URL.Query().Get("channel"))
	if err != nil {
		return updateManifest{}, err
	}
	arch, err := store.NormalizeReleaseArch(platform, r.URL.Query().Get("arch"))
	if err != nil {
		return updateManifest{}, err
	}
	if h.store != nil {
		release, err := h.store.GetLatestAppReleaseFor(r.Context(), platform, channel, arch)
		if err == nil {
			return h.releaseManifest(r, *release), nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return updateManifest{}, err
		}
	}

	manifest, err := h.loadUpdateManifestFile(r, platform, channel, arch)
	if err != nil {
		return updateManifest{}, err
	}
	return manifest, nil
}

func (h *Handler) loadUpdateManifestFile(r *http.Request, platform, channel, arch string) (updateManifest, error) {
	paths := []string{
		filepath.Join(h.updates.ReleasesDir, platform, channel, arch, "latest.json"),
	}
	for _, key := range fallbackManifestKeys(channel, arch) {
		paths = append(paths, filepath.Join(h.updates.ReleasesDir, platform, key.channel, key.arch, "latest.json"))
	}
	paths = append(paths, filepath.Join(h.updates.ReleasesDir, platform, "latest.json"))
	if platform == store.ReleasePlatformWindows {
		paths = append(paths, filepath.Join(h.updates.ReleasesDir, "pc", "latest.json"))
	}

	var lastErr error
	for _, path := range uniqueStrings(paths) {
		var manifest updateManifest
		if err := loadManifestFile(path, &manifest); err != nil {
			lastErr = err
			continue
		}
		if err := h.normalizeLoadedManifest(r, platform, channel, arch, &manifest); err != nil {
			lastErr = err
			continue
		}
		return manifest, nil
	}
	if lastErr == nil {
		lastErr = os.ErrNotExist
	}
	return updateManifest{}, lastErr
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
	channel, err := store.NormalizeReleaseChannel(release.Channel)
	if err != nil {
		return err
	}
	arch, err := store.NormalizeReleaseArch(platform, release.Arch)
	if err != nil {
		return err
	}
	release.Platform = platform
	release.Channel = channel
	release.Arch = arch
	if h.updates.ReleasesDir == "" {
		return errors.New("releases dir is not configured")
	}

	payload := h.releaseManifest(r, release)

	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	targets := []string{
		filepath.Join(h.updates.ReleasesDir, platform, release.Channel, release.Arch, "latest.json"),
	}
	if release.Channel == store.ReleaseChannelStable && release.Arch == store.ReleaseArchUniversal {
		targets = append(targets, filepath.Join(h.updates.ReleasesDir, platform, "latest.json"))
		if platform == store.ReleasePlatformWindows {
			targets = append(targets, filepath.Join(h.updates.ReleasesDir, "pc", "latest.json"))
		}
	}
	for _, target := range uniqueStrings(targets) {
		if err := writeManifestAtomic(target, data); err != nil {
			return err
		}
	}
	return nil
}

func writeManifestAtomic(target string, data []byte) error {
	dir := filepath.Dir(target)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
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

func (h *Handler) releaseManifest(r *http.Request, release store.AppRelease) updateManifest {
	publicURL := h.releaseDownloadURL(r, release.StoragePath)
	downloadURL := strings.TrimSpace(release.DownloadURL)
	if downloadURL == "" {
		downloadURL = "/releases/" + strings.TrimLeft(release.StoragePath, "/")
	}
	publicDownloadURL := strings.TrimSpace(release.PublicDownloadURL)
	if publicDownloadURL == "" || !isAbsoluteURL(publicDownloadURL) {
		publicDownloadURL = publicURL
	}
	updateLevel := release.UpdateLevel
	if updateLevel == "" {
		updateLevel = store.ReleaseUpdateOptional
	}
	title := strings.TrimSpace(release.Title)
	if title == "" {
		title = fmt.Sprintf("HSgram %s", release.Version)
	}
	return updateManifest{
		Platform:                release.Platform,
		Channel:                 release.Channel,
		Arch:                    release.Arch,
		VersionName:             release.Version,
		Version:                 release.Version,
		VersionCode:             release.VersionCode,
		MinSupportedVersionCode: release.MinSupportedVersionCode,
		UpdateLevel:             updateLevel,
		Force:                   updateLevel == store.ReleaseUpdateRequired,
		Title:                   title,
		Changelog:               release.Changelog,
		FileName:                release.Filename,
		FileSize:                release.FileSize,
		SHA256:                  release.SHA256,
		MimeType:                release.MimeType,
		StorageKey:              release.StorageKey,
		DownloadURL:             publicDownloadURL,
		PublicDownloadURL:       publicDownloadURL,
		FileURL:                 publicDownloadURL,
		PublishedAt:             release.PublishedAt,
	}
}

func (h *Handler) normalizeLoadedManifest(r *http.Request, platform, channel, arch string, manifest *updateManifest) error {
	if manifest == nil {
		return errInvalidUpdateManifest
	}
	if manifest.Version == "" {
		manifest.Version = manifest.VersionName
	}
	if manifest.VersionName == "" {
		manifest.VersionName = manifest.Version
	}
	if manifest.Platform == "" {
		manifest.Platform = platform
	} else {
		normalized, err := store.NormalizeReleasePlatform(manifest.Platform)
		if err != nil {
			return errInvalidUpdateManifest
		}
		manifest.Platform = normalized
	}
	if manifest.Channel == "" {
		manifest.Channel = channel
	} else {
		normalized, err := store.NormalizeReleaseChannel(manifest.Channel)
		if err != nil {
			return errInvalidUpdateManifest
		}
		manifest.Channel = normalized
	}
	if manifest.Arch == "" {
		manifest.Arch = arch
	} else {
		normalized, err := store.NormalizeReleaseArch(manifest.Platform, manifest.Arch)
		if err != nil {
			return errInvalidUpdateManifest
		}
		manifest.Arch = normalized
	}
	if manifest.UpdateLevel == "" {
		if manifest.Force {
			manifest.UpdateLevel = store.ReleaseUpdateRequired
		} else {
			manifest.UpdateLevel = store.ReleaseUpdateOptional
		}
	} else {
		normalized, err := store.NormalizeReleaseUpdateLevel(manifest.UpdateLevel)
		if err != nil {
			return errInvalidUpdateManifest
		}
		manifest.UpdateLevel = normalized
		manifest.Force = normalized == store.ReleaseUpdateRequired
	}
	if manifest.DownloadURL == "" {
		manifest.DownloadURL = manifest.FileURL
	}
	if manifest.PublicDownloadURL == "" {
		manifest.PublicDownloadURL = manifest.DownloadURL
	}
	manifest.DownloadURL = h.resolvePublicURL(r, manifest.DownloadURL)
	manifest.PublicDownloadURL = h.resolvePublicURL(r, manifest.PublicDownloadURL)
	if manifest.FileURL == "" {
		manifest.FileURL = manifest.PublicDownloadURL
	} else {
		manifest.FileURL = h.resolvePublicURL(r, manifest.FileURL)
	}
	if strings.TrimSpace(manifest.Version) == "" || manifest.VersionCode <= 0 || strings.TrimSpace(manifest.PublicDownloadURL) == "" {
		return errInvalidUpdateManifest
	}
	if manifest.Title == "" {
		manifest.Title = "HSgram " + manifest.Version
	}
	return nil
}

func fallbackManifestKeys(channel, arch string) []releaseManifestKey {
	keys := []releaseManifestKey{{channel: channel, arch: arch}}
	add := func(next releaseManifestKey) {
		for _, key := range keys {
			if key == next {
				return
			}
		}
		keys = append(keys, next)
	}
	add(releaseManifestKey{channel: channel, arch: store.ReleaseArchUniversal})
	add(releaseManifestKey{channel: store.ReleaseChannelStable, arch: arch})
	add(releaseManifestKey{channel: store.ReleaseChannelStable, arch: store.ReleaseArchUniversal})
	return keys
}

type releaseManifestKey struct {
	channel string
	arch    string
}

func isAbsoluteURL(raw string) bool {
	parsed, err := url.Parse(raw)
	return err == nil && parsed.IsAbs()
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
