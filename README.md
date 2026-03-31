# API 2 Cursor

Cursor IDE API 代理网关。

> **致敬** 协议桥接与「让 Cursor 走第三方中转」的原始构想来自开源项目 [h88782481/api2cursor](https://github.com/h88782481/api2cursor)（Python / Flask）；本仓库为其 **Go 重写与功能扩展**，详见下方 [致谢](#致谢)。

## 功能特性

### 核心功能
- **多 API Key** — 可创建与管理多个 API Key，各自独立鉴权与限额
- **多渠道** — 同时配置多个 API 提供商渠道，支持优先级和权重负载均衡
- **多模型映射** — 灵活的模型名映射，将 Cursor 模型名映射到上游实际模型
- **协议转换** — 自动在 OpenAI / Anthropic / Gemini / Responses 四种协议间双向转换
- **流式支持** — 完整的 SSE 流式代理，包括流式协议转换
- **用量统计** — 按 API Key、渠道、模型维度的请求日志和 Token 用量统计
- **管理面板** — 暗色主题 Web 管理界面，支持所有资源的 CRUD 操作

### 渠道类型
| 类型 | 说明 | 上游端点 |
|------|------|---------|
| `openai` | OpenAI 兼容接口 | `/v1/chat/completions` |
| `anthropic` | Anthropic Messages | `/v1/messages` |
| `gemini` | Google Gemini | `generateContent` |
| `responses` | OpenAI Responses | `/v1/responses` |

### 负载均衡
- **优先级调度** — 高优先级渠道优先使用
- **权重均衡** — 同优先级渠道按权重随机分配
- **模型过滤** — 渠道可限制仅支持特定模型
- **故障统计** — 自动记录渠道失败次数

## 快速开始

### 方式一：直接运行

```bash
# 克隆并进入项目（默认目录名为 A2esr）
git clone https://github.com/hoshino-nyan/A2esr.git
cd A2esr

# 复制配置
cp .env.example .env

# 编辑配置
# 编辑 .env 设置 ADMIN_TOKEN

# 编译运行
go build -o api2cursor .
./api2cursor
```

### 方式二：Docker Compose

```bash
cd A2esr
cp .env.example .env
# 编辑 .env

docker compose up -d
```

### 方式三：Docker 自动部署脚本

```bash
cd A2esr
chmod +x deploy.sh
./deploy.sh
```

### 仅用现成镜像部署

将本仓库中的 `compose.yml`、`.env.example`、`deploy.sh` 拷到服务器同一目录，`cp .env.example .env` 后按需编辑，再执行 `docker compose --env-file .env pull && docker compose --env-file .env up -d`，或运行 `./deploy.sh deploy`。镜像名以 `compose.yml` 中 `image` 为准。

### 首次使用

1. 访问 `http://localhost:28473/admin` 进入管理面板
2. 使用 `ADMIN_TOKEN` 或默认密钥 `sk-api2cursor-default` 登录
3. 在「渠道管理」中添加至少一个 API 渠道
4. 在「模型映射」中配置模型名映射
5. 在 Cursor 中配置 API 地址为 `http://localhost:28473`，API Key 为你创建的密钥

## 配置说明

### 环境变量

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `PORT` | `28473` | 服务端口 |
| `LISTEN_ADDR` | `127.0.0.1` | 监听地址；本机反代建议保持默认。Docker Compose 内固定为 `0.0.0.0`，宿主机端口仅绑定 `127.0.0.1` |
| `ADMIN_TOKEN` | _(空)_ | 管理员令牌 |
| `DB_PATH` | `data/api2cursor.db` | SQLite 数据库路径 |
| `DEBUG_MODE` | `off` | 调试模式 (off/simple/verbose) |

### API 端点

| 路径 | 方法 | 说明 |
|------|------|------|
| `/v1/chat/completions` | POST | Chat Completions 代理 |
| `/v1/responses` | POST | Responses API 代理 |
| `/v1/messages` | POST | Anthropic Messages 透传 |
| `/v1/models` | GET | 模型列表 |
| `/health` | GET | 健康检查 |
| `/admin` | GET | 管理面板 |

### 管理 API

所有管理 API 需要 Admin 权限。

| 路径 | 方法 | 说明 |
|------|------|------|
| `/api/admin/login` | POST | 管理员登录 |
| `/api/admin/users` | GET/POST | 账户资源列表/创建（与 Key 归属关联） |
| `/api/admin/users/{id}` | PUT/DELETE | 账户资源更新/删除 |
| `/api/admin/keys` | GET/POST | API Key 列表/创建 |
| `/api/admin/keys/{id}` | PUT/DELETE | API Key 更新/删除 |
| `/api/admin/channels` | GET/POST | 渠道列表/创建 |
| `/api/admin/channels/{id}` | PUT/DELETE | 渠道更新/删除 |
| `/api/admin/mappings` | GET/POST | 模型映射列表/创建 |
| `/api/admin/mappings/{id}` | PUT/DELETE | 模型映射更新/删除 |
| `/api/admin/stats` | GET | 用量统计 |

## 架构

```
Cursor IDE                    API 2 Cursor                      上游渠道
    │                              │                               │
    ├─ /v1/chat/completions ──→ 认证 → 渠道选择 → 协议转换 ──→ OpenAI
    │                              │                           ├── Anthropic
    ├─ /v1/responses ─────────→ 认证 → 渠道选择 → 协议转换 ──→ Gemini
    │                              │                           └── Responses
    └─ /v1/messages ──────────→ 认证 → 渠道选择 → 透传 ──────→ Anthropic
```

### 技术栈
- **语言**: Go 1.22+
- **数据库**: SQLite (WAL mode)
- **依赖**: `github.com/mattn/go-sqlite3`
- **前端**: 原生 HTML/CSS/JS (暗色主题)
- **部署**: Docker / Docker Compose

## 致谢

- **[api2cursor](https://github.com/h88782481/api2cursor)**（[MIT](https://github.com/h88782481/api2cursor/blob/main/LICENSE)）：原版以 Python / Flask 实现 Cursor 与中转站之间的 Chat Completions、Responses、Messages 等协议转换，思路清晰、文档完善，是本项目的灵感来源。
- 当前代码为 **独立 Go 实现**，在架构（SQLite 多 Key/渠道/映射、管理面板与部署方式等）上与原版并不相同；若你更偏好单中转站 + Flask 栈，可直接使用上述原仓库。

## License

MIT
