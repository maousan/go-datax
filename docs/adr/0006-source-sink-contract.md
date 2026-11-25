# Source/Sink 采用 batch-native 契约，首批只做 DB 与本地文件

新平台的 Source/Sink 不复用 DataX 的 reader/writer 接口。我们决定采用 batch-native 契约：Source 在 `Open(ctx, cfg)` 后提供 `Discover(ctx)` 返回显式 Schema、`Splits(ctx)` 返回可序列化的分片、`ReadBatch(ctx, split)` 返回可复用的行批；Sink 提供 `Open`、`WriteBatch(ctx, batch)`、`Close`。Batch 是带 Schema 的行批，目标大小 256–1024 行；Source/Sink 通过 `Boundedness` 与 `SplitState` 字段为未来流式/checkpoint 留 seam，本期限定 bounded。

Schema 是显式 canonical 类型系统：int64、float64、decimal（精确字符串）、string、bytes、bool、timestamp、null；Source 声明，Sink 在发布/运行时校验，各 connector 负责物理类型映射。首批连接器是 MySQL、PostgreSQL、SQLite 与本地 CSV/TXT/JSONL，全部编译期内置到 `etl-worker` 的 registry；Source/Sink 引用共享 DataSource，配置按 connector 校验。写入模式支持 insert、truncate+insert，以及 connector 原生支持的 upsert（MySQL `ON DUPLICATE KEY`、PG `ON CONFLICT`），默认 insert。v1 错误策略 fail-fast：第一个错误终止运行，`BatchError` 带 split 与行号；不做 errorLimit/脏数据。

**Considered Options**: DataX 风格行回调（选 batch-native，兑现 ADR-0005 的批处理与背压）；Arrow 列式 RecordBatch（v1 不做，Arrow Go compute 不成熟）；动态列类型或采样推断 Schema（选显式 Schema，发布前可校验）；首批就上 Oracle/ClickHouse/对象存储（选最小 DB+文件集合）；insert-only 或 delete-by-range（选 insert/truncate/upsert，兼顾重跑与幂等）；`.so` 插件或外部 connector 进程（选编译期 registry）。

**Consequences**: v1 没有 errorLimit/脏数据与列式执行；新增 connector 需要改 `etl-worker` 并重新发布；connector 需要一套 conformance 测试（内存 Source→Sink、schema 映射、truncate/upsert、错误定位）；run spec 由 `etl-server` 序列化 Source/Sink 配置与已解析凭据，worker 只执行。
