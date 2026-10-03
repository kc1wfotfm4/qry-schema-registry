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

### `GET /healthz`

返回服务与存储状态。正常时 HTTP 200：

```json
{"status":"ok","database":"ok"}
```

存储不可用时 HTTP 503：

```json
{"error":{"code":"storage_unavailable","message":"database is not available"}}
```

### `POST /api/v1/subjects/{id}/versions`

为编号为 `{id}` 的主题注册新的结构版本。请求体是 JSON 对象：

```json
{"schema":"{\"fields\":{\"id\":\"string\"},\"required\":[\"id\"]}","compatibility":"BACKWARD"}
```

- `schema`：结构定义字符串，必须能解析为含 `fields` 与 `required` 的 JSON 对象；`fields` 把字段名映射到类型字符串，`required` 是其中字段名的子集。
- `compatibility`：`NONE`、`BACKWARD`、`FORWARD`、`FULL` 之一。

版本号按主题从事务内从 1 连续分配，并发注册不会重复或跳号；`schema` 原样持久化。成功时返回 HTTP 201：

```json
{"subject":1,"schema":"{\"fields\":{\"id\":\"string\"},\"required\":[\"id\"]}","version":1,"compatibility":"BACKWARD"}
```

错误响应（均为顶层 `error` 对象）：

| 场景 | HTTP | code |
|---|---|---|
| 请求体不是合法 JSON | 400 | `invalid_request` |
| 缺少 `schema` 或 `compatibility` | 400 | `missing_field` |
| `schema` 形状无效 | 400 | `invalid_schema` |
| `compatibility` 非法 | 400 | `invalid_compatibility` |
| 违反与同主题上一版本的兼容级别 | 409 | `incompatible_schema` |

兼容检查只比较同主题相邻两个版本：`NONE` 不检查；`BACKWARD` 允许新增可选字段、删除可选字段，禁止删除必填字段、改变同名字段类型、可选改必填、新增必填字段；`FORWARD` 允许新增字段、必填改可选，禁止删除字段、改变类型、可选改必填；`FULL` 要求两者同时满足。违反时不写入新版本，旧版本保持不变。

### `GET /api/v1/subjects`

列出至少注册过一个版本的主题。成功时返回 HTTP 200，`subjects` 是主题编号数组，按数值升序且不重复；尚无记录时返回空数组：

```json
{"subjects":[1,3]}
```

### `GET /api/v1/subjects/{id}/versions/{v}`

读取主题 `{id}` 的第 `{v}` 个版本。`{id}` 与 `{v}` 都必须是十进制整数。成功时返回 HTTP 200，`schema` 与 `compatibility` 保持注册时的原始值：

```json
{"subject":1,"schema":"{\"fields\":{\"id\":\"string\"},\"required\":[\"id\"]}","version":1,"compatibility":"BACKWARD"}
```

查询只读取已有记录，不会生成版本或重新计算兼容结果。错误响应（均为顶层 `error` 对象）：

| 场景 | HTTP | code |
|---|---|---|
| `{id}` 或 `{v}` 无法解析为十进制整数 | 400 | `invalid_request` |
| 主题没有任何版本 | 404 | `subject_not_found` |
| 主题存在但指定版本不存在 | 404 | `version_not_found` |

## 错误约定

所有错误响应都是单个顶层 `error` 对象，包含 `code` 与 `message` 两个字符串字段；`message` 不包含 SQL、堆栈或文件路径。
