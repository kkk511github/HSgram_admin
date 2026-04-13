# HSgram Admin

一个独立的新目录，用来承载 HSgram 的后台管理界面，并支持部署成公网可访问的后台站点。

## 目录

- `backend/`: Go 管理 API 和内嵌的管理页面
- `deploy/`: 公网部署所需的 `Caddyfile` 和环境变量示例
- `sql/`: 后台账号和审计日志表的参考 SQL
- `docker-compose.public.yaml`: 对外提供 `80/443` 的部署编排

## 已实现能力

- 管理员登录
- 用户列表与搜索
- 用户详情查看
- 封禁 / 解封
- 踢下线
- 基础操作日志
- `healthz` 健康检查
- 登录失败限流
- Android / PC 公共 OTA manifest 与制品下载入口
- Docker 化部署
- Caddy 反向代理和 HTTPS 入口

## 本地开发运行

1. 参考 `backend/.env.example` 设置环境变量。
2. 进入 `backend/` 执行：

```bash
go run ./cmd/admin-api
```

3. 浏览器打开 `http://127.0.0.1:8088`

## 公网部署

如果你希望“任何电脑打开后台地址都能登录”，请把它部署到一台固定服务器，并给它一个域名。

### 当前端口说明

- 当前对外访问端口是 `80`
- 当前访问地址是 `http://43.134.228.34`
- `admin-api` 容器内部监听端口是 `8088`
- `caddy` 会把外部 `80` 端口转发到内部 `admin-api:8088`
- 当前没有给 admin 使用 `443`，因为该端口已被现有服务占用，所以现在是 `HTTP` 而不是 `HTTPS`

### 1. 域名准备

- 准备一个域名，例如 `admin.example.com`
- 把该域名的 DNS A 记录指向部署服务器公网 IP
- 确保服务器安全组 / 防火墙已放行 `80` 和 `443`

### 2. 配置环境变量

复制 `deploy/public.env.example` 为你自己的环境文件，例如 `deploy/public.env`，然后修改：

- `ADMIN_IMAGE=hsgram_admin-admin-api:latest`
- `ADMIN_SITE_ADDRESS=admin.example.com`
- `ADMIN_DATABASE_DSN=...`
- `ADMIN_JWT_SECRET=强随机密钥`
- `ADMIN_BOOTSTRAP_PASSWORD=强密码`
- `ADMIN_ENABLE_BROADCASTS=false`
- `ADMIN_RELEASES_DIR=/app/releases`
- `ADMIN_PUBLIC_BASE_URL=https://admin.example.com`

注意：

- 如果 MySQL 就运行在同一台服务器并且对宿主机开放了 `3306`，可以直接保留 `host.docker.internal:3306`
- 如果 MySQL 在另一台机器，请把 `host.docker.internal` 改成真实数据库地址
- 上面的 `host.docker.internal` 之所以可用，是因为 `docker-compose.public.yaml` 已通过 `extra_hosts` 把它映射到了宿主机网关；如果你不用这份 compose，需要自己补这层映射
- 如果 MySQL 只监听 `127.0.0.1`，容器通常仍然连不上；正式部署时请确认 MySQL 监听地址允许来自 Docker 容器所在网段，或直接把数据库部署到同一 Docker 网络里
- `ADMIN_ENABLE_BROADCASTS=false` 时，后台只启用用户管理能力，不依赖消息服务链路，最适合 `4核8G` 的轻量部署
- 只有要使用系统广播时，才把 `ADMIN_ENABLE_BROADCASTS=true`，并同时配置可用的 `ADMIN_MSG_RPC_ADDR`
- `ADMIN_RELEASES_DIR` 对应容器内的 OTA 制品目录；默认 compose 已把宿主机 `./data/releases` 挂载到 `/app/releases`
- `ADMIN_PUBLIC_BASE_URL` 用来把 manifest 里的相对路径补成完整下载地址，建议填最终公网地址

### 3. 先在构建机生成镜像

为了避免服务器在部署时现场 `go build` 导致 CPU/内存飙高，默认建议在本地机器或另一台更强的机器先构建镜像：

```bash
docker build -f HSgram_admin/backend/Dockerfile -t hsgram_admin-admin-api:latest .
```

如果服务器不直接拉仓库镜像，可以导出后传过去：

```bash
docker save hsgram_admin-admin-api:latest -o hsgram-admin-api.tar
```

在服务器上导入：

