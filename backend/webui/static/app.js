const PANEL_META = {
  users: { title: "用户管理", subtitle: "搜索与处理用户账号、会话与审计" },
  risk: { title: "风控策略", subtitle: "配置踢下线后的登录限制，以及同 IP 注册上限" },
  invites: { title: "邀请码", subtitle: "生成邀请码并查看实时使用记录" },
  releases: { title: "安装包发布", subtitle: "上传 Android / PC 安装包并发布为当前最新版本" },
  broadcast: { title: "系统广播", subtitle: "通过系统通知号向用户发送广播消息" },
};

const state = {
  token: localStorage.getItem("hsgram_admin_token") || "",
  admin: null,
  features: { broadcasts: false },
  activePanel: "users",
  page: 1,
  pageSize: 20,
  query: "",
  restricted: "",
  total: 0,
  users: [],
  selectedUserId: null,
  broadcastPreview: null,
  broadcasts: [],
  selectedBroadcastId: null,
  releases: [],
  selectedReleaseId: null,
  uploadProgress: 0,
  uploadState: "idle",
  riskSettings: { kickLoginBlockSeconds: 300, signupIpDailyLimit: 5 },
  signupIpStats: [],
  inviteCodes: [],
  selectedInviteCode: null,
  selectedInviteDetail: null,
};

const elements = {
  loginView: document.getElementById("loginView"),
  dashboardView: document.getElementById("dashboardView"),
  sidebarNav: document.getElementById("sidebarNav"),
  contentTitle: document.getElementById("contentTitle"),
  contentSubtitle: document.getElementById("contentSubtitle"),
  panelUsers: document.getElementById("panelUsers"),
  panelRisk: document.getElementById("panelRisk"),
  panelInvites: document.getElementById("panelInvites"),
  panelReleases: document.getElementById("panelReleases"),
  panelBroadcast: document.getElementById("panelBroadcast"),
  sessionPanel: document.getElementById("sessionPanel"),
  adminName: document.getElementById("adminName"),
  loginForm: document.getElementById("loginForm"),
  logoutButton: document.getElementById("logoutButton"),
  searchInput: document.getElementById("searchInput"),
  restrictedFilter: document.getElementById("restrictedFilter"),
  searchButton: document.getElementById("searchButton"),
  statusText: document.getElementById("statusText"),
  usersTableBody: document.getElementById("usersTableBody"),
  prevPageButton: document.getElementById("prevPageButton"),
  nextPageButton: document.getElementById("nextPageButton"),
  pageText: document.getElementById("pageText"),
  detailEmpty: document.getElementById("detailEmpty"),
  detailPanel: document.getElementById("detailPanel"),
  detailSummary: document.getElementById("detailSummary"),
  banReasonInput: document.getElementById("banReasonInput"),
  banButton: document.getElementById("banButton"),
  unbanButton: document.getElementById("unbanButton"),
  kickSessionsButton: document.getElementById("kickSessionsButton"),
  sessionsList: document.getElementById("sessionsList"),
  riskKickMinutesInput: document.getElementById("riskKickMinutesInput"),
  riskSignupLimitInput: document.getElementById("riskSignupLimitInput"),
  riskSaveButton: document.getElementById("riskSaveButton"),
  riskRefreshButton: document.getElementById("riskRefreshButton"),
  riskStatusText: document.getElementById("riskStatusText"),
  riskStatsList: document.getElementById("riskStatsList"),
  inviteCodeInput: document.getElementById("inviteCodeInput"),
  inviteMaxUsesInput: document.getElementById("inviteMaxUsesInput"),
  inviteNoteInput: document.getElementById("inviteNoteInput"),
  inviteCreateButton: document.getElementById("inviteCreateButton"),
  inviteRefreshButton: document.getElementById("inviteRefreshButton"),
  inviteStatusText: document.getElementById("inviteStatusText"),
  inviteCodesList: document.getElementById("inviteCodesList"),
  inviteDetailList: document.getElementById("inviteDetailList"),
  inviteEnableButton: document.getElementById("inviteEnableButton"),
  inviteDisableButton: document.getElementById("inviteDisableButton"),
  auditLogsList: document.getElementById("auditLogsList"),
  broadcastDisabledNotice: document.getElementById("broadcastDisabledNotice"),
  broadcastTargetType: document.getElementById("broadcastTargetType"),
  broadcastIdentifiersWrap: document.getElementById("broadcastIdentifiersWrap"),
  broadcastIdentifiers: document.getElementById("broadcastIdentifiers"),
  broadcastFiltersWrap: document.getElementById("broadcastFiltersWrap"),
  broadcastFilterRestricted: document.getElementById("broadcastFilterRestricted"),
  broadcastFilterDeleted: document.getElementById("broadcastFilterDeleted"),
  broadcastFilterIsBot: document.getElementById("broadcastFilterIsBot"),
  broadcastFilterCountryCode: document.getElementById("broadcastFilterCountryCode"),
  broadcastMessage: document.getElementById("broadcastMessage"),
  broadcastPreviewButton: document.getElementById("broadcastPreviewButton"),
  broadcastSendButton: document.getElementById("broadcastSendButton"),
  broadcastPreviewResult: document.getElementById("broadcastPreviewResult"),
  broadcastPreviewUsers: document.getElementById("broadcastPreviewUsers"),
  broadcastHistoryList: document.getElementById("broadcastHistoryList"),
  broadcastDetailList: document.getElementById("broadcastDetailList"),
  releasePlatform: document.getElementById("releasePlatform"),
  releaseVersion: document.getElementById("releaseVersion"),
  releaseVersionCode: document.getElementById("releaseVersionCode"),
  releaseChangelog: document.getElementById("releaseChangelog"),
  releaseFile: document.getElementById("releaseFile"),
  releaseUploadButton: document.getElementById("releaseUploadButton"),
  releaseUploadStatus: document.getElementById("releaseUploadStatus"),
  releaseUploadProgressBar: document.getElementById("releaseUploadProgressBar"),
  releaseHistoryList: document.getElementById("releaseHistoryList"),
  releaseDetailList: document.getElementById("releaseDetailList"),
  releaseNotifyUsers: document.getElementById("releaseNotifyUsers"),
  releasePublishMessage: document.getElementById("releasePublishMessage"),
  releaseBroadcastNotice: document.getElementById("releaseBroadcastNotice"),
  releasePublishButton: document.getElementById("releasePublishButton"),
  toast: document.getElementById("toast"),
};

