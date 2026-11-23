# Spec: go-datax Web Admin（ETL 同步管理台）

> Status: ready-for-agent
> Tracker: 未配置（未运行 `/setup-matt-pocock-skills`），本文件暂存于本地 markdown 跟踪目录。

## Problem Statement

使用 go-datax 做数据同步时，作业配置是手写的 DataX JSON，执行要记住 `datax -job <file>` 命令，运行过程中的日志只存在于终端，跑完就散失。想改一个数据库连接，需要找出所有引用它的作业文件逐个改；想确认昨天那次同步为什么失败，没有任何历史记录可查；发现任务跑错想停下来，只能去任务管理器里杀进程。随着作业数量增加，这种方式既容易出错，也无法回答"现在同步是否正常"。

## Solution

提供一个单机运行的 Web 管理台：浏览器打开即用，登录后可以管理数据源与同步任务，一键运行并实时查看日志，随时停止运行中的任务，并在运行历史中回看每一次执行的配置快照与日志。管理台由仓库内新增的独立二进制 `datax-server` 提供，前端静态资源内嵌其中；任务定义保存在内置 SQLite 中，运行时以子进程方式调用现有的 `datax` 二进制，因此不改动 DataX 兼容性，也不改变 `datax` 命令行用法。

## User Stories

### 认证与会话

1. As an AdminUser, I want to log in with the username and password configured on the server, so that only I can manage data sources and sync jobs.
2. As an AdminUser, I want my login to persist across page reloads and server restarts until the token expires, so that I do not need to log in repeatedly during the day.
3. As an AdminUser, I want to be redirected to the login page when my token is missing or expired, so that I understand why my actions stopped working.
4. As an AdminUser, I want to be returned to the page I was on after logging in again, so that an expiry does not lose my place.
5. As an AdminUser, I want to log out explicitly, so that a shared machine does not keep my access.
6. As an operator, I want the server to generate a random password on first start and print it once, so that setup is zero-configuration without shipping a default password.
7. As an operator, I want to store a bcrypt hash instead of a plaintext password in the config file, so that reading the file does not reveal a usable credential.
8. As an operator, I want the server to generate and persist a JWT signing secret when none is configured, so that tokens survive restarts without manual key management.

### 概览

9. As an AdminUser, I want to see the total number of data sources and sync jobs, so that I can confirm the environment is populated.
10. As an AdminUser, I want to see recent JobRuns with their status and duration, so that I can tell at a glance whether sync is healthy.
11. As an AdminUser, I want to see a success rate over a chosen recent window, so that I can spot a deteriorating job without scanning every run.
12. As an AdminUser, I want to jump from a recent JobRun directly to its detail, so that investigating a failure takes one click.

### DataSource 管理

13. As an AdminUser, I want to create a DataSource with a name, type, JDBC URL, username and password, so that sync jobs can reuse one connection definition.
14. As an AdminUser, I want to choose among MySQL, PostgreSQL and SQLite for a DataSource, so that the common local sources are fully supported by the forms.
15. As an AdminUser, I want to test an unsaved DataSource's connection, so that I find wrong credentials before saving.
16. As an AdminUser, I want to test a saved DataSource's connection, so that I can diagnose a connection that later broke.
17. As an AdminUser, I want the test result to show success or the underlying error message, so that I know what to fix.
18. As an AdminUser, I want passwords masked in lists and detail views, so that screen sharing does not expose credentials.
19. As an AdminUser, I want editing a DataSource without retyping the password to keep the existing password, so that routine edits do not force credential re-entry.
20. As an AdminUser, I want duplicate DataSource names rejected with a clear message, so that references stay unambiguous.
21. As an AdminUser, I want deleting a DataSource that is referenced by a SyncJob to be blocked with an explanation, so that I cannot break existing jobs by accident.
22. As an AdminUser, I want to search DataSources by name, so that I can find one quickly as the list grows.

### SyncJob 管理

