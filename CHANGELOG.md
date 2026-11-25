# Changelog

All notable changes to this project will be documented in this file.

## [Unreleased]

### BREAKING CHANGES
- **PostgreSQL and MySQL Writer Behavior Change**: Changed conflict handling from SKIP to UPSERT strategy
  - **Previous Behavior**: Used `ON CONFLICT DO NOTHING` (PostgreSQL) and `INSERT IGNORE` (MySQL) to skip conflicting records
  - **New Behavior**: Automatically queries primary keys and updates non-PK columns on conflicts using `ON CONFLICT DO UPDATE` (PostgreSQL) and `ON DUPLICATE KEY UPDATE` (MySQL)
  - **Impact**: Existing records will now be updated instead of skipped when primary key conflicts occur
  - **Edge Cases Handled**:
    - Tables with no primary key: Falls back to standard INSERT
    - All-primary-key tables (e.g., junction tables): Falls back to DO NOTHING/IGNORE to avoid syntax errors
    - Composite primary keys: Fully supported with proper conflict detection
  - **Migration Note**: If your workload relies on skipping duplicates, you may see different behavior. Consider reviewing your data sync logic.
- **告警管理取代系统设置中的通知设置**: 旧的单 Webhook 通知接口已移除
  - `GET/PUT /api/v1/settings/notifications` 与 `/test` 不再存在，改用 `GET/PUT /api/v1/alerts/config`、`POST /api/v1/alerts/config/test`
  - 首次启动时会自动把旧的 `notifications` 配置迁移为一条告警渠道，无需手工重配
  - 告警日志通过 `GET/DELETE /api/v1/alerts/logs` 查询与清理
- **管理面更名为 `etl-server`，DataX API 加 `/datax` 前缀**:
  - `datax-server` 二进制与 `cmd/datax-server` 不再存在，改用 `etl-server`；不保留别名，运维脚本需同步更新
  - DataX 专属 API 移到 `/api/v1/datax/sync-jobs`、`/api/v1/datax/datasets`（原 `/api/v1/query-templates`）、`/api/v1/datax/collect-tasks`、`/api/v1/datax/runs`
  - 共享 API（data-sources、alerts、monitor、settings、overview、auth、health）保持顶层
  - JWT issuer 改为 `etl-server`，已有 token 失效一次，需要重新登录
  - `datax` CLI、`admin.db` 与现有表名不变

### Changed
- **架构改进**: 重构为无状态设计，支持灵活的执行模式
  - 新增 `core/job/job.go`: 实现了符合 Go 哲学的 `Job` 结构体，类似 `exec.Cmd` 模式
  - `Job` 提供 `Start()` (异步), `Wait()` (阻塞), `Run()` (同步) 方法
  - `Job` 提供 `Status()` 方法实时查询任务状态和进度
  - `Job` 提供 `Done()` channel 用于 Go 风格的完成信号
  - 任务完成后自动释放重对象 (`JobContainer`)，只保留轻量级统计摘要
  - `JobContainer` 新增 `StartWithContext()` 方法支持 context 取消机制
  - `JobContainer.Start()` 保持向后兼容，内部调用 `StartWithContext()`
  - `Engine` 简化为无状态设计，删除未使用的 `configuration` 字段
  - CLI 模式完全向后兼容，无破坏性变更

### Added
- **etl-worker 独立执行器**:
  - 新增 `etl-worker` 二进制与 HTTP/JSON + SSE 协议：shared token、诊断 run、进度、日志与取消
  - `etl-server` 默认启动并监管本地 worker；支持 `worker.url` + `worker.token` 连接外部 worker
  - worker 崩溃时在飞运行标记失败并自动重启；worker 不访问 `admin.db`
  - 诊断运行写入 `pipeline_runs`（状态/起止/错误）；server 重启、worker 崩溃或外部连接中断时标记失败
  - 新增配置项 `worker.url`、`worker.token`、`worker.bin`、`worker.listen`
- **告警管理与告警日志**:
  - 新增「告警管理」菜单，包含「告警配置」「告警日志」页面
  - 渠道类型：邮箱（SMTP，支持 SSL/TLS 与 STARTTLS）、钉钉、飞书、企业微信、Slack、通用 Webhook
  - 每个渠道可独立选择失败、恢复、成功、取消事件；钉钉/飞书机器人支持加签密钥
  - SMTP 密码与签名密钥在接口返回时掩码，更新时留空或保持掩码表示不修改
  - 运行结束按事件分发到所有匹配渠道；失败后会自动重试的任务只在最终失败或恢复时告警
  - 每次投递（含测试发送）都会记录告警日志，包含渠道、事件、任务、运行、耗时与错误，支持筛选、分页、清空与按保留天数自动清理
- **管理台「服务监控」页面**:
  - 新增 `GET /api/v1/monitor`，每 10 秒采样进程 CPU 使用率、协程数量与状态、堆内存和 GC 次数，保留最近 15 分钟
  - 新增「运行中心 → 服务监控」页面，包含处理器、协程、协程状态、内存四组趋势图，每 10 秒自动刷新
  - 协程状态通过解析运行时协程堆栈归为运行中、阻塞中、等待中、系统调用与其他
  - CPU 使用率按整机核心数折算，Windows 使用 `GetProcessTimes`、Unix 使用 `getrusage` 采集进程 CPU 时间