elements.loginForm.addEventListener("submit", onLogin);
elements.logoutButton.addEventListener("click", logout);

document.querySelectorAll(".nav-item").forEach((btn) => {
  btn.addEventListener("click", () => {
    const panel = btn.getAttribute("data-panel");
    if (panel) {
      setActivePanel(panel);
    }
  });
});
elements.searchButton.addEventListener("click", runSearch);
elements.prevPageButton.addEventListener("click", () => changePage(-1));
elements.nextPageButton.addEventListener("click", () => changePage(1));
elements.banButton.addEventListener("click", () => mutateSelectedUser("ban"));
elements.unbanButton.addEventListener("click", () => mutateSelectedUser("unban"));
elements.kickSessionsButton.addEventListener("click", () => mutateSelectedUser("kick-sessions"));
elements.riskSaveButton.addEventListener("click", saveRiskSettings);
elements.riskRefreshButton.addEventListener("click", refreshRiskPanel);
elements.inviteCreateButton.addEventListener("click", createInviteCode);
elements.inviteRefreshButton.addEventListener("click", () => loadInviteCodes().catch((error) => toast(error.message || "加载邀请码失败")));
elements.inviteEnableButton.addEventListener("click", () => updateInviteCodeEnabled(true));
elements.inviteDisableButton.addEventListener("click", () => updateInviteCodeEnabled(false));
elements.broadcastTargetType.addEventListener("change", syncBroadcastTargetFields);
elements.broadcastPreviewButton.addEventListener("click", previewBroadcast);
elements.broadcastSendButton.addEventListener("click", sendBroadcast);
elements.releaseUploadButton.addEventListener("click", uploadRelease);
elements.releasePublishButton.addEventListener("click", publishSelectedRelease);
elements.releaseNotifyUsers.addEventListener("change", syncReleaseBroadcastFields);
elements.searchInput.addEventListener("keydown", (event) => {
  if (event.key === "Enter") {
    runSearch();
  }
});

bootstrap();

async function bootstrap() {
  if (!state.token) {
    renderLoggedOut();
    return;
  }

  try {
    const response = await api("/api/admin/me");
    state.admin = response.data.admin;
    state.features = response.data.features || { broadcasts: false };
    renderLoggedIn();
    await loadUsers();
    await refreshRiskPanel();
    await loadInviteCodes();
    await loadReleases();
    if (state.features.broadcasts) {
      await loadBroadcasts();
    } else {
      renderBroadcasts();
      renderBroadcastDetail(null);
    }
  } catch (error) {
    logout();
  }
}

async function onLogin(event) {
  event.preventDefault();

  const username = document.getElementById("usernameInput").value.trim();
  const password = document.getElementById("passwordInput").value;

  try {
    const response = await api("/api/admin/login", {
      method: "POST",
      body: JSON.stringify({ username, password }),
      auth: false,
    });

    state.token = response.data.token;
    state.admin = response.data.admin;
    state.features = response.data.features || { broadcasts: false };
    localStorage.setItem("hsgram_admin_token", state.token);

    renderLoggedIn();
    await loadUsers();
    await refreshRiskPanel();
    await loadInviteCodes();
    await loadReleases();
    if (state.features.broadcasts) {
      await loadBroadcasts();
    } else {
      renderBroadcasts();
      renderBroadcastDetail(null);
    }
    toast("登录成功");
    elements.loginForm.reset();
  } catch (error) {
    toast(error.message || "登录失败");
  }
}

function logout() {
  state.token = "";
  state.admin = null;
  state.features = { broadcasts: false };
  state.activePanel = "users";
  state.users = [];
  state.selectedUserId = null;
  state.releases = [];
  state.selectedReleaseId = null;
  state.uploadProgress = 0;
  state.uploadState = "idle";
  state.riskSettings = { kickLoginBlockSeconds: 300, signupIpDailyLimit: 5 };
  state.signupIpStats = [];
  state.inviteCodes = [];
  state.selectedInviteCode = null;
  state.selectedInviteDetail = null;
  localStorage.removeItem("hsgram_admin_token");
  renderLoggedOut();
}

function renderLoggedOut() {
  elements.loginView.classList.remove("hidden");
  elements.dashboardView.classList.add("hidden");
  elements.sessionPanel.classList.add("hidden");
  elements.sidebarNav.classList.add("hidden");
}

