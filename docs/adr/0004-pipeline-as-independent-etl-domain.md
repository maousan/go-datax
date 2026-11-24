# Pipeline 是独立于 DataX 的批处理 ETL 领域

管理台原本以 DataX 兼容的 SyncJob/JobRun 为中心。新增的 ETL 平台与 DataX 无关，因此 Pipeline 作为新的顶层实体：一条 Pipeline 是线性数据流（一个 Source → 0..N 个 Transform → 一个 Sink），Draft 可编辑，手动发布后生成不可变 Pipeline Version，运行与 Schedule 绑定版本。Source/Sink 是可复用的顶层定义，发布版本时快照其配置；Transform 内联在 Pipeline 中。原 SyncJob 冻结并改名为 SyncDataXJob，只保存完整 DataX JSON、保留现有运行与采集任务，不再扩展；Dataset、JobRun、CollectTask 留在 DataX 区。

DataX 区与 Pipeline 区并列在同一个 `datax-server`、SQLite 与 web-admin 内，共享 DataSource、告警、监控与认证，但运行与调度实体分开（JobRun/CollectTask 对 PipelineRun/Schedule）。一个 Pipeline 可挂多个 Schedule；Schedule 默认执行最新发布版本，可 pin 到某版本；版本不可变，回滚靠 pin 或从旧版本重建草稿，不支持取消发布。不做 DataX JSON → Pipeline 映射，也不迁移旧任务。

**Considered Options**: Pipeline 内 DAG（SeaTunnel 式多源多汇）vs 线性数据流 + 命名端口（选线性，DAG 后置）；Pipeline 包装 SyncJob vs 独立实体（选独立，避免 DataX 语义渗入）；无版本/自动发布 vs 草稿 + 手动发布（选后者，保证已发布行为稳定）；Source/Sink 内联 vs 顶层 + 快照（选顶层 + 快照，兼顾复用与稳定性）；复用 JobRun/CollectTask vs 独立实体 + 共享调度实现（选独立）。

**Consequences**: 过渡期有两套菜单、运行历史与调度实体；新平台的连接器与运行时是 greenfield，不复用 42 个 DataX 插件；ADR 0001 的每 Run 子进程模型只约束 DataX 区；性能目标由新平台自行定义，DataX 基线只作参考。
