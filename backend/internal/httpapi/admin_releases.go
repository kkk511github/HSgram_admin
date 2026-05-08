package httpapi

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"hsgram-admin/backend/internal/broadcast"
	"hsgram-admin/backend/internal/store"
)

const (
	maxReleaseUploadSize = 2 << 30
)

func (h *Handler) handleReleaseUpload(w http.ResponseWriter, r *http.Request, admin store.AdminUser) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxReleaseUploadSize)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		log.Printf("release upload parse multipart failed: admin=%s remote=%s content_type=%q content_length=%d err=%v",
			admin.Username,
			r.RemoteAddr,
			r.Header.Get("Content-Type"),
			r.ContentLength,
			err,
		)
		writeError(w, http.StatusBadRequest, "invalid multipart form")
		return
	}

	platform, err := store.NormalizeReleasePlatform(r.FormValue("platform"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	channel, err := store.NormalizeReleaseChannel(r.FormValue("channel"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	arch, err := store.NormalizeReleaseArch(platform, r.FormValue("arch"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	updateLevel, err := store.NormalizeReleaseUpdateLevel(r.FormValue("updateLevel"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	version := strings.TrimSpace(r.FormValue("version"))
	versionCode, err := strconv.Atoi(strings.TrimSpace(r.FormValue("versionCode")))
	if err != nil || versionCode <= 0 {
		writeError(w, http.StatusBadRequest, "versionCode must be a positive integer")
		return
	}
	minSupportedVersionCode := 0
	if raw := strings.TrimSpace(r.FormValue("minSupportedVersionCode")); raw != "" {
		minSupportedVersionCode, err = strconv.Atoi(raw)
		if err != nil || minSupportedVersionCode < 0 {
			writeError(w, http.StatusBadRequest, "minSupportedVersionCode must be zero or a positive integer")
			return
		}
	}
	title := strings.TrimSpace(r.FormValue("title"))
	changelog := strings.TrimSpace(r.FormValue("changelog"))

	file, header, err := r.FormFile("file")
	if err != nil {
		log.Printf("release upload missing file: admin=%s remote=%s content_type=%q err=%v",
			admin.Username,
			r.RemoteAddr,
			r.Header.Get("Content-Type"),
			err,
		)
		writeError(w, http.StatusBadRequest, "release file is required")
		return
	}
	defer file.Close()

	storagePath, filename, mimeType, err := h.prepareReleasePath(platform, channel, arch, version, header)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	relativeDownloadURL := "/releases/" + strings.TrimLeft(storagePath, "/")
	publicDownloadURL := h.resolvePublicURL(r, relativeDownloadURL)

	fileSize, sha256Hex, err := h.writeUploadedRelease(storagePath, file)
	if err != nil {
		log.Printf("release upload save file failed: admin=%s platform=%s storage_path=%q err=%v",
			admin.Username,
			platform,
			storagePath,
			err,
		)
		writeError(w, http.StatusInternalServerError, "save release file failed")
		return
	}

	release, err := h.store.CreateAppRelease(r.Context(), admin, store.CreateAppReleaseParams{
		Platform:                platform,
		Channel:                 channel,
		Arch:                    arch,
		Version:                 version,
		VersionCode:             versionCode,
		MinSupportedVersionCode: minSupportedVersionCode,
		UpdateLevel:             updateLevel,
		Title:                   title,
		Filename:                filename,
		StoragePath:             storagePath,
		StorageKey:              storagePath,
		FileSize:                fileSize,
		SHA256:                  sha256Hex,
		MimeType:                mimeType,
		DownloadURL:             relativeDownloadURL,
		PublicDownloadURL:       publicDownloadURL,
		Changelog:               changelog,
	})
	if err != nil {
		_ = os.Remove(filepath.Join(h.updates.ReleasesDir, filepath.FromSlash(storagePath)))
		log.Printf("release upload create record failed: admin=%s platform=%s storage_path=%q err=%v",
			admin.Username,
			platform,
			storagePath,
			err,
		)
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	_ = h.store.CreateAuditLog(r.Context(), admin, "release.upload", 0, map[string]any{
		"releaseId":   release.ID,
		"platform":    release.Platform,
		"channel":     release.Channel,
		"arch":        release.Arch,
		"version":     release.Version,
		"versionCode": release.VersionCode,
		"filename":    release.Filename,
		"fileSize":    release.FileSize,
	})

	writeJSON(w, http.StatusOK, apiResponse{
		OK:   true,
		Data: h.releasePayload(r, *release),
	})
}

func (h *Handler) handleReleaseRoutes(w http.ResponseWriter, r *http.Request, admin store.AdminUser) {
	path := strings.TrimPrefix(r.URL.Path, "/api/admin/releases")
	path = strings.Trim(path, "/")

	if path == "" {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		h.handleReleaseList(w, r, admin)
		return
	}

	parts := strings.Split(path, "/")
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid release id")
		return
	}

	if len(parts) == 1 {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		h.handleReleaseDetail(w, r, admin, id)
		return
	}

	switch parts[1] {
	case "publish":
		h.handleReleasePublish(w, r, admin, id)
	default:
		writeError(w, http.StatusNotFound, "route not found")
	}
}

func (h *Handler) handleReleaseList(w http.ResponseWriter, r *http.Request, admin store.AdminUser) {
	items, err := h.store.ListAppReleases(r.Context(), 100)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "load releases failed")
		return
	}

	payload := make([]any, 0, len(items))
	for _, release := range items {
		payload = append(payload, h.releasePayload(r, release))
	}

	writeJSON(w, http.StatusOK, apiResponse{
		OK:   true,
		Data: payload,
	})
}

func (h *Handler) handleReleaseDetail(w http.ResponseWriter, r *http.Request, admin store.AdminUser, releaseID int64) {
	release, err := h.store.GetAppRelease(r.Context(), releaseID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "release not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "load release failed")
		return
	}

	writeJSON(w, http.StatusOK, apiResponse{
		OK:   true,
		Data: h.releasePayload(r, *release),
	})
}

func (h *Handler) handleReleasePublish(w http.ResponseWriter, r *http.Request, admin store.AdminUser, releaseID int64) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		NotifyUsers bool   `json:"notifyUsers"`
		MessageText string `json:"messageText"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if req.NotifyUsers && h.broadcasts == nil {
		writeError(w, http.StatusServiceUnavailable, "broadcast feature is disabled")
		return
	}

	release, err := h.store.PublishAppRelease(r.Context(), releaseID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "release not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "publish release failed")
		return
	}
	if err := h.writeLatestUpdateManifest(r, *release); err != nil {
		log.Printf("admin-api: write latest release manifest failed: %v", err)
		writeError(w, http.StatusInternalServerError, "publish release manifest failed")
		return
	}

	var broadcastRecord *store.BroadcastRecord
	var broadcastError string
	if req.NotifyUsers {
		messageText := strings.TrimSpace(req.MessageText)
		if messageText == "" {
			messageText = defaultReleaseBroadcastMessage(*release, h.releaseDownloadURL(r, release.StoragePath))
		}
		record, _, err := h.broadcasts.Enqueue(r.Context(), admin, broadcast.Request{
			MessageText: messageText,
			TargetType:  store.BroadcastTargetTypeAll,
		})
		if err != nil {
			broadcastError = err.Error()
		} else {
			broadcastRecord = record
		}
	}

	metadata := map[string]any{
		"releaseId":   release.ID,
		"platform":    release.Platform,
		"channel":     release.Channel,
		"arch":        release.Arch,
		"version":     release.Version,
		"versionCode": release.VersionCode,
		"notifyUsers": req.NotifyUsers,
	}
	if broadcastRecord != nil {
		metadata["broadcastId"] = broadcastRecord.ID
	}
	if broadcastError != "" {
		metadata["broadcastError"] = broadcastError
	}
	_ = h.store.CreateAuditLog(r.Context(), admin, "release.publish", 0, metadata)

	response := map[string]any{
		"release": h.releasePayload(r, *release),
	}
	if broadcastRecord != nil {
		response["broadcast"] = broadcastRecord
	}
	if broadcastError != "" {
		response["broadcastError"] = broadcastError
	}

	writeJSON(w, http.StatusOK, apiResponse{
		OK:   true,
		Data: response,
	})
}

func (h *Handler) prepareReleasePath(platform, channel, arch, version string, header *multipart.FileHeader) (string, string, string, error) {
	version = sanitizeReleaseToken(version)
	if version == "" {
		return "", "", "", fmt.Errorf("version is required")
	}
	channel = sanitizeReleaseToken(channel)
	arch = sanitizeReleaseToken(arch)
	if channel == "" {
		channel = store.ReleaseChannelStable
	}
	if arch == "" {
		arch = store.ReleaseArchUniversal
	}

	ext := strings.ToLower(filepath.Ext(header.Filename))
	mimeType := "application/octet-stream"
	switch platform {
	case store.ReleasePlatformAndroid:
		if ext != ".apk" {
			return "", "", "", fmt.Errorf("android release must be an .apk file")
		}
		mimeType = "application/vnd.android.package-archive"
	case store.ReleasePlatformWindows:
		if ext != ".exe" {
			return "", "", "", fmt.Errorf("windows release must be an .exe file")
		}
		mimeType = "application/vnd.microsoft.portable-executable"
	}

	baseName := fmt.Sprintf("HSgram-%s-%s-%s-%s%s", platform, channel, arch, version, ext)
	baseName = sanitizeFilename(baseName)
	if baseName == "" {
		return "", "", "", fmt.Errorf("invalid release filename")
	}

	storagePath := filepath.ToSlash(filepath.Join(platform, channel, arch, baseName))
	absolutePath := filepath.Join(h.updates.ReleasesDir, filepath.FromSlash(storagePath))
	if _, err := os.Stat(absolutePath); err == nil {
		nameOnly := strings.TrimSuffix(baseName, ext)
		baseName = fmt.Sprintf("%s-%d%s", nameOnly, time.Now().Unix(), ext)
		storagePath = filepath.ToSlash(filepath.Join(platform, channel, arch, baseName))
	}
	return storagePath, baseName, mimeType, nil
}

func (h *Handler) writeUploadedRelease(storagePath string, src multipart.File) (int64, string, error) {
	targetPath := filepath.Join(h.updates.ReleasesDir, filepath.FromSlash(storagePath))
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return 0, "", err
	}

	tmpFile, err := os.CreateTemp(filepath.Dir(targetPath), ".upload-*")
	if err != nil {
		return 0, "", err
	}
	tmpPath := tmpFile.Name()
	defer func() {
		_ = tmpFile.Close()
	}()

	hasher := sha256.New()
	size, err := io.Copy(io.MultiWriter(tmpFile, hasher), src)
	if err != nil {
		_ = os.Remove(tmpPath)
		return 0, "", err
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return 0, "", err
	}
	if err := os.Rename(tmpPath, targetPath); err != nil {
		_ = os.Remove(tmpPath)
		return 0, "", err
	}

	return size, hex.EncodeToString(hasher.Sum(nil)), nil
}

func (h *Handler) releasePayload(r *http.Request, release store.AppRelease) map[string]any {
	return map[string]any{
		"id":                      release.ID,
		"platform":                release.Platform,
		"channel":                 release.Channel,
		"arch":                    release.Arch,
		"version":                 release.Version,
		"versionCode":             release.VersionCode,
		"minSupportedVersionCode": release.MinSupportedVersionCode,
		"updateLevel":             release.UpdateLevel,
		"title":                   release.Title,
		"filename":                release.Filename,
		"storagePath":             release.StoragePath,
		"storageKey":              release.StorageKey,
		"fileSize":                release.FileSize,
		"sha256":                  release.SHA256,
		"mimeType":                release.MimeType,
		"changelog":               release.Changelog,
		"status":                  release.Status,
		"isLatest":                release.IsLatest,
		"createdByAdminId":        release.CreatedByAdminID,
		"createdByUsername":       release.CreatedByUsername,
		"createdByRole":           release.CreatedByRole,
		"publishedAt":             release.PublishedAt,
		"createdAt":               release.CreatedAt,
		"updatedAt":               release.UpdatedAt,
		"downloadUrl":             h.releaseDownloadURL(r, release.StoragePath),
		"publicDownloadUrl":       h.releaseDownloadURL(r, release.StoragePath),
	}
}

func (h *Handler) releaseDownloadURL(r *http.Request, storagePath string) string {
	return h.resolvePublicURL(r, "/releases/"+strings.TrimLeft(storagePath, "/"))
}

func defaultReleaseBroadcastMessage(release store.AppRelease, downloadURL string) string {
	lines := []string{
		fmt.Sprintf("HSgram %s update released", strings.ToUpper(release.Platform)),
		fmt.Sprintf("Version: %s (%d)", release.Version, release.VersionCode),
		fmt.Sprintf("Channel: %s / Arch: %s / Level: %s", release.Channel, release.Arch, release.UpdateLevel),
	}
	if changelog := strings.TrimSpace(release.Changelog); changelog != "" {
		lines = append(lines, "", "Changelog:", changelog)
	}
	lines = append(lines, "", "Download:", downloadURL)
	return strings.Join(lines, "\n")
}

func sanitizeReleaseToken(input string) string {
	input = strings.TrimSpace(input)
	if input == "" {
		return ""
	}
	var builder strings.Builder
	for _, r := range input {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			builder.WriteRune(r)
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
		case r == '.', r == '-', r == '_':
			builder.WriteRune(r)
		}
	}
	return builder.String()
}

func sanitizeFilename(input string) string {
	input = filepath.Base(strings.TrimSpace(input))
	if input == "." || input == "" {
		return ""
	}
	var builder strings.Builder
	for _, r := range input {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			builder.WriteRune(r)
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
		case r == '.', r == '-', r == '_':
			builder.WriteRune(r)
		}
	}
	return builder.String()
}