function setActivePanel(panel) {
  const allowed = ["users", "risk", "invites", "releases", "broadcast"];
  const id = allowed.includes(panel) ? panel : "users";
  state.activePanel = id;

  document.querySelectorAll(".nav-item").forEach((btn) => {
    btn.classList.toggle("active", btn.getAttribute("data-panel") === id);
  });

  elements.panelUsers.classList.toggle("hidden", id !== "users");
  elements.panelRisk.classList.toggle("hidden", id !== "risk");
  elements.panelInvites.classList.toggle("hidden", id !== "invites");
  elements.panelReleases.classList.toggle("hidden", id !== "releases");
  elements.panelBroadcast.classList.toggle("hidden", id !== "broadcast");

  const meta = PANEL_META[id] || PANEL_META.users;
  elements.contentTitle.textContent = meta.title;
  elements.contentSubtitle.textContent = meta.subtitle;
}

function renderLoggedIn() {
  elements.loginView.classList.add("hidden");
  elements.dashboardView.classList.remove("hidden");
  elements.sessionPanel.classList.remove("hidden");
  elements.sidebarNav.classList.remove("hidden");
  elements.adminName.textContent = `${state.admin.username} (${state.admin.role})`;
  if (state.features.broadcasts) {
    elements.broadcastDisabledNotice.classList.add("hidden");
  } else {
    elements.broadcastDisabledNotice.classList.remove("hidden");
  }
  elements.broadcastTargetType.disabled = !state.features.broadcasts;
  elements.broadcastIdentifiers.disabled = !state.features.broadcasts;
  elements.broadcastFilterRestricted.disabled = !state.features.broadcasts;
  elements.broadcastFilterDeleted.disabled = !state.features.broadcasts;
  elements.broadcastFilterIsBot.disabled = !state.features.broadcasts;
  elements.broadcastFilterCountryCode.disabled = !state.features.broadcasts;
  elements.broadcastMessage.disabled = !state.features.broadcasts;
  elements.broadcastPreviewButton.disabled = !state.features.broadcasts;
  elements.broadcastSendButton.disabled = !state.features.broadcasts;
  elements.releaseNotifyUsers.disabled = !state.features.broadcasts;
  if (!state.features.broadcasts) {
    elements.releaseNotifyUsers.checked = false;
  }
  elements.releaseBroadcastNotice.textContent = state.features.broadcasts
    ? "勾选后会在发布成功后向全体用户创建一条系统广播通知。"
    : "当前部署未开启广播能力，仍可上传并发布安装包。";
  syncBroadcastTargetFields();
  syncReleaseBroadcastFields();
  renderUploadProgress();
  setActivePanel(state.activePanel || "users");
}

async function loadUsers() {
  const params = new URLSearchParams({
    page: String(state.page),
    pageSize: String(state.pageSize),
  });
  if (state.query) {
    params.set("q", state.query);
  }
  if (state.restricted !== "") {
    params.set("restricted", state.restricted);
  }

  const response = await api(`/api/admin/users?${params.toString()}`);
  state.users = response.data.items;
  state.total = response.data.total;

  renderUsers();
  elements.statusText.textContent = `共 ${state.total} 个用户`;
  elements.pageText.textContent = `第 ${state.page} 页`;
}

async function loadReleases() {
  try {
    const response = await api("/api/admin/releases");
    state.releases = response.data || [];
    renderReleases();
    if (state.selectedReleaseId) {
      const exists = state.releases.some((release) => release.id === state.selectedReleaseId);
      if (exists) {
        await loadReleaseDetail(state.selectedReleaseId);
      } else {
        state.selectedReleaseId = null;
        renderReleaseDetail(null);
      }
    } else {
      renderReleaseDetail(null);
    }
  } catch (error) {
    elements.releaseHistoryList.innerHTML = `<div class="muted">发布历史加载失败</div>`;
  }
}

function renderReleases() {
  elements.releaseHistoryList.innerHTML = "";
  if (!state.releases.length) {
    elements.releaseHistoryList.innerHTML = `<div class="muted">暂无安装包发布记录</div>`;
    return;
  }

  state.releases.forEach((release) => {
    const item = document.createElement("div");
    item.className = "list-item";
    if (release.id === state.selectedReleaseId) {
      item.style.borderColor = "#2f6fed";
    }
    item.innerHTML = `
      <div class="list-item-title">#${release.id} ${escapeHTML(release.platform)} ${escapeHTML(release.version)}</div>
      <div>版本编码: ${release.versionCode} | 状态: ${escapeHTML(release.status)}${release.isLatest ? " | 当前最新" : ""}</div>
      <div>文件: ${escapeHTML(release.filename)} | 大小: ${formatFileSize(release.fileSize)}</div>
    `;
    item.addEventListener("click", () => loadReleaseDetail(release.id));
    elements.releaseHistoryList.appendChild(item);
  });
}

async function loadReleaseDetail(id) {
  try {
    const response = await api(`/api/admin/releases/${id}`);
    state.selectedReleaseId = id;
    renderReleases();
    renderReleaseDetail(response.data);
  } catch (error) {
    toast(error.message || "加载发布详情失败");
  }
}