- **`dx_script` Custom Script Transformer**:
  - New record-level transformer backed by [go-scripts](https://github.com/tx7do/go-scripts) pure-Go engines
  - Supports `language`: `javascript` (goja), `starlark` (starlark-go) and `expr` (expr-lang)
  - Configure with `code` or `codeFile` (relative paths resolve against `$GO_DATAX_HOME`, falling back to `$ADDAX_HOME`)
  - Language-neutral record API: `get_string`/`set_long`/`set_double`/`set_bool`/`set_date`/`set_bytes`/`set_null`, `is_null`, `get_type`, `column_count`, `log`
  - Returning `false` filters the record; script errors, conversion failures and out-of-range columns become dirty data
  - JavaScript supports a per-record `timeoutMs`; scripts are compiled once and executed per record without recompilation
  - `dx_groovy` remains unsupported (no JVM); `dx_script` is the replacement, and existing Groovy scripts must be rewritten
- **Transformer Pipeline with Addax-compatible Metrics and Dirty Data Handling**:
  - Re-enabled the transformer chain (`common/transformer`, `core/registry.TransformerChannel`), which had been commented out during the architecture refactor
  - Built-ins: `dx_substr`, `dx_pad`, `dx_replace`, `dx_filter`, plus go-datax's `dx_digest`
    - `dx_replace` supports the Addax positional form (`pos`, `length`, `str`) and the legacy 2-parameter global replace form
    - `dx_filter` uses full-match regular expressions for `like`/`not like` and follows Addax null semantics
    - String positions/lengths are rune-based, so CJK values are not split by bytes
  - `dx_groovy`/`dx_map` fail fast with an explicit "not supported" error
  - Metrics: transform input/output records & bytes, dirty records & bytes, success/failed/filter counts, and total/per-transformer used time
  - Dirty data: the first failing transformer marks the record dirty, skips the remaining transformers and drops the record; dirty records count toward `errorLimit`
  - `errorLimit.percentage` now accepts both DataX-style fractions (`0.02`) and percentages (`5`); previously the integer conversion truncated fractions and disabled the limit
  - `JobStatus`/`StatsSummary` expose a `transformer` metrics block; fixed writer statistics to increment `writeReceivedRecords` so `succeedRecords` is no longer always zero
  - Admin server: raw job validation now accepts valid transformer chains and rejects invalid ones (previously rejected every transformer config)
- **Multi-Table Write Support with Intuitive Channel Semantics**: Enhanced multi-table split logic beyond DataX Java
  - **Channel Semantic**: `channel` parameter always means "concurrency per table" (not total tasks)
  - **Single-Table Mode**: `channel: N` → Creates N parallel tasks for that table
  - **Multi-Table Mode**: `channel: N` → Creates N tasks per table (total = tableCount × N)
  - **Go Enhancement**: More intuitive than Java's restrictive "tableCount must equal taskCount" constraint
  - **SQL Placeholder Support**: `@table` placeholder in preSql/postSql is replaced with actual table name per task
  - **Example Configurations**:
    ```json
    // Single table with 6 concurrent tasks
    {
      "connection": [{"table": ["users"]}],
      "channel": 6
    }
    → Creates 6 tasks, all writing to users table

    // Multi-table: each table gets 6 concurrent tasks
    {
      "connection": [{"table": ["users", "orders", "products"]}],
      "channel": 6
    }
    → Creates 18 tasks total (3 tables × 6 tasks per table)
    → users: 6 concurrent tasks with independent PK detection
    → orders: 6 concurrent tasks with independent PK detection
    → products: 6 concurrent tasks with independent PK detection
    ```

- **Primary Key-Based Upsert for PostgreSQL and MySQL**:
  - Automatically queries and caches primary key columns during task initialization
  - Supports single and composite primary keys
  - Generates database-specific upsert SQL:
    - PostgreSQL: `ON CONFLICT (pk) DO UPDATE SET non_pk_cols=EXCLUDED.non_pk_cols`
    - MySQL: `ON DUPLICATE KEY UPDATE non_pk_cols=VALUES(non_pk_cols)`
  - Edge case handling:
    - No primary key: Standard INSERT
    - All-primary-key tables: DO NOTHING/IGNORE
    - Query failures: Graceful fallback to standard INSERT

- **Job 状态管理**:
  - `JobState`: CREATED, RUNNING, SUCCEEDED, FAILED, CANCELLED
  - `JobStatus`: 包含 ID, State, StartTime, EndTime, Progress, Stats, Error
  - `ProgressInfo`: 实时进度信息（记录数、字节数、速度、百分比）
  - `StatsSummary`: 轻量级最终统计摘要（完成后保留）

- **Context 取消支持**:
  - 在 `schedule()` 循环中检查 context 取消信号
  - 优雅关闭：等待已启动的 task group 完成

### Benefits
- **内核灵活性**: 同一内核既可用于 CLI 阻塞模式，也可用于未来的 HTTP Service 异步模式
- **内存管理**: 任务完成后自动释放重对象，避免内存泄漏
- **Go 最佳实践**: 遵循标准库模式（`exec.Cmd`, `context.Context`, `Done()` channel）
- **可观测性**: 通过 `Job.Status()` 实时监控任务进度和指标
- **可扩展性**: 为未来的 Service 模式、gRPC 模式、消息队列模式打下基础

## [v1.4.0] - 2025-09-30

### Added
- Initial release with core functionality
- Support for multiple data sources (MySQL, PostgreSQL, Oracle, MongoDB, etc.)
- Plugin-based architecture
- Real-time statistics and progress reporting