23. As an AdminUser, I want to create a SyncJob in form mode by picking a reader DataSource, a table, columns, an optional where clause and split column, and a writer DataSource with its table and columns, so that common sync jobs need no JSON.
24. As an AdminUser, I want to set the channel count and error limit in form mode, so that I can tune throughput and tolerance without leaving the UI.
25. As an AdminUser, I want to create a SyncJob in raw mode by pasting a complete DataX JSON document, so that any of the 30+ plugins remains usable.
26. As an AdminUser, I want raw JSON validated for structure and required fields before saving, so that I do not discover a typo when the run starts.
27. As an AdminUser, I want form-mode jobs validated for missing DataSources, empty tables and column count mismatches, so that obvious configuration errors are caught early.
28. As an AdminUser, I want the authoring mode fixed after creation, so that the UI never has to guess how to render a job.
29. As an AdminUser, I want to edit, rename and duplicate a SyncJob, so that building a variant does not start from scratch.
30. As an AdminUser, I want to delete a SyncJob while keeping its JobRun history, so that cleanup does not erase evidence.
31. As an AdminUser, I want to import an existing DataX JSON file as a raw-mode SyncJob, so that migrating from file-based DataX is one paste.
32. As an AdminUser, I want to export a SyncJob as standard DataX JSON, so that I can run it outside the admin or store it in version control.
33. As an AdminUser, I want export to include resolved connection credentials with an explicit warning, so that the exported file is runnable as-is.
34. As an AdminUser, I want to search and filter SyncJobs by name, so that finding one among many is fast.
35. As an AdminUser, I want each SyncJob row to show the last JobRun status and time, so that I can scan for failures.

### 运行与实时日志

36. As an AdminUser, I want to start a JobRun from the SyncJob list or detail page, so that running a job takes one click.
37. As an AdminUser, I want the run button disabled while that SyncJob is running, so that I cannot accidentally start a duplicate.
38. As an AdminUser, I want a clear conflict message if a duplicate start is attempted, so that I understand why nothing happened.
39. As an AdminUser, I want a clear message when the server's global run limit is reached, so that I know to wait rather than retry blindly.
40. As an AdminUser, I want to follow a running job's log live, so that I can see progress without leaving the browser.
41. As an AdminUser, I want the log view to keep following when I scroll to the bottom and stop following when I scroll up, so that reading history is not interrupted.
42. As an AdminUser, I want a running job's status to update without manual refresh, so that I know when it finishes.
43. As an AdminUser, I want to stop a running job, so that a mistaken or runaway sync does not have to be killed in the OS.
44. As an AdminUser, I want a canceled run to be clearly marked as canceled with its logs retained, so that the history reflects what happened.
45. As an AdminUser, I want to view the full log of a finished JobRun, so that I can diagnose failures after the fact.
46. As an AdminUser, I want to download a run's log file, so that I can share it or analyze it elsewhere.
47. As an AdminUser, I want to view and download the Run Snapshot of a finished run, so that I can reproduce exactly what was executed.

### 运行历史

48. As an AdminUser, I want a paginated list of JobRuns filterable by SyncJob, status and time range, so that I can find a specific execution.
49. As an AdminUser, I want a run detail page showing status, start and end time, duration, exit code and error summary, so that I do not have to read the whole log first.
50. As an AdminUser, I want runs that were in progress when the server restarted marked as failed, so that the history has no permanently stuck entries.
51. As an AdminUser, I want old JobRuns and their logs pruned automatically by count, age and log size limits, so that the data directory does not grow without bound.

### 部署与运维

52. As an operator, I want `datax-server` to serve both the API and the built UI from one binary, so that deployment is a single file plus a data directory.
53. As an operator, I want the server to default to `127.0.0.1:8080` and accept a `--listen` override, so that local use is safe and remote binding is explicit.
54. As an operator, I want the data directory configurable and defaulted per OS, so that state lives somewhere conventional.
55. As an operator, I want the server to fail fast with a clear message when the `datax` binary cannot be found, so that misconfiguration surfaces at startup.
56. As an operator, I want a health endpoint reporting server version and resolved datax version, so that I can verify a deployment.
57. As an operator, I want the config file parsed with flags overriding file values, so that a one-off override does not require editing the file.
58. As an operator, I want auth, datax location, listen address and retention configurable in one JSON file, so that operations are manageable without code changes.