function renderReleaseDetail(release) {
  elements.releaseDetailList.innerHTML = "";
  if (!release) {
    elements.releasePublishButton.disabled = true;
    elements.releaseDetailList.innerHTML = `<div class="muted">选择一条发布记录查看详情</div>`;
    return;
  }
  elements.releasePublishButton.disabled = false;

  const item = document.createElement("div");
  item.className = "list-item";
  item.innerHTML = `
    <div class="list-item-title">${escapeHTML(release.platform)} ${escapeHTML(release.version)}${release.isLatest ? "（当前最新）" : ""}</div>
    <div>版本编码: ${release.versionCode}</div>
    <div>状态: ${escapeHTML(release.status)}</div>
    <div>文件: ${escapeHTML(release.filename)}</div>
    <div>大小: ${formatFileSize(release.fileSize)}</div>
    <div>SHA256: ${escapeHTML(release.sha256 || "-")}</div>
    <div>上传者: ${escapeHTML(release.createdByUsername || "-")} (${escapeHTML(release.createdByRole || "-")})</div>
    <div>上传时间: ${formatDateTime(release.createdAt)}</div>
    <div>发布时间: ${formatDateTime(release.publishedAt)}</div>
    <div>下载地址: <a class="link-text" href="${escapeHTML(release.downloadUrl)}" target="_blank" rel="noopener noreferrer">打开下载链接</a></div>
    <div>更新说明: ${escapeHTML(release.changelog || "-")}</div>
  `;
  elements.releaseDetailList.appendChild(item);
}

function syncReleaseBroadcastFields() {
  const enabled = state.features.broadcasts && elements.releaseNotifyUsers.checked;
  elements.releasePublishMessage.disabled = !enabled;
}

function renderUploadProgress() {
  const percent = Math.max(0, Math.min(100, state.uploadProgress || 0));
  elements.releaseUploadProgressBar.style.width = `${percent}%`;
  switch (state.uploadState) {
    case "uploading":
      elements.releaseUploadStatus.textContent = `上传中 ${percent}%`;
      break;
    case "done":
      elements.releaseUploadStatus.textContent = "上传完成";
      break;
    case "error":
      if (!elements.releaseUploadStatus.textContent) {
        elements.releaseUploadStatus.textContent = "上传失败";
      }
      break;
    default:
      elements.releaseUploadStatus.textContent = "上传后会写入后台制品目录，并记录到发布历史。";
      elements.releaseUploadProgressBar.style.width = "0%";
      break;
  }
  const busy = state.uploadState === "uploading";
  elements.releaseUploadButton.disabled = busy;
  elements.releasePublishButton.disabled = busy || !state.selectedReleaseId;
  elements.releasePlatform.disabled = busy;
  elements.releaseVersion.disabled = busy;
  elements.releaseVersionCode.disabled = busy;
  elements.releaseChangelog.disabled = busy;
  elements.releaseFile.disabled = busy;
}

async function uploadRelease() {
  const file = elements.releaseFile.files && elements.releaseFile.files[0];
  if (!file) {
    toast("请选择安装包文件");
    return;
  }
  if (!elements.releaseVersion.value.trim()) {
    toast("请填写版本号");
    return;
  }
  if (!elements.releaseVersionCode.value.trim()) {
    toast("请填写版本编码");
    return;
  }

  const formData = new FormData();
  formData.append("platform", elements.releasePlatform.value);
  formData.append("version", elements.releaseVersion.value.trim());
  formData.append("versionCode", elements.releaseVersionCode.value.trim());
  formData.append("changelog", elements.releaseChangelog.value.trim());
  formData.append("file", file);

  state.uploadState = "uploading";
  state.uploadProgress = 0;
  renderUploadProgress();

  try {
    const payload = await uploadWithProgress("/api/admin/releases/upload", formData, (percent) => {
      state.uploadProgress = percent;
      renderUploadProgress();
    });
    state.uploadState = "done";
    state.uploadProgress = 100;
    renderUploadProgress();
    elements.releaseFile.value = "";
    await loadReleases();
    if (payload.data && payload.data.id) {
      await loadReleaseDetail(payload.data.id);
    }
    toast("安装包上传成功");
  } catch (error) {
    state.uploadState = "error";
    elements.releaseUploadStatus.textContent = error.message || "上传失败";
    renderUploadProgress();
    toast(error.message || "上传失败");
  }
}

async function publishSelectedRelease() {
  if (!state.selectedReleaseId) {
    toast("请先选择一条发布记录");
    return;
  }

  try {
    const response = await api(`/api/admin/releases/${state.selectedReleaseId}/publish`, {
      method: "POST",
      body: JSON.stringify({
        notifyUsers: !!elements.releaseNotifyUsers.checked,
        messageText: elements.releasePublishMessage.value.trim(),
      }),
    });
    await loadReleases();
    await loadReleaseDetail(state.selectedReleaseId);
    if (response.data && response.data.broadcastError) {
      toast(`安装包已发布，但广播失败：${response.data.broadcastError}`);
      return;
    }
    toast(elements.releaseNotifyUsers.checked ? "安装包已发布，并创建广播通知" : "安装包已发布为最新版本");
  } catch (error) {
    toast(error.message || "发布失败");
  }
}

function uploadWithProgress(url, formData, onProgress) {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.open("POST", url);
    if (state.token) {
      xhr.setRequestHeader("Authorization", `Bearer ${state.token}`);
    }
    xhr.upload.onprogress = (event) => {
      if (!event.lengthComputable || !onProgress) {
        return;
      }
      onProgress(Math.round((event.loaded / event.total) * 100));
    };
    xhr.onload = () => {
      const payload = safeParseJSON(xhr.responseText);
      if (xhr.status >= 200 && xhr.status < 300 && payload && payload.ok) {
        resolve(payload);
        return;
      }
      reject(new Error((payload && payload.error) || "upload failed"));
    };
    xhr.onerror = () => reject(new Error("upload failed"));
    xhr.send(formData);
  });
}

