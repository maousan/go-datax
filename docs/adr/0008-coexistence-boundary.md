# DataX 区与 Pipeline 区共享基础设施，API 以 /datax 前缀分区

DataX 区与新的 Pipeline 平台并列在同一个 `etl-server`、SQLite 与 web-admin 内，但领域实体与运行历史分开：DataX 区是 SyncDataXJob、Dataset、CollectTask、JobRun；Pipeline 区是 Pipeline、PipelineVersion、Source、Sink、Schedule、PipelineRun。两边共享 DataSource、告警、监控、认证与概览。DataSource 是 connector-first 的共享连接定义：`Type` + `Properties`（connector 专用配置）+ 可选 `JDBCUrl`；创建时先选类型，字段由 connector 决定；JDBC 类型可以由 host/port/db/user 组合出 JDBC URL（可覆盖），非 JDBC 类型把 basePath/bucket/endpoint 等放进 `Properties`，现有 JDBC 行保持兼容。文件 connector 的 DataSource 表示文件系统连接（本地目录、S3 bucket 等），Source 选择相对路径/文件与 format；不为每个文件建 DataSource。DataSource 被 SyncDataXJob、Dataset 或 Pipeline Source/Sink 任一引用时禁止删除，错误按区列出引用。告警渠道共享，AlertLog 增加 `source_kind` 与 Pipeline/PipelineRun 字段，payload 带来源；`etl-worker` 暴露进程/协程/内存/GC 指标，`etl-server` 聚合自己与 worker，监控页同时展示两边。

API 边界：DataX 专属路径移到 `/api/v1/datax/*`（`/datax/sync-jobs`、`/datax/datasets`、`/datax/collect-tasks`、`/datax/runs`），共享路径保持顶层（`/data-sources`、`/alerts`、`/monitor`、`/settings`、`/overview`），新平台使用顶层新路径（`/pipelines`、`/pipeline-versions`、`/sources`、`/sinks`、`/schedules`、`/pipeline-runs`）。不保留旧别名，表名不变。DataX JSON → Pipeline 映射与旧任务迁移继续不做；DataX 区冻结，只允许菜单/命名调整与缺陷修复。

**Considered Options**: 两个区各自一套告警配置/日志（选共享渠道 + 来源字段，运维只配一次）；共享 DataSource 保持 JDBC-only、新平台另建 Connection 实体（选 connector-first 共享 DataSource，避免凭据重复且不让 JDBC URL 主导创建流程）；每个文件一个 DataSource（选文件系统根 + Source 选文件，避免一次性实体）；DataSource 软删除（选禁止删除并列出引用，避免“已删除仍可用”的模糊状态）；保留旧 API 路径（选 `/datax` 前缀，让归属写进 URL）；全部命名空间化（不做，共享路径保持顶层）。

**Consequences**: DataX API 路径是破坏性变更，前端 API 模块与测试需要同步更新；DataSource 创建流程改为 connector-first，`JDBCUrl` 对非 JDBC 类型留空，DataX 区现有表单暂保持 JDBC 形态；AlertLog 需要加可空字段并兼容旧日志；`etl-worker` 需要提供指标端点；DataSource 删除检查扩展到 Pipeline；不涉及数据库表迁移。
