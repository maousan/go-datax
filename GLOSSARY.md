# go-datax

Go 实现的 DataX 兼容离线数据同步引擎，以及承载 DataX 区与 Pipeline 平台的单机 Web 管理台。

## Language

**DataSource**:
一组可复用的数据源连接定义（类型、地址、凭据），可被多个 Pipeline 或 SyncDataXJob 引用。
_Avoid_: 连接、Connection、数据源配置（指代实体时）

**Dataset（数据集）**:
可复用的读取定义，描述一个 DataSource 上的表或 SQL 查询；属于 DataX 区。
_Avoid_: QueryTemplate（代码旧名）、查询模板

**DataX 区**:
管理台中冻结保留的 DataX 功能集合：SyncDataXJob、采集任务、DataX 运行历史与数据集。
_Avoid_: 旧系统、legacy（用户可见文案）

**Pipeline**:
一条线性数据流定义：一个 Source → 0..N 个 Transform → 一个 Sink；可编辑草稿，发布后生成不可变版本。
_Avoid_: SyncDataXJob、Job（无修饰时）、流程

**Pipeline Draft（草稿）**:
Pipeline 的可编辑工作副本；发布后生成不可变 Pipeline Version。
_Avoid_: 未发布版本

**Pipeline Version（发布版本）**:
Pipeline 的一次不可变发布快照；运行与调度绑定到发布版本，而不是可变草稿。回滚靠 Schedule pin 到旧版本或从旧版本重建草稿，不支持取消发布。
_Avoid_: 版本（无修饰时）、Snapshot

**PipelineRun**:
Pipeline Version 的一次执行实例，记录状态、起止时间、退出码与所执行的版本。
_Avoid_: JobRun（指代 Pipeline 运行时）

**Schedule**:
绑定 Pipeline 的定时触发器，可多个；默认执行最新发布版本，也可固定到某个 Pipeline Version；自带 cron、时区、retry、timeout、blocking 策略。
_Avoid_: CollectTask（DataX 区的定时任务）

**CollectTask（采集任务）**:
DataX 区的定时触发器，绑定 SyncDataXJob，负责 cron、阻塞策略、重试与超时。
_Avoid_: Schedule（用于 Pipeline 区）

**Source**:
Pipeline 的读取端定义，可被多个 Pipeline 引用；发布 Pipeline Version 时快照其配置。
_Avoid_: Reader（指代 Pipeline 模型中的端点时）

**Transform**:
Pipeline 内的有序转换步骤；不做顶层复用实体。
_Avoid_: Transformer（指代单个实现时保留）

**Sink**:
Pipeline 的写入端定义，可被多个 Pipeline 引用；发布 Pipeline Version 时快照其配置。
_Avoid_: Writer（指代 Pipeline 模型中的端点时）

**SyncDataXJob**:
冻结保留的 DataX 兼容任务定义，保存完整 DataX JSON；只维护缺陷，不再扩展能力。
_Avoid_: SyncJob（旧称）、同步任务（无修饰时）

**Form mode（表单模式，legacy）**:
旧 SyncJob 的编写方式，以 DataSource 引用加结构化参数描述两端；新任务应使用 Pipeline。
_Avoid_: 简单模式

**Raw mode（原始模式）**:
SyncDataXJob 的编写方式，直接保存完整 DataX JSON，自包含且不引用 DataSource；作为 100% 兼容兜底。
_Avoid_: 高级模式、JSON 模式

**JobRun**:
SyncDataXJob 的一次执行实例，记录状态、起止时间与退出码，是 DataX 区运行历史的单位。
_Avoid_: 与 PipelineRun 混用

**RunLog**:
属于一次 JobRun 或 PipelineRun 的日志输出。
_Avoid_: 任务日志、Output

**Run Snapshot（运行快照）**:
一次运行启动时固化的完整执行配置，用于复现该次运行与排障。
_Avoid_: 配置备份、Resolved Config

**AdminUser**:
管理台唯一的管理账号，凭据与令牌签名密钥来自服务端配置文件。
_Avoid_: 用户、User、账号（指代该实体时）

**Task**:
仅指 DataX 引擎内部的执行单元（分片、任务组内的读写任务）。
_Avoid_: 在管理台语境中用 Task 指代 SyncDataXJob、Pipeline 或 JobRun