function renderUsers() {
  elements.usersTableBody.innerHTML = "";

  if (!state.users.length) {
    const row = document.createElement("tr");
    row.innerHTML = `<td colspan="5" class="muted">没有查询到用户</td>`;
    elements.usersTableBody.appendChild(row);
    return;
  }

  state.users.forEach((user) => {
    const row = document.createElement("tr");
    if (user.id === state.selectedUserId) {
      row.classList.add("active");
    }
    row.innerHTML = `
      <td>${user.id}</td>
      <td>${escapeHTML(user.displayName)}</td>
      <td>${escapeHTML(user.username || "-")}</td>
      <td>${escapeHTML(user.phone || "-")}</td>
      <td>
        <span class="status-badge ${user.restricted ? "restricted" : ""}">
          ${user.restricted ? "已封禁" : "正常"}
        </span>
      </td>
    `;
    row.addEventListener("click", async () => {
      state.selectedUserId = user.id;
      renderUsers();
      await loadUserDetail(user.id);
    });
    elements.usersTableBody.appendChild(row);
  });
}

async function loadUserDetail(userId) {
  const response = await api(`/api/admin/users/${userId}`);
  const user = response.data;

  elements.detailEmpty.classList.add("hidden");
  elements.detailPanel.classList.remove("hidden");

  elements.detailSummary.innerHTML = `
    <div><strong>${escapeHTML(user.displayName)}</strong></div>
    <div>ID: ${user.id}</div>
    <div>用户名: ${escapeHTML(user.username || "-")}</div>
    <div>手机号: ${escapeHTML(user.phone || "-")}</div>
    <div>国家码: ${escapeHTML(user.countryCode || "-")}</div>
    <div>状态: ${user.restricted ? "已封禁" : "正常"}</div>
    <div>封禁原因: ${escapeHTML(user.restrictionReason || "-")}</div>
    <div>签名: ${escapeHTML(user.about || "-")}</div>
  `;

  elements.sessionsList.innerHTML = "";
  if (!user.sessions.length) {
    elements.sessionsList.innerHTML = `<div class="muted">暂无会话</div>`;
  } else {
    user.sessions.forEach((session) => {
      const item = document.createElement("div");
      item.className = "list-item";
      item.innerHTML = `
        <div class="list-item-title">${escapeHTML(session.deviceModel || "Unknown device")}</div>
        <div>IP: ${escapeHTML(session.ip || "-")} | ${escapeHTML(session.country || "-")} ${escapeHTML(session.region || "")}</div>
        <div>App: ${escapeHTML(session.appName || "-")} ${escapeHTML(session.appVersion || "")}</div>
        <div>最后活跃时间戳: ${session.dateActive || 0}</div>
      `;
      elements.sessionsList.appendChild(item);
    });
  }

  elements.auditLogsList.innerHTML = "";
  if (!user.auditLogs.length) {
    elements.auditLogsList.innerHTML = `<div class="muted">暂无操作日志</div>`;
  } else {
    user.auditLogs.forEach((entry) => {
      const item = document.createElement("div");
      item.className = "list-item";
      item.innerHTML = `
        <div class="list-item-title">${escapeHTML(entry.action)}</div>
        <div>操作者: ${escapeHTML(entry.actorUsername)} (${escapeHTML(entry.actorRole)})</div>
        <div>时间: ${new Date(entry.createdAt).toLocaleString()}</div>
      `;
      elements.auditLogsList.appendChild(item);
    });
  }
}

async function mutateSelectedUser(action) {
  if (!state.selectedUserId) {
    toast("请先选择用户");
    return;
  }

  const request = {
    method: "POST",
  };
  if (action === "ban") {
    request.body = JSON.stringify({ reason: elements.banReasonInput.value.trim() });
  }

  try {
    await api(`/api/admin/users/${state.selectedUserId}/${action}`, request);
    toast(action === "ban" ? "封禁成功" : action === "unban" ? "解封成功" : "已踢下线");
    await loadUsers();
    await loadUserDetail(state.selectedUserId);
  } catch (error) {
    toast(error.message || "操作失败");
  }
}

function runSearch() {
  state.page = 1;
  state.query = elements.searchInput.value.trim();
  state.restricted = elements.restrictedFilter.value;
  loadUsers().catch((error) => toast(error.message || "加载用户失败"));
}

function syncBroadcastTargetFields() {
  if (!state.features.broadcasts) {
    elements.broadcastIdentifiersWrap.classList.add("hidden");
    elements.broadcastFiltersWrap.classList.add("hidden");
    return;
  }
  const targetType = elements.broadcastTargetType.value;
  elements.broadcastIdentifiersWrap.classList.toggle("hidden", targetType !== "selected");
  elements.broadcastFiltersWrap.classList.toggle("hidden", targetType !== "filtered");
}

function buildBroadcastRequest() {
  return {
    targetType: elements.broadcastTargetType.value,
    identifiers: elements.broadcastIdentifiers.value.trim(),
    messageText: elements.broadcastMessage.value.trim(),
    filters: {
      restricted: toOptionalBool(elements.broadcastFilterRestricted.value),
      deleted: toOptionalBool(elements.broadcastFilterDeleted.value),
      isBot: toOptionalBool(elements.broadcastFilterIsBot.value),
      countryCode: elements.broadcastFilterCountryCode.value.trim(),
    },
  };
}

