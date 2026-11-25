# 同步任务由服务端以子进程方式执行

管理台需要启动、观测和取消同步任务。我们决定每次 JobRun 都启动一个独立的 `datax -job <快照>` 子进程，而不是把引擎嵌入管理服务进程内。原因是 `engine.Main` 目前是 flag 解析加 `os.Exit` 的 CLI 入口，嵌入需要重构；更重要的是进程隔离能在任务 panic 或内存失控时保住管理面，日志也可以直接按管道采集。

**Considered Options**: 嵌入引擎（少一层进程管理，但需重构入口且故障不隔离）；常驻 worker 池加队列（能力更强，但 MVP 阶段明显过度设计）。

**Consequences**: 服务端必须管理进程生命周期、取消时杀进程树、重启时把残留的 `running` 记录标记为 `failed`；部署时 `datax` 与 `datax-server`（现名 `etl-server`）两个二进制必须同时存在。
