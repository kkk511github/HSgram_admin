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
		writeError(w, http.StatusMethodNotAllowed, "请求方法不允许")
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
		writeError(w, http.StatusBadRequest, "安装包上传表单无效")
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
		writeError(w, http.StatusBadRequest, "versionCode 必须是正整数")
		return
	}
	minSupportedVersionCode := 0
	if raw := strings.TrimSpace(r.FormValue("minSupportedVersionCode")); raw != "" {
		minSupportedVersionCode, err = strconv.Atoi(raw)
		if err != nil || minSupportedVersionCode < 0 {
			writeError(w, http.StatusBadRequest, "minSupportedVersionCode 必须为 0 或正整数")
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
		writeError(w, http.StatusBadRequest, "请上传安装包文件")
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
		writeError(w, http.StatusInternalServerError, "保存安装包文件失败")
		return
	}
	if platform == store.ReleasePlatformAndroid {
		apkPath := filepath.Join(h.updates.ReleasesDir, filepath.FromSlash(storagePath))
		metadata, err := readAPKManifestMetadata(apkPath)
		if err != nil {
			_ = os.Remove(apkPath)
			writeError(w, http.StatusBadRequest, "读取 APK 版本信息失败，请确认文件是有效 APK")
			return
		}
		if metadata.VersionCode != versionCode {
			_ = os.Remove(apkPath)
			writeError(w, http.StatusBadRequest, fmt.Sprintf("versionCode 与 APK 内部版本不一致：表单填写 %d，APK 实际为 %d", versionCode, metadata.VersionCode))
			return
		}
		if metadata.VersionName != "" && version != metadata.VersionName {
			_ = os.Remove(apkPath)
			writeError(w, http.StatusBadRequest, fmt.Sprintf("版本号与 APK 内部版本不一致：表单填写 %s，APK 实际为 %s", version, metadata.VersionName))
			return
		}
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
			writeError(w, http.StatusMethodNotAllowed, "请求方法不允许")
			return
		}
		h.handleReleaseList(w, r, admin)
		return
	}

	parts := strings.Split(path, "/")
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "发布记录 ID 无效")
		return
	}

	if len(parts) == 1 {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "请求方法不允许")
			return
		}
		h.handleReleaseDetail(w, r, admin, id)
		return
	}

	switch parts[1] {
	case "publish":
		h.handleReleasePublish(w, r, admin, id)
	default:
		writeError(w, http.StatusNotFound, "发布接口不存在")
	}
}

func (h *Handler) handleReleaseList(w http.ResponseWriter, r *http.Request, admin store.AdminUser) {
	items, err := h.store.ListAppReleases(r.Context(), 100)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "加载发布记录失败")
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
			writeError(w, http.StatusNotFound, "发布记录不存在")
			return
		}
		writeError(w, http.StatusInternalServerError, "加载发布详情失败")
		return
	}

	writeJSON(w, http.StatusOK, apiResponse{
		OK:   true,
		Data: h.releasePayload(r, *release),
	})
}

func (h *Handler) handleReleasePublish(w http.ResponseWriter, r *http.Request, admin store.AdminUser, releaseID int64) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "请求方法不允许")
		return
	}

	var req struct {
		NotifyUsers bool   `json:"notifyUsers"`
		MessageText string `json:"messageText"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "发布请求内容无效")
		return
	}
	if req.NotifyUsers && h.broadcasts == nil {
		writeError(w, http.StatusServiceUnavailable, "系统广播功能未启用")
		return
	}

	release, err := h.store.PublishAppRelease(r.Context(), releaseID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "发布记录不存在")
			return
		}
		writeError(w, http.StatusInternalServerError, "发布安装包失败")
		return
	}
	if err := h.writeLatestUpdateManifest(r, *release); err != nil {
		log.Printf("admin-api: write latest release manifest failed: %v", err)
		writeError(w, http.StatusInternalServerError, "发布更新清单失败")
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
		return "", "", "", fmt.Errorf("请填写版本号")
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
			return "", "", "", fmt.Errorf("Android 安装包必须是 .apk 文件")
		}
		mimeType = "application/vnd.android.package-archive"
	case store.ReleasePlatformWindows:
		if ext != ".exe" {
			return "", "", "", fmt.Errorf("Windows 安装包必须是 .exe 文件")
		}
		mimeType = "application/vnd.microsoft.portable-executable"
	}

	baseName := fmt.Sprintf("HSgram-%s-%s-%s-%s%s", platform, channel, arch, version, ext)
	baseName = sanitizeFilename(baseName)
	if baseName == "" {
		return "", "", "", fmt.Errorf("安装包文件名无效")
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
		fmt.Sprintf("HSgram %s 更新已发布", releasePlatformText(release.Platform)),
		fmt.Sprintf("版本: %s (%d)", release.Version, release.VersionCode),
		fmt.Sprintf("渠道: %s / 架构: %s / 更新级别: %s", releaseChannelText(release.Channel), releaseArchText(release.Arch), releaseUpdateLevelText(release.UpdateLevel)),
	}
	if changelog := strings.TrimSpace(release.Changelog); changelog != "" {
		lines = append(lines, "", "更新说明:", changelog)
	}
	lines = append(lines, "", "下载链接:", downloadURL)
	return strings.Join(lines, "\n")
}

func releasePlatformText(platform string) string {
	switch platform {
	case store.ReleasePlatformAndroid:
		return "Android"
	case store.ReleasePlatformWindows:
		return "Windows"
	default:
		return strings.ToUpper(platform)
	}
}

func releaseChannelText(channel string) string {
	switch channel {
	case store.ReleaseChannelStable:
		return "稳定版"
	case store.ReleaseChannelBeta:
		return "测试版"
	case store.ReleaseChannelInternal:
		return "内部版"
	default:
		return channel
	}
}

func releaseArchText(arch string) string {
	switch arch {
	case store.ReleaseArchUniversal:
		return "通用"
	case store.ReleaseArchWindowsX64:
		return "Windows x64"
	case store.ReleaseArchWindowsArm64:
		return "Windows ARM64"
	default:
		return arch
	}
}

func releaseUpdateLevelText(level string) string {
	switch level {
	case store.ReleaseUpdateRequired:
		return "强制更新"
	case store.ReleaseUpdateRecommended:
		return "推荐更新"
	case store.ReleaseUpdateOptional:
		return "普通可选更新"
	default:
		return level
	}
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