async function previewBroadcast() {
  if (!state.features.broadcasts) {
    toast("当前部署未开启广播能力");
    return;
  }
  try {
    const response = await api("/api/admin/broadcasts/preview", {
      method: "POST",
      body: JSON.stringify(buildBroadcastRequest()),
    });
    state.broadcastPreview = response.data;
    renderBroadcastPreview();
    toast(`预估 ${response.data.count} 个接收用户`);
  } catch (error) {
    toast(error.message || "预估失败");
  }
}

function renderBroadcastPreview() {
  const preview = state.broadcastPreview;
  if (!preview) {
    elements.broadcastPreviewResult.textContent = "";
    elements.broadcastPreviewUsers.innerHTML = "";
    return;
  }

  const unresolved = preview.unresolved && preview.unresolved.length
    ? `，未解析: ${preview.unresolved.join(", ")}`
    : "";
  elements.broadcastPreviewResult.textContent = `预计发送给 ${preview.count} 个用户${unresolved}`;
  elements.broadcastPreviewUsers.innerHTML = "";

  if (!preview.sampleUsers || !preview.sampleUsers.length) {
    elements.broadcastPreviewUsers.innerHTML = `<div class="muted">暂无预览用户</div>`;
    return;
  }

  preview.sampleUsers.forEach((user) => {
    const item = document.createElement("div");
    item.className = "list-item";
    item.innerHTML = `
      <div class="list-item-title">${escapeHTML(user.displayName || "-")}</div>
      <div>ID: ${user.id} | 用户名: ${escapeHTML(user.username || "-")} | 手机号: ${escapeHTML(user.phone || "-")}</div>
    `;
    elements.broadcastPreviewUsers.appendChild(item);
  });
}

async function sendBroadcast() {
  if (!state.features.broadcasts) {
    toast("当前部署未开启广播能力");
    return;
  }
  try {
    const response = await api("/api/admin/broadcasts", {
      method: "POST",
      body: JSON.stringify(buildBroadcastRequest()),
    });
    state.broadcastPreview = response.data.preview;
    renderBroadcastPreview();
    elements.broadcastMessage.value = "";
    elements.broadcastIdentifiers.value = "";
    await loadBroadcasts();
    if (response.data.broadcast && response.data.broadcast.id) {
      await loadBroadcastDetail(response.data.broadcast.id);
    }
    toast("广播任务已创建");
  } catch (error) {
    toast(error.message || "创建广播失败");
  }
}

async function loadBroadcasts() {
  if (!state.features.broadcasts) {
    state.broadcasts = [];
    renderBroadcasts();
    return;
  }
  try {
    const response = await api("/api/admin/broadcasts");
    state.broadcasts = response.data || [];
    renderBroadcasts();
  } catch (error) {
    elements.broadcastHistoryList.innerHTML = `<div class="muted">广播历史加载失败</div>`;
  }
}

function renderBroadcasts() {
  elements.broadcastHistoryList.innerHTML = "";
  if (!state.features.broadcasts) {
    elements.broadcastHistoryList.innerHTML = `<div class="muted">广播功能未开启</div>`;
    return;
  }
  if (!state.broadcasts.length) {
    elements.broadcastHistoryList.innerHTML = `<div class="muted">暂无广播历史</div>`;
    return;
  }

  state.broadcasts.forEach((broadcast) => {
    const item = document.createElement("div");
    item.className = "list-item";
    if (broadcast.id === state.selectedBroadcastId) {
      item.style.borderColor = "#2f6fed";
    }
    item.innerHTML = `
      <div class="list-item-title">#${broadcast.id} ${escapeHTML(broadcast.status)}</div>
      <div>范围: ${escapeHTML(broadcast.targetType)} | 成功: ${broadcast.successCount} | 失败: ${broadcast.failureCount}</div>
      <div>${escapeHTML((broadcast.messageText || "").slice(0, 80) || "-")}</div>
    `;
    item.addEventListener("click", () => loadBroadcastDetail(broadcast.id));
    elements.broadcastHistoryList.appendChild(item);
  });
}

async function loadBroadcastDetail(id) {
  try {
    const response = await api(`/api/admin/broadcasts/${id}`);
    state.selectedBroadcastId = id;
    renderBroadcasts();
    renderBroadcastDetail(response.data);
  } catch (error) {
    toast(error.message || "加载广播详情失败");
  }
}

function renderBroadcastDetail(broadcast) {
  elements.broadcastDetailList.innerHTML = "";
  if (!state.features.broadcasts) {
    elements.broadcastDetailList.innerHTML = `<div class="muted">广播功能未开启</div>`;
    return;
  }
  if (!broadcast) {
    elements.broadcastDetailList.innerHTML = `<div class="muted">暂无投递记录</div>`;
    return;
  }

  const summary = document.createElement("div");
  summary.className = "list-item";
  summary.innerHTML = `
    <div class="list-item-title">广播 #${broadcast.id}</div>
    <div>状态: ${escapeHTML(broadcast.status)} | 总数: ${broadcast.totalTargets} | 成功: ${broadcast.successCount} | 失败: ${broadcast.failureCount}</div>
    <div>内容: ${escapeHTML(broadcast.messageText || "-")}</div>
  `;
  elements.broadcastDetailList.appendChild(summary);

  if (!broadcast.deliveries || !broadcast.deliveries.length) {
    const empty = document.createElement("div");
    empty.className = "muted";
    empty.textContent = "暂无投递记录";
    elements.broadcastDetailList.appendChild(empty);
    return;
  }

  broadcast.deliveries.forEach((delivery) => {
    const item = document.createElement("div");
    item.className = "list-item";
    item.innerHTML = `
      <div class="list-item-title">${escapeHTML(delivery.targetDisplayName || "-")} (${delivery.targetUserId})</div>
      <div>状态: ${escapeHTML(delivery.status)} ${delivery.errorMessage ? `| 错误: ${escapeHTML(delivery.errorMessage)}` : ""}</div>
    `;
    elements.broadcastDetailList.appendChild(item);
  });
}