### 界面与可用性

59. As an AdminUser, I want the interface in Chinese, so that terminology matches how I work.
60. As an AdminUser, I want clear empty states and error messages, so that a fresh install or a failed request explains what to do next.
61. As an AdminUser, I want form validation messages next to the offending field, so that correcting input is quick.
62. As an AdminUser, I want the UI to keep working after a temporary network drop by reconnecting the log stream and refetching state, so that a flaky connection does not require a page reload.
63. As an AdminUser, I want the raw JSON editor to highlight syntax errors and format the document, so that hand-editing large configs is practical.

## Implementation Decisions

### 仓库与交付形态

- 新增 `web/`（React 19 + Vite 8 + TypeScript strict 前端）与 `cmd/datax-server`（独立服务二进制）。现有 `datax` CLI 的行为与入口不变。
- `datax-server` 通过 `go:embed` 提供前端静态资源；未匹配的非 API 路径回退到 `index.html`，支持前端路由直达。
- 前端产物目录需要在内嵌包编译时存在，因此仓库保留一个占位 `index.html`；`make build` 先执行 `pnpm --dir web build` 再编译两个二进制。Makefile 现有的 `cmd/` 多应用发现机制会自动包含 `datax-server`。

### 认证与配置

- 始终要求登录，包括监听 `127.0.0.1` 时。
- 登录端点校验配置中的单账号；密码支持 bcrypt 哈希（`$2a$`/`$2b$` 前缀自动识别）与明文两种形式。
- 登录成功签发 HS256 JWT，claims 至少包含 `sub`（用户名）、`iat`、`exp`、`iss`（固定标识）。TTL 默认 12 小时，可配置。
- `jwtSecret` 未配置时，首次启动生成 32 字节随机值并写回配置文件。
- 前端把 token 存 `localStorage`；请求统一附加 `Authorization: Bearer <token>`；收到 401 清除 token 并跳转登录页，登录后回到原页面。
- MVP 不支持服务端吊销；登录失败不做锁定或限流。
- 配置文件默认 `<data-dir>/server.json`，`--config` 可覆盖；flag 优先级高于文件。配置文件包含 `listen`、`dataDir`、`auth`（username/password/tokenTTL/jwtSecret）、`datax`（bin）、`retention`（maxRuns/maxAgeDays/maxLogBytes）。
- 首次启动若配置文件不存在：创建默认配置（含随机密码与随机 `jwtSecret`），权限 0600，并在控制台打印一次生成的用户名与密码。
- 提供 `datax-server hash-password` 子命令，从标准输入或参数读取明文，输出 bcrypt 哈希。

### 领域模型与持久化

- SQLite 单文件（GORM + 已引入的纯 Go `glebarez/sqlite` 驱动），启用 WAL。表结构：
  - `data_sources`: id、name（唯一）、type（`mysql` | `postgres` | `sqlite`）、jdbc_url、username、password、properties(JSON)、created_at、updated_at
  - `sync_jobs`: id、name、authoring_mode（`form` | `raw`）、definition(JSON)、created_at、updated_at
  - `job_runs`: id、sync_job_id（可空）、sync_job_name（创建时快照）、status（`running` | `succeeded` | `failed` | `canceled`）、trigger（MVP 固定 `manual`）、started_at、finished_at、exit_code、error_summary、snapshot_path、log_path、log_size、created_at
- 不建立用户表：AdminUser 来自配置文件。
- DataSource 的 `password` 以明文存本地数据库；所有 API 响应一律脱敏为固定掩码，编辑时提交空密码表示保持不变。快照文件包含已展开的凭据，依赖数据目录文件权限保护。
- 删除被 SyncJob 引用的 DataSource 返回 409，并在响应中列出引用它的 SyncJob 名称。
- 删除 SyncJob 为硬删除，但 `job_runs` 保留并依靠 `sync_job_name` 展示历史；正在运行的 SyncJob 禁止删除（409）。
- 保留策略：启动时与每次运行结束后清理，保留最近 `maxRuns` 条（默认 500）且不超过 `maxAgeDays`（默认 30）；单次日志超过 `maxLogBytes`（默认 10MB）时停止写入并在日志尾部追加截断提示。

