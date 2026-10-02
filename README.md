# qry-schema-registry

把主题的结构定义、版本号、兼容级别与演进检查结果记录成可查询的服务，支持按主题读取指定版本并拒绝破坏兼容性的新版本注册请求。

## 运行要求

- Go 1.26 或以上
- SQLite（本服务自带存储，不需要外部数据库）

## 构建、测试与启动

```bash
go build ./...
go test ./...
go run .
```

服务默认监听 `127.0.0.1:8080`。可用环境变量覆盖：

| 变量 | 默认值 | 用途 |
|---|---|---|
| `ADDR` | `127.0.0.1:8080` | HTTP 监听地址 |
| `DB_PATH` | `qry-schema-registry.db` | SQLite 数据库文件路径 |

## 已公开的入口

### `POST /api/v1/subjects/{id}/versions`

为主题 `{id}` 注册一个新的结构版本。请求体是 JSON 对象：

```json
{"schema":"{\"fields\":{\"id\":\"string\"},\"required\":[\"id\"]}","compatibility":"BACKWARD"}
```

- `schema`：结构定义字符串，本身必须解析为含 `fields` 与 `required` 的 JSON 对象；`fields` 把字段名映射到类型字符串，`required` 是其中字段名的子集。
- `compatibility`：`NONE`、`BACKWARD`、`FORWARD`、`FULL` 之一。

版本号按主题在事务内从 1 连续分配，并发注册不会重复或跳号。成功时 HTTP 201：

```json
{"subject":"user-events","schema":"{\"fields\":{\"id\":\"string\"},\"required\":[\"id\"]}","version":1,"compatibility":"BACKWARD"}
```

同主题相邻版本按兼容级别检查：`NONE` 不检查；`BACKWARD` 允许新增可选字段或删除可选字段，禁止删除必填字段、改变同名字段类型、把可选改为必填；`FORWARD` 允许新增字段或把必填改为可选，禁止删除字段、改变类型、把可选改为必填；`FULL` 同时满足两者。违反时返回 HTTP 409 且 `code` 为 `incompatible_schema`，不写入新版本也不改旧版本。

请求校验失败时均返回 HTTP 400：请求体不是合法 JSON 时 `code` 为 `invalid_request`；缺少 `schema` 或 `compatibility` 时为 `missing_field`；`schema` 形状无效时为 `invalid_schema`；`compatibility` 非法时为 `invalid_compatibility`。

### `GET /healthz`

返回服务与存储状态。正常时 HTTP 200：

```json
{"status":"ok","database":"ok"}
```

存储不可用时 HTTP 503：

```json
{"error":{"code":"storage_unavailable","message":"database is not available"}}
```

## 错误约定

所有错误响应都是单个顶层 `error` 对象，包含 `code` 与 `message` 两个字符串字段；`message` 不包含 SQL、堆栈或文件路径。