async function loadInviteCodes() {
  try {
    const response = await api("/api/admin/invite-codes");
    state.inviteCodes = response.data || [];
    if (state.selectedInviteCode) {
      const exists = state.inviteCodes.some((item) => item.code === state.selectedInviteCode);
      if (!exists) {
        state.selectedInviteCode = null;
        state.selectedInviteDetail = null;
      }
    }
    renderInviteCodes();
    renderInviteDetail();
    elements.inviteStatusText.textContent = "邀请码列表已刷新";
  } catch (error) {
    elements.inviteStatusText.textContent = error.message || "邀请码列表加载失败";
    throw error;
  }
}

function renderInviteCodes() {
  elements.inviteCodesList.innerHTML = "";
  if (!state.inviteCodes.length) {
    elements.inviteCodesList.innerHTML = `<div class="muted">暂时还没有邀请码</div>`;
    return;
  }

  state.inviteCodes.forEach((item) => {
    const row = document.createElement("div");
    row.className = "list-item invite-code-row";
    row.classList.toggle("selected", item.code === state.selectedInviteCode);
    row.innerHTML = `
      <div class="list-item-title">${escapeHTML(item.code)}</div>
      <div>状态: ${item.enabled ? "启用中" : "已禁用"} | 已用: ${item.usedCount}/${item.maxUses > 0 ? item.maxUses : "不限"}</div>
      <div>备注: ${escapeHTML(item.note || "-")}</div>
    `;
    row.addEventListener("click", () => loadInviteDetail(item.code));
    elements.inviteCodesList.appendChild(row);
  });
}

async function createInviteCode() {
  const maxUses = Number(elements.inviteMaxUsesInput.value || 0);
  if (!Number.isFinite(maxUses) || maxUses < 0) {
    toast("可用次数不能小于 0");
    return;
  }

  try {
    const response = await api("/api/admin/invite-codes", {
      method: "POST",
      body: JSON.stringify({
        code: elements.inviteCodeInput.value.trim(),
        note: elements.inviteNoteInput.value.trim(),
        maxUses: Math.round(maxUses),
      }),
    });

    elements.inviteCodeInput.value = "";
    elements.inviteNoteInput.value = "";
    elements.inviteMaxUsesInput.value = "1";
    state.selectedInviteCode = response.data.code;
    toast(`邀请码 ${response.data.code} 已创建`);
    await loadInviteCodes();
    await loadInviteDetail(response.data.code);
  } catch (error) {
    toast(error.message || "创建邀请码失败");
  }
}

async function loadInviteDetail(code) {
  try {
    const response = await api(`/api/admin/invite-codes/${encodeURIComponent(code)}`);
    state.selectedInviteCode = code;
    state.selectedInviteDetail = response.data;
    renderInviteCodes();
    renderInviteDetail();
  } catch (error) {
    toast(error.message || "加载邀请码详情失败");
  }
}

function renderInviteDetail() {
  elements.inviteDetailList.innerHTML = "";
  elements.inviteEnableButton.disabled = !state.selectedInviteCode;
  elements.inviteDisableButton.disabled = !state.selectedInviteCode;

  if (!state.selectedInviteDetail) {
    elements.inviteDetailList.innerHTML = `<div class="muted">从列表中选择一个邀请码查看详情。</div>`;
    return;
  }

  const { code, usages } = state.selectedInviteDetail;
  const summary = document.createElement("div");
  summary.className = "list-item";
  summary.innerHTML = `
    <div class="list-item-title">${escapeHTML(code.code)}</div>
    <div>状态: ${code.enabled ? "启用中" : "已禁用"} | 创建人: ${escapeHTML(code.createdBy || "-")}</div>
    <div>可用次数: ${code.maxUses > 0 ? code.maxUses : "不限"} | 已用次数: ${code.usedCount}</div>
    <div>备注: ${escapeHTML(code.note || "-")}</div>
    <div>创建时间: ${formatUnixDateTime(code.createdAt)} | 更新时间: ${formatUnixDateTime(code.updatedAt)}</div>
  `;
  elements.inviteDetailList.appendChild(summary);

  if (!usages || !usages.length) {
    const empty = document.createElement("div");
    empty.className = "muted";
    empty.textContent = "还没有使用记录";
    elements.inviteDetailList.appendChild(empty);
    return;
  }

  usages.forEach((usage) => {
    const item = document.createElement("div");
    item.className = "list-item";
    item.innerHTML = `
      <div class="list-item-title">${escapeHTML(usage.phone || "-")}</div>
      <div>状态: ${escapeHTML(usage.status || "-")} | 用户 ID: ${usage.userId || 0}</div>
      <div>时间: ${formatUnixDateTime(usage.usedAt)}</div>
    `;
    elements.inviteDetailList.appendChild(item);
  });
}