### SyncJob 定义与快照

- form 模式的 `definition` 保存：`reader`（dataSourceId、table、column、where、splitPk）、`writer`（dataSourceId、table、column、writeMode）、`setting`（channel、errorLimit）。
- raw 模式的 `definition` 保存完整 DataX 配置对象（含 `job` 段）。
- form → DataX 的展开规则：DataSource 类型映射到插件名（`mysql` → `mysqlreader`/`mysqlwriter`，`postgres` → `postgresqlreader`/`postgresqlwriter`，`sqlite` → `sqlitereader`/`sqlitewriter`）；jdbcUrl 转成 DataX 期望的数组或字符串形式；username/password 注入；`column` 与 `channel` 原样传递；未填写 `writeMode` 时使用 `insert`。
- 启动 JobRun 时先展开并校验（插件存在、必填段齐全、列数量一致），成功后写入 `snapshots/<runId>.json` 并启动子进程；校验失败不创建 JobRun，直接返回 422。
- 导出使用同一展开逻辑，输出标准 DataX JSON，包含解析后的凭据，并在 UI 中提示这一行为。

### 执行模型

- 每次 JobRun 启动 `<datax-bin> -job <snapshot>` 子进程；stdout 与 stderr 合并写入 `logs/<runId>.log`，同时推送给订阅者。
- 同一 SyncJob 单飞：已有 `running` 记录时返回 409。全局并发上限 2，超出返回 429。
- 取消：结束进程树（Windows 使用 `taskkill /T /F` 或等效 Job Object 方案，Unix 使用进程组信号），状态置 `canceled`；进程正常退出但被标记取消时也保持 `canceled`。
- 启动时把所有残留 `running` 记录改为 `failed`，`error_summary` 说明服务重启导致中断。
- 子进程退出码 0 → `succeeded`；非 0 → `failed`，`error_summary` 取日志末尾非空行（截断到合理长度）。
- `datax` 二进制定位顺序：`--datax-bin` / 配置 `datax.bin` → server 可执行文件同目录下的 `datax` → `PATH`。启动时执行 `datax -version` 验证并记录版本。

### HTTP API

统一前缀 `/api/v1`，JSON 请求与响应，错误响应形如 `{"error":{"code":"...","message":"..."}}`；除登录与健康检查外均需 Bearer token。

- 认证：`POST /auth/login`、`GET /auth/me`
- 数据源：`GET|POST /data-sources`、`GET|PUT|DELETE /data-sources/{id}`、`POST /data-sources/test`
- 同步任务：`GET|POST /sync-jobs`、`GET|PUT|DELETE /sync-jobs/{id}`、`POST /sync-jobs/{id}/duplicate`、`POST /sync-jobs/import`、`GET /sync-jobs/{id}/export`、`POST /sync-jobs/{id}/runs`
- 运行：`GET /runs`（分页与过滤）、`GET /runs/{id}`、`POST /runs/{id}/cancel`、`GET /runs/{id}/logs/stream`（SSE）、`GET /runs/{id}/log/download`、`GET /runs/{id}/snapshot`
- 概览：`GET /overview`
- 健康：`GET /health`（无需认证）

SSE 事件：`log`（`{seq,line}`）、`status`（`{status,exitCode,finishedAt}`）、`end`；支持 `offset` 参数从头或指定位置回放，15 秒心跳；同一运行允许多个订阅者。

### 前端

- 路由：`/login`、`/`（概览）、`/data-sources`、`/sync-jobs`、`/sync-jobs/new`、`/sync-jobs/:id/edit`、`/runs`、`/runs/:id`。
- 状态：TanStack Query 管理服务端状态；登录态与 token 放在轻量 Context 中；不使用额外全局状态库。
- 表单：Ant Design 6 `Form`，中文校验文案；数据源密码字段留空表示不变。
- JSON 编辑：Monaco Editor，JSON 语言模式，提供格式化与语法错误提示；服务端仍做结构与语义校验。
- 日志：SSE Hook 支持自动重连（指数退避）、粘底滚动、暂停跟随；运行结束时显示状态与退出码。
- 文案集中在一个模块中管理，但 MVP 不引入 i18n 库。
- 开发模式下 Vite dev server 将 `/api` 代理到 `127.0.0.1:8080`；生产由 `datax-server` 同源提供。

