# 新平台由独立的 etl-worker 执行，管理面为 etl-server

新 Pipeline 平台与 DataX 区无关，执行必须与管理面隔离。我们决定把管理面二进制从 `datax-server` 硬改名为 `etl-server`，由它拥有 SQLite、web-admin、DataSource/告警/认证以及 Pipeline 控制面状态（Pipeline、Pipeline Version、PipelineRun、Schedule、日志）；新平台的执行放在独立的常驻进程 `etl-worker` 中。worker 是无状态执行器：server 通过 127.0.0.1 的 HTTP/JSON 下发已解析的 run spec，worker 通过 SSE 回传日志与进度，不直接访问 SQLite。默认由 `etl-server` 启动并监管 `etl-worker`（崩溃时标记运行失败并重启），也可配置外部 worker URL；本地用 shared token，外部部署可加 TLS。

worker 内部按 split/task 启动 goroutine，使用 256–1024 行的有界 batch channel、字节预算背压和 `SendBatch(ctx)`；统计走 per-task 原子/分片计数。失败不做 checkpoint，重启后全量重跑；worker 崩溃或断连时 server 把运行标记失败，由 Schedule 的 retry 策略重试。每个 Schedule 配置 discard/overwrite/queue，同一 Pipeline 默认不并发。

**Considered Options**: 进程内 worker pool（启动最快，但 panic/OOM 会带走管理面）vs 独立常驻 worker（选后者，隔离与独立测试更好）vs 每 Run 子进程（启动成本过不了 A 档 ≤300ms）；HTTP/JSON + SSE（选）vs gRPC；共享 SQLite（引入锁竞争与耦合）vs 无状态 worker（选）；server 监管 worker（选，保留外部 URL）vs 永远外部服务。

**Consequences**: 部署从两个二进制变成 `datax`、`etl-server`、`etl-worker` 三个；`datax-server` 名字硬删除，运维脚本必须更新；JWT issuer 改为 `etl-server`，已有 token 失效一次；worker 协议需要版本握手；ADR 0001 的子进程模型继续只约束 DataX 区；A/B/C 性能目标依赖批量传输与字节预算。