async function updateInviteCodeEnabled(enabled) {
  if (!state.selectedInviteCode) {
    toast("请先选择邀请码");
    return;
  }

  const action = enabled ? "enable" : "disable";
  try {
    await api(`/api/admin/invite-codes/${encodeURIComponent(state.selectedInviteCode)}/${action}`, {
      method: "POST",
    });
    toast(enabled ? "邀请码已启用" : "邀请码已禁用");
    await loadInviteCodes();
    await loadInviteDetail(state.selectedInviteCode);
  } catch (error) {
    toast(error.message || "更新邀请码状态失败");
  }
}

async function loadRiskSettings() {
  const response = await api("/api/admin/risk/settings");
  state.riskSettings = response.data || { kickLoginBlockSeconds: 300, signupIpDailyLimit: 5 };
  renderRiskPanel();
}

async function loadSignupIpStats() {
  const response = await api("/api/admin/risk/signup-ip-stats?limit=50");
  state.signupIpStats = (response.data && response.data.items) || [];
  renderRiskPanel();
}

async function refreshRiskPanel() {
  try {
    await Promise.all([loadRiskSettings(), loadSignupIpStats()]);
    elements.riskStatusText.textContent = "风控配置和统计已刷新";
  } catch (error) {
    elements.riskStatusText.textContent = error.message || "风控数据加载失败";
    throw error;
  }
}

function renderRiskPanel() {
  const minutes = Math.max(1, Math.round((state.riskSettings.kickLoginBlockSeconds || 300) / 60));
  elements.riskKickMinutesInput.value = String(minutes);
  elements.riskSignupLimitInput.value = String(state.riskSettings.signupIpDailyLimit || 5);
  elements.riskStatsList.innerHTML = "";

  if (!state.signupIpStats.length) {
    elements.riskStatsList.innerHTML = `<div class="muted">今天暂时没有注册 IP 记录</div>`;
    return;
  }

  state.signupIpStats.forEach((item) => {
    const row = document.createElement("div");
    row.className = "list-item";
    row.innerHTML = `
      <div class="list-item-title">${escapeHTML(item.ip)}</div>
      <div>今日注册数: ${item.count}</div>
    `;
    elements.riskStatsList.appendChild(row);
  });
}

async function saveRiskSettings() {
  const kickMinutes = Number(elements.riskKickMinutesInput.value || 0);
  const signupLimit = Number(elements.riskSignupLimitInput.value || 0);
  if (!Number.isFinite(kickMinutes) || kickMinutes < 1) {
    toast("踢下线限制分钟数不能小于 1");
    return;
  }
  if (!Number.isFinite(signupLimit) || signupLimit < 1) {
    toast("单 IP 注册上限不能小于 1");
    return;
  }

  try {
    const response = await api("/api/admin/risk/settings", {
      method: "POST",
      body: JSON.stringify({
        kickLoginBlockSeconds: Math.round(kickMinutes * 60),
        signupIpDailyLimit: Math.round(signupLimit),
      }),
    });
    state.riskSettings = response.data;
    renderRiskPanel();
    elements.riskStatusText.textContent = "风控配置已保存";
    toast("风控配置已保存");
  } catch (error) {
    toast(error.message || "保存风控配置失败");
  }
}

function changePage(offset) {
  const nextPage = state.page + offset;
  if (nextPage < 1) {
    return;
  }
  if ((nextPage - 1) * state.pageSize >= state.total && offset > 0) {
    return;
  }
  state.page = nextPage;
  loadUsers().catch((error) => toast(error.message || "翻页失败"));
}

async function api(url, options = {}) {
  const headers = {
    "Content-Type": "application/json",
    ...(options.headers || {}),
  };

  if (options.auth !== false && state.token) {
    headers.Authorization = `Bearer ${state.token}`;
  }

  const response = await fetch(url, {
    ...options,
    headers,
  });

  const payload = await response.json().catch(() => ({ ok: false, error: "invalid server response" }));
  if (!response.ok || !payload.ok) {
    throw new Error(payload.error || "request failed");
  }

  return payload;
}

function toast(message) {
  elements.toast.textContent = message;
  elements.toast.classList.remove("hidden");
  clearTimeout(toast.timer);
  toast.timer = setTimeout(() => {
    elements.toast.classList.add("hidden");
  }, 2500);
}

function escapeHTML(input) {
  return String(input)
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#39;");
}

function safeParseJSON(input) {
  try {
    return JSON.parse(input);
  } catch (error) {
    return null;
  }
}

function formatDateTime(value) {
  if (!value) {
    return "-";
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return "-";
  }
  return date.toLocaleString();
}

function formatUnixDateTime(value) {
  const seconds = Number(value || 0);
  if (!Number.isFinite(seconds) || seconds <= 0) {
    return "-";
  }
  return new Date(seconds * 1000).toLocaleString();
}

function formatFileSize(size) {
  const value = Number(size || 0);
  if (!Number.isFinite(value) || value <= 0) {
    return "-";
  }
  if (value < 1024) {
    return `${value} B`;
  }
  if (value < 1024 * 1024) {
    return `${(value / 1024).toFixed(1)} KB`;
  }
  if (value < 1024 * 1024 * 1024) {
    return `${(value / (1024 * 1024)).toFixed(1)} MB`;
  }
  return `${(value / (1024 * 1024 * 1024)).toFixed(2)} GB`;
}

function toOptionalBool(value) {
  if (value === "true") {
    return true;
  }
  if (value === "false") {
    return false;
  }
  return null;
}