## Testing Decisions

**什么是好测试**：只断言外部可观察行为——HTTP 状态码与响应体、数据库经 API 呈现的结果、日志流内容、进程生命周期与状态流转；不断言私有函数、内部字段或实现顺序。唯一例外是纯函数式的配置展开逻辑，用表驱动单测直接锁定输入输出。

**选定的测试缝隙（seams）**：

1. **HTTP API（最高缝隙，主要缝隙）**：用 `httptest` 启动真实路由、真实 SQLite（临时目录）、真实执行管理器，仅把 `datax` 替换为一个测试期构建的假二进制。该缝隙覆盖认证、数据源 CRUD 与引用约束、任务 CRUD、展开与校验、运行启动/取消、状态流转、SSE 日志、保留策略。API 测试通过一个登录辅助函数拿到 token。
2. **配置展开（纯函数缝隙）**：`SyncJob + DataSource → DataX JSON` 的展开与校验作为无 IO 的纯函数，表驱动覆盖三种数据源映射、缺失数据源、列数不一致、默认值填充、raw 直通。
3. **前端页面（页面级缝隙）**：Vitest + Testing Library 渲染完整页面（Router + QueryClient + AntD），HTTP 用 MSW 拦截。覆盖登录跳转与回跳、数据源表单校验与密码留空语义、任务运行按钮的禁用/冲突提示、日志视图渲染与状态更新。不做组件内部实现断言。

**假 datax 二进制**：`TestMain` 中把一个小型 Go 辅助程序构建为临时可执行文件，通过环境变量控制退出码、输出行数与延迟，用于成功、失败、慢任务与取消场景。这样测试不依赖真实数据库，也不依赖系统 shell。

**现有先例**：仓库目前没有 HTTP 层测试；最近为 JSON 读取器新增的表驱动单测是唯一的 Go 测试先例，风格上沿用表驱动与明确的 `t.Fatalf` 失败信息。前端为全新测试设施，Vitest + Testing Library + MSW 一次到位。

**质量闸门**：`go vet ./...` 与 `go test ./...` 必须通过；`pnpm --dir web lint && pnpm --dir web test && pnpm --dir web build` 必须通过。MVP 不引入 Playwright E2E，以构建产物手工冒烟（登录 → 建数据源 → 建任务 → 运行 → 看日志 → 取消）作为验收。

## Out of Scope

- cron/定时调度、任务依赖与 DAG 编排
- 告警通知（邮件、Webhook、IM）
- 多用户、RBAC、审计日志
- 性能图表、历史吞吐趋势、数据质量校验
- 分布式执行、远程 agent、SSH 执行
- Tauri 桌面打包（保留为 M2 选项）
- CI 中构建前端、发布流程改造
- Playwright E2E
- MySQL/PostgreSQL/SQLite 之外的 DataSource 表单与连接测试（其他插件通过 raw 模式使用）
- form 与 raw 模式之间的相互转换
- JWT 服务端吊销、登录限流与账号锁定
- 国际化（仅中文界面）

## Further Notes

- 未运行 `/setup-matt-pocock-skills`，本 spec 暂存 `.scratch/etl-admin/spec.md`；配置 tracker 后可迁移为 GitHub Issue 并保留 `ready-for-agent` 标签。
- 风险与注意点：Windows 下子进程树终止需要专门实现并测试；快照含明文凭据，数据目录权限与保留策略必须可靠；Monaco 会增加前端体积并需要 Vite worker 配置；SQLite 与 Go 子进程同时访问同一数据文件时需确认不与业务库混用（管理库只存元数据）。
- M2 候选：cron 调度、运行通知、多用户与权限、吞吐趋势图、Tauri 桌面壳、远程执行器、CI 集成。