```bash
docker load -i hsgram-admin-api.tar
```

### 4. 启动服务

在 `HSgram_admin` 目录执行：

```bash
docker compose --env-file ./deploy/public.env -f docker-compose.public.yaml up -d
```

启动后访问：

```text
https://admin.example.com
```

不要把 `admin-api` 的内部监听端口 `8088` 直接暴露到公网；公网入口只保留 Caddy 的 `80/443`。

管理员账号默认来自：

- 用户名：`ADMIN_BOOTSTRAP_USERNAME`，默认 `admin`
- 密码：`ADMIN_BOOTSTRAP_PASSWORD`

说明：

- 公网部署时内部应用监听端口固定为 `8088`，镜像、健康检查和 Caddy 代理都按这个端口对齐，不需要在 `public.env` 里单独修改
- `ADMIN_BOOTSTRAP_PASSWORD` 是必填项；如果没配，服务会直接启动失败，避免部署成功但没有管理员可登录
- `docker-compose.public.yaml` 默认使用 `image:` 模式，不会在服务器部署时触发现场构建

### 5. 健康检查

- 后台应用健康检查：`https://admin.example.com/api/healthz`
- 反向代理健康检查：`https://admin.example.com/healthz`

## OTA 更新源

这版已经把最小 OTA 更新源接到了 `HSgram_admin`：

- Android manifest：`GET /api/updates/android/latest`
- PC manifest(JSON)：`GET /api/updates/pc/latest`
- PC 兼容旧检查器：`GET /td/current`
- 公共制品下载目录：`GET /releases/...`

建议目录：

```text
data/releases/
  android/latest.json
  android/HSgram-android-1.2.3.apk
  pc/latest.json
  pc/HSgram-pc-2.0.0.exe
```

示例 manifest 已放在：

- `deploy/release-examples/android.latest.json`
- `deploy/release-examples/pc.latest.json`

### Android 接入

当前主工程实际使用的是 `TMessagesProj_App`，不是 `AppHockeyApp`。现在主 App 已支持通过 `BuildConfig.BETA_URL` 读取自定义 manifest。

在 `HSgram_android/local.properties` 增加：

```text
BETA_URL=https://admin.example.com/api/updates/android/latest
```

然后正常构建当前使用的 `TMessagesProj_App` 变体即可；当 `BETA_URL` 非空时，客户端会：

- 定时检查 manifest
- 下载 APK
- 弹出更新提示并调起安装

### PC 接入

PC 端这次没有强行接入旧的签名更新包协议，而是做了“最小安装包更新”：

- 继续使用现有 `autoupdate_url_prefix + /current` 检查入口
- `HSgram_admin` 的 `/td/current` 会从 `pc/latest.json` 生成兼容响应
- 下载到本地的是普通安装包，不再要求先产出 `tupdate/tx64upd` 那套签名包
- 用户点击现有“Update”按钮后，会直接拉起安装包；旧缓存目录 `tdata` 不会被 OTA 逻辑主动清掉

要让 PC 指向你的 Admin 更新源，需要把 `autoupdate_url_prefix` 设成：

```text
https://admin.example.com/td
```

如果你沿用现有服务端下发配置，就让 HSgram 服务端把这个前缀发给客户端；如果你手动测试，也可以直接写 PC 的本地 `tdata/prefix`。

### 发布流程

1. 把 Android APK 和 PC 安装包复制到宿主机 `HSgram_admin/data/releases/android`、`HSgram_admin/data/releases/pc`
2. 生成或更新各自的 `latest.json`
3. 确认 `https://admin.example.com/api/updates/android/latest` 和 `https://admin.example.com/td/current` 可访问
4. 用旧版本客户端验证检查更新、下载和安装流程

### 发布新安装包

当前版本还没有做“后台上传制品”页面，发布新版本时直接替换宿主机目录中的安装包和 `latest.json` 即可。

制品目录：

```text
HSgram_admin/data/releases/
  android/
    latest.json
    HSgram-android-1.2.4.apk
  pc/
    latest.json
    HSgram-pc-2.0.1.exe
```

说明：

- `docker-compose.public.yaml` 已把宿主机 `./data/releases` 挂载到容器内 `/app/releases`
- 后台每次请求都会直接读取磁盘上的 manifest 和安装包
- 正常情况下，替换文件后不需要重启 `admin-api`

#### Android 发布

