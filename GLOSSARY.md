# go-datax

Go 实现的 DataX 兼容离线数据同步引擎，以及其配套的单机 Web 管理台。

## Language

**DataSource**:
一组可复用的数据源连接定义（类型、地址、凭据），可被多个同步任务引用。
_Avoid_: 连接、Connection、数据源配置（指代实体时）

**SyncJob**:
一条同步任务的定义：从哪里读、写到哪里、如何分片与转换；不包含执行历史。
_Avoid_: Job、任务（无修饰时）、Pipeline

**Form mode（表单模式）**:
SyncJob 的一种编写方式，以 DataSource 引用加结构化参数描述两端。
_Avoid_: 简单模式

**Raw mode（原始模式）**:
SyncJob 的一种编写方式，直接保存完整 DataX JSON，自包含且不引用 DataSource。
_Avoid_: 高级模式、JSON 模式

**JobRun**:
SyncJob 的一次执行实例，记录状态、起止时间与退出码，是运行历史的单位。
_Avoid_: Execution、Instance、运行（作名词时用 JobRun）

**RunLog**:
属于一次 JobRun 的日志输出。
_Avoid_: 任务日志、Output

**Run Snapshot（运行快照）**:
JobRun 启动时固化的完整 DataX 配置，用于复现该次运行与排障。
_Avoid_: 配置备份、Resolved Config

**AdminUser**:
管理台唯一的管理账号，凭据与令牌签名密钥来自服务端配置文件。
_Avoid_: 用户、User、账号（指代该实体时）

**Task**:
仅指 DataX 引擎内部的执行单元（分片、任务组内的读写任务）。
_Avoid_: 在管理台语境中用 Task 指代 SyncJob 或 JobRun
