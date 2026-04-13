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

注意：

- 如果 MySQL 就运行在同一台服务器并且对宿主机开放了 `3306`，可以直接保留 `host.docker.internal:3306`
- 如果 MySQL 在另一台机器，请把 `host.docker.internal` 改成真实数据库地址
- 上面的 `host.docker.internal` 之所以可用，是因为 `docker-compose.public.yaml` 已通过 `extra_hosts` 把它映射到了宿主机网关；如果你不用这份 compose，需要自己补这层映射
- 如果 MySQL 只监听 `127.0.0.1`，容器通常仍然连不上；正式部署时请确认 MySQL 监听地址允许来自 Docker 容器所在网段，或直接把数据库部署到同一 Docker 网络里
- `ADMIN_ENABLE_BROADCASTS=false` 时，后台只启用用户管理能力，不依赖消息服务链路，最适合 `4核8G` 的轻量部署
- 只有要使用系统广播时，才把 `ADMIN_ENABLE_BROADCASTS=true`，并同时配置可用的 `ADMIN_MSG_RPC_ADDR`

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