1. 构建新的 APK
2. 把 APK 放到 `HSgram_admin/data/releases/android/HSgram-android-<version>.apk`
3. 更新 `HSgram_admin/data/releases/android/latest.json`
4. 访问 manifest 和 APK 链接确认可下载

Android manifest 模板：

```json
{
  "version": "1.2.4",
  "version_code": 124,
  "file_url": "/releases/android/HSgram-android-1.2.4.apk",
  "changelog": "- 修复若干问题\n- 优化更新流程"
}
```

验证地址：

- `https://admin.example.com/api/updates/android/latest`
- `https://admin.example.com/releases/android/HSgram-android-1.2.4.apk`

#### PC 发布

1. 构建新的 PC 安装包，当前最小版支持普通安装包，例如 `.exe`、`.AppImage`、`.run`
2. 把安装包放到 `HSgram_admin/data/releases/pc/HSgram-pc-<version>.exe`
3. 更新 `HSgram_admin/data/releases/pc/latest.json`
4. 访问 manifest、兼容检查口和安装包链接确认可下载

PC manifest 模板：

```json
{
  "version": "2.0.1",
  "version_code": 2001,
  "download_url": "/releases/pc/HSgram-pc-2.0.1.exe",
  "changelog": "- 修复若干问题\n- 优化更新流程"
}
```

验证地址：

- `https://admin.example.com/api/updates/pc/latest`
- `https://admin.example.com/td/current`
- `https://admin.example.com/releases/pc/HSgram-pc-2.0.1.exe`

#### 最小上线检查清单

1. 新安装包文件名和 manifest 里的路径一致
2. `version_code` 比旧版本大
3. 浏览器能正常打开 manifest URL
4. 浏览器能正常下载安装包 URL
5. 用旧版本客户端点“检查更新”验证弹窗、下载、安装流程

## 低压力部署顺序

如果你的服务器是 `4核8G`，建议按下面顺序分步启动，而不是一次性拉起全部组件：

1. 先启动 HSgram 核心依赖：

```bash
cd /home/ubuntu/HSgram/HSgram_server
docker compose -f docker-compose.core.yaml up -d
```

2. 等 `mysql`、`redis`、`etcd`、`kafka` 稳定后，再启动 `teamgram`：

```bash
docker compose -f docker-compose.yaml up -d
```

3. 最后再启动 Admin：

```bash
cd /home/ubuntu/HSgram/HSgram_admin
docker compose --env-file ./deploy/public.env -f docker-compose.public.yaml up -d
```

4. 观测和日志组件按需启动，不要默认常驻：

```bash
cd /home/ubuntu/HSgram/HSgram_server
docker compose -f docker-compose.optional.yaml up -d
```

## 关键环境变量

- `ADMIN_IMAGE`: 预构建的 Admin 镜像名，服务器部署时直接复用
- `ADMIN_DATABASE_DSN`: HSgram MySQL 连接串
- `ADMIN_ENABLE_BROADCASTS`: 是否启用系统广播，默认 `false`
- `ADMIN_MSG_RPC_ADDR`: 广播启用时的消息服务地址
- `ADMIN_RELEASES_DIR`: OTA 制品目录，默认本地开发用 `./releases`，compose 默认为 `/app/releases`
- `ADMIN_PUBLIC_BASE_URL`: 对外基准地址，用来把 manifest 里的相对下载路径转换成完整 URL
- `ADMIN_JWT_SECRET`: 后台 JWT 签名密钥
- `ADMIN_BOOTSTRAP_USERNAME`: 启动时自动创建或更新的管理员用户名
- `ADMIN_BOOTSTRAP_PASSWORD`: 启动时自动创建或更新的管理员密码
- `ADMIN_LISTEN_ADDR`: 后台监听地址，默认 `:8088`，主要用于本地开发；公网 compose 默认固定使用该端口
- `ADMIN_TOKEN_TTL`: 登录 token 过期时间，默认 `12h`
- `ADMIN_SITE_ADDRESS`: 对外访问地址，例如 `admin.example.com`

## 说明

- 后台直接读取 `users` 和 `auth_users` 表，避免改动现有客户端 MTProto 链路。
- `admin_users` / `admin_audit_logs` 会在服务启动时自动建表。
- 审计日志和 `role` 字段已预留，后续可以继续扩展成 RBAC。
- `auth_users` 兼容新旧字段结构，适配已有迁移差异。
- 若只用公网 IP 而没有域名，HTTPS 证书自动签发通常不可用，建议正式环境务必配域名。
