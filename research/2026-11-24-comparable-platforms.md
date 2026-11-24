# 同类数据集成平台调研：Pipeline 模型、执行模型与连接器抽象

- 日期：2026-11-24
- 仓库：`maousan/go-datax`
- 关联议题：[issue #4](https://github.com/maousan/go-datax/issues/4)
- 调研对象：Apache SeaTunnel 3.1.0、Airbyte Platform 2.0.0 / Airbyte Protocol v0.19.0、Singer 0.3.0（含 Meltano）、Redpanda Connect 4.119.0（原 Benthos）、Kestra 2.0.11、Dagster 1.13.31
- 方法：只使用一手来源（官方文档、协议/Spec、源码）。文档引用给 URL；源码引用给文件路径与行号。所有事实性判断均在句末标注引用编号，引用清单见文末。
- 版本时点：以上版本为 2026-11-24 时各项目的最新发布或官方文档版本（SeaTunnel 3.1.0 与 docs 3.1.0 对齐；Benthos 已更名 Redpanda Connect，docs 标称 4.119.0）。

## 0. 读法

本文先逐平台回答五个问题：

1. Pipeline/Job 的领域模型与版本化
2. 单机执行模型（进程、worker、thread/goroutine、内存）
3. Source/Sink 接口与连接器复用
4. 转换层的位置与表达力
5. 对 go-datax 的直接取舍

第 7 节做横向对比，第 8 节“结论”把每条发现映射到 go-datax 的具体决策点（对应 wayfinder 议题：Pipeline 模型、执行模型、Source/Sink、Transform、DataX 兼容）。

## 1. Apache SeaTunnel

### 1.1 Pipeline/Job 领域模型与版本化

SeaTunnel 的作业定义是一个声明式配置文件，固定包含四个块：`env`、`source`、`transform`、`sink`；`transform` 可选，`env` 承载 `job.mode`（BATCH/STREAMING）、`parallelism`、checkpoint 等作业级参数。配置支持 HOCON、JSON 与 SQL 三种写法 [ST1]。多源、多转换、多目标之间通过 `plugin_output` 命名中间数据集、`plugin_input` 消费上游数据集来接线，因此作业在逻辑上可以不是一条直线，而是一张图 [ST1][ST7]。

在引擎内部，这个配置被翻译为两级模型：

- `LogicalDag`：用户意图，由 `LogicalVertex`（SourceAction / TransformChainAction / SinkAction）、`LogicalEdge` 和 `JobConfig` 组成，每个 vertex 带 parallelism [ST4]。
- `PhysicalPlan`：实际执行计划，包含多个 `SubPlan`（pipeline），每个 pipeline 有独立的 `CheckpointCoordinator` 和一组 `PhysicalVertex`（TaskGroup + 分配到的 slot）[ST4][ST2]。

Job 生命周期由 `JobMaster` 管理，状态机为 `Created → Initialized → Scheduled → Running → Finished / Failed / Canceled`；Task 另有 `CREATED → INIT → WAITING_RESTORE/READY_START → STARTING → RUNNING → PREPARE_CLOSE → CLOSED` 的状态机 [ST4]。

版本化方面：SeaTunnel 的作业配置没有内建的 schema version 或 revision 字段，文档只定义上述四块结构与变量替换 [ST1]；作业配置只写 connector 名称、不写版本；兼容性以发行版为单位管理，官方维护一份逐版本的 `Incompatible Changes` 文档，要求升级前检查 [ST11]。

对 go-datax 的直接含义：SeaTunnel 把“用户定义（LogicalDag）”与“执行计划（PhysicalPlan/SubPlan）”分开，这是支持多引擎与失败恢复的根因；go-datax 目前是“DataX JSON 直接驱动 JobContainer”，如果要保留 DataX 兼容层又引入新 Pipeline 模型，值得借鉴这种“定义 → 计划 → 执行”的分层，而不是让新模型直接复用 `JobContainer` 的内部结构。

### 1.2 单机执行模型

SeaTunnel Engine（Zeta）本身是 master-worker 架构：`CoordinatorService` 管理全部 JobMaster，`JobMaster` 负责单个 job 的物理计划与 checkpoint，`ResourceManager` 按 slot 分配 CPU/heap，worker 上的 `TaskExecutionService` 执行 TaskGroup [ST4]。数据以 Split 为单位并行，每个 pipeline 独立 checkpoint，Task 可以被 fuse 成 `Source → Transform → Sink` 的 TaskGroup 在同一个线程里执行 [ST4][ST5]。

“单机”在 SeaTunnel 里对应 local mode：`seatunnel.sh --config ... -m local` 会在提交作业的进程内启动 Zeta 服务，作业完成后进程退出；每个作业一个独立进程，因此作业之间互不影响。代价是该模式不支持暂停/恢复、不支持查看任务列表、不能通过命令取消（只能杀进程），只适合对稳定性要求高的场景 [ST9]。JVM 内存通过 `$SEATUNNEL_HOME/config/jvm_client_options` 或 `-DJvmOption="-Xms2G -Xmx2G"` 控制 [ST9]。限速是 `env` 里的 `read_limit.rows_per_second` / `read_limit.bytes_per_second`，按每个线程生效，需要乘以并行度才是全局上限 [ST10]。

这与 go-datax 的 ADR 0001（每次 JobRun 启动一个 `datax -job <快照>` 子进程）几乎同构：进程隔离、作业结束进程退出、管理面不受引擎崩溃影响 [GD1]。SeaTunnel 的 local mode 还额外说明了两点可借鉴的边界：进程隔离换来了“不能暂停/恢复/列举/优雅取消”的能力缺口 [ST9]；如果 go-datax 需要这些能力，就必须由管理面补，而不是指望引擎子进程。

### 1.3 Source/Sink 接口与连接器复用

SeaTunnel 的 Source API 是 split-based 的工厂接口：

```java
public interface SeaTunnelSource<T, SplitT extends SourceSplit, StateT extends Serializable> {
    Boundedness getBoundedness();
    SourceReader<T, SplitT> createReader(SourceReader.Context readerContext) throws Exception;
    SourceSplitEnumerator<SplitT, StateT> createEnumerator(SourceSplitEnumerator.Context<SplitT> ctx);
    SourceSplitEnumerator<SplitT, StateT> restoreEnumerator(..., StateT checkpointState);
    List<CatalogTable> getProducedCatalogTables();
}
```

以上方法签名与行号见 `SeaTunnelSource.java` 第 38–99 行 [ST12]；接口语义（枚举器在 master、Reader 在 worker、Split 可重分配、split state 进 checkpoint）见官方 Source 架构文档 [ST5]。`Boundedness` 明确区分 BOUNDED（批）与 UNBOUNDED（流），这是同一套 API 同时覆盖批与流的 seam [ST5][ST12]。

Sink API 同样是工厂：必需 `createWriter`，可选 `createCommitter` 与 `createAggregatedCommitter`，配合 checkpoint 的 `prepareCommit → commit` 两阶段提交实现 exactly-once [ST6][ST13]。`SeaTunnelSink.java` 第 47–126 行定义了这四个工厂方法 [ST13]。

连接器复用靠两层：

- 引擎无关的 SeaTunnel API（Source/Sink/Transform），由 translation layer 适配到 Zeta、Flink、Spark；同一份 connector 代码可跨引擎运行 [ST2][ST3]。
- Java SPI + 每个 connector 独立 classloader，connector 以 jar 形式热插拔，依赖隔离 [ST2][ST3]。

连接器配置本身也是契约：每个插件用 `Option` 声明 key/type/default/description，用 `OptionRule` 声明 required/exclusive/bundled/conditional 以及 value constraints；`ConfigValidator` 在 job submission 时统一校验，同一份元数据还能通过 REST 暴露给 UI [ST8]。这把“配置错误”从运行期提前到提交期。

官方设计文档明确记录了取舍：engine independence 换来高复用，但无法利用引擎特有优化，且多一层 translation 开销；他们认为 I/O 占主导，translation 开销可忽略 [ST3]。

对 go-datax 的含义：go-datax 现有的 `ReaderJob.Split` + `ReaderTask.StartRead` 两级接口，结构上对应 SeaTunnel 的“Enumerator 产生 Split / Reader 消费 Split” [GD4][ST5]；`WriterJob.Split` + `WriterTask.StartWrite` 对应 SinkWriter 侧 [GD5]。两者都把分片决策与读取执行分开，但 SeaTunnel 的 `SourceSplit` 是可序列化、可重分配、可 checkpoint 的对象 [ST5][ST12]，go-datax 的 `Split` 目前只返回 `[]Configuration` [GD4]。这说明现有插件接口方向是对的，不需要为了“新模型”推翻；真正值得补的是 Split 上的状态/offset 语义（见 F4）。

### 1.4 转换层的位置与表达力

Transform 位于 Source 与 Sink 之间，可选；它操作 SeaTunnel 自己的 row 与 `CatalogTable`，不仅做字段映射，还可以增删列、改 schema、暴露 row kind/event time 等元数据，并在多表作业里做路由/合并/过滤 [ST7]。Transform 通过 `plugin_output`/`plugin_input` 形成逻辑图，而不是死板的单链 [ST7]。运行时契约是 `SeaTunnelTransform`，其两个子接口分别是 1:1 的 `SeaTunnelMapTransform.map(row)` 与 1:N 的 `SeaTunnelFlatMapTransform` [ST7][ST14][ST15]。

表达力上，官方列出的转换类别包括：字段投影/改名（FieldMapper、FieldRename、Copy）、过滤/路由（Filter、TableFilter、TableMerge）、SQL 与表达式（SQL、JsonPath、RegexExtract）、元数据与 CDC 适配（Metadata、RowKindExtractor、FilterRowKind）、可编程/AI（DynamicCompile、Python、LLM、Embedding）[ST7]。也就是说，SeaTunnel 的 transform 是一个 schema-aware 的插件层，而不是 DataX 那种按列下标执行的固定函数表。

对 go-datax 的含义：把 Transform 放在引擎内、Source 与 Sink 之间的同一条 Task 流水线里，是主流做法，也避免 Meltano 式“每级一个进程”的序列化成本（见 3.4）。但 SeaTunnel 的 schema-aware transform 依赖 `CatalogTable` 类型系统；go-datax 当前 `Record` 是动态列数组、没有 schema 抽象 [GD13]，直接照搬会引入一整套类型系统。MVP 阶段更合理的是保留 DataX `transformer` 的列级兼容层，另加 record-level 的批式 transform（见 F6）。

## 2. Airbyte

### 2.1 Pipeline/Job 领域模型与版本化

Airbyte 的领域模型是 actor + connection，而不是 pipeline DAG：

- Actor：Source 或 Destination，由 `spec()`、`check()` 以及 Source 的 `discover()`/`read()`、Destination 的 `write()` 定义 [AB1]。
- 数据描述原语：Stream（schema + 元数据）、Field、Catalog（Stream 列表）、ConfiguredCatalog（每个 Stream 怎么同步）[AB1]。
- Connection：`sourceId` + `destinationId` + `syncCatalog` + `schedule`/`scheduleType`/`scheduleData` + `status` 等；`sourceCatalogId`/`destinationCatalogId` 引用一次 discover 得到的 catalog [AB11]。

连接器版本由 `ActorDefinitionVersion` 承载：`dockerRepository`、`dockerImageTag`、`spec`、`protocolVersion`、`documentationUrl`、`supportLevel` 等字段 [AB10]。Airbyte 明确把“协议版本”和“平台版本”独立版本化：Protocol 用 SemVer，MAJOR 做不兼容变更并承诺在新 MAJOR 下通过翻译层让旧 connector 继续工作；升级平台前 `airbyte-bootloader` 会检查所有活跃 connection 的 connector 是否仍被支持，不通过则中止升级 [AB2]。升级 connector 时也会校验 Protocol Version 是否受支持 [AB2]。

因此 Airbyte 的“版本化”是三层：connector 镜像 tag、Protocol SemVer、connection 里 pin 住的 catalog id；Connection 定义本身没有 revision 字段，运行历史由 job/attempt 记录承担 [AB4][AB11]。

对 go-datax 的含义：Airbyte 把“兼容契约”显式版本化（Protocol SemVer）并做升级前校验，这对 DataX 兼容层是直接可借鉴的模式——DataX JSON 兼容性应有明确的 compat version 与导入时校验，而不是靠“尽量兼容”的默契。另一方面，Airbyte 的 Connection 模型没有 Transform 步骤 [AB1][AB11]，说明“连接定义”和“转换”可以是两个独立关注点；go-datax 的 SyncJob 目前把两端与 transformer 放在同一份 JSON 里，新 Pipeline 模型可以保留这个整体性，但内部应把 transform 作为独立 stage 对待。

### 2.2 单机执行模型

Airbyte 平台是一组微服务：`airbyte-server`、`airbyte-db`、`airbyte-temporal`、`airbyte-worker`、`airbyte-workload-api-server`、`airbyte-workload-launcher` 等 [AB3]。同步作业以 workload 为单位：一个 workload 是一个 pod，封装一次 connector 调用（READ / WRITE / CHECK / DISCOVER / SPEC）；同步 pod 同时启动 source 与 destination 两个 connector 容器，并配一个 middleware 容器（legacy 模式叫 container orchestrator，socket 模式叫 Bookkeeper）[AB4]。

数据传输有两种模式：legacy 模式所有 record 经 orchestrator 走 STDIN/STDOUT JSON；socket 模式 record 直接经 Unix domain socket 从 source 到 destination，Bookkeeper 只处理日志/状态/统计，官方称有 4–10 倍性能提升 [AB1][AB4]。Bookkeeper 的资源参考是 1 CPU / 1024Mi [AB4]。在实现上，orchestrator 侧用 `AirbyteSource`/`AirbyteDestination` 抽象 connector 的输入输出，`ReplicationWorker` 的 `runReplicationBlocking` 驱动整次复制 [AB12]；容器实现 `LocalContainerAirbyteSource` 用 `LineGobbler` 收集 stderr 日志，并用 buffered reader 逐行解析 stdout 的 `AirbyteMessage` [AB13]。也就是说，Airbyte 的单机部署并不是“一个进程”，而是“一个 kind Kubernetes 集群”：`abctl local install` 用 kind 在 Docker 里创建一个 K8s 集群，再用 Helm 安装 Airbyte [AB5]。

对 go-datax 的含义：Airbyte 是“单机也要跑一套 K8s + 多微服务”的反例，直接说明它不适合作为 go-datax 单机优先的模板。但它的两点仍然成立：connector 作为独立进程/容器，崩溃隔离；record 不经过中心进程（socket 模式）可以显著降低序列化开销 [AB1][AB4]。go-datax 若坚持单进程内跑 connector，需要自己在引擎层保证单 connector 的 panic/内存失控不拖垮整个 job。

### 2.3 Source/Sink 接口与连接器复用

Airbyte Protocol 的方法集合很小：

- 共同：`spec() -> ConnectorSpecification`、`check(Config) -> AirbyteConnectionStatus`
- Source：`discover(Config) -> AirbyteCatalog`、`read(Config, ConfiguredAirbyteCatalog, State) -> Stream<AirbyteRecordMessage | AirbyteStateMessage>`
- Destination：`write(Config, AirbyteCatalog, Stream<AirbyteMessage>) -> Stream<AirbyteStateMessage>`

以上见官方 Protocol 文档的 Actor Interface 一节 [AB1]。所有输出都包在 `AirbyteMessage` 信封里，`type` 枚举包括 `RECORD`、`STATE`、`LOG`、`SPEC`、`CONNECTION_STATUS`、`CATALOG`、`TRACE`、`CONTROL`、`DESTINATION_CATALOG`；每条消息一行 JSON，STDERR 只放日志 [AB1][AB9]。`AirbyteRecordMessage` 必填 `stream`、`data`、`emitted_at`，`AirbyteStateMessage` 支持 GLOBAL/STREAM/LEGACY 三种状态类型 [AB9]。

Python CDK 把这个协议落成类：`AbstractSource` 提供 `check_connection`、`discover`、`read` 的抽象方法，`read` 的签名是 `(logger, config, catalog, state) -> Iterator[AirbyteMessage]` [AB14]；`Destination.write(config, configured_catalog, input_messages)` 从 stdin 逐行解析 `AirbyteMessage` [AB15]。

连接器复用靠“协议 + 容器”：connector 是 Docker image，因此可以用任何语言实现 [AB3]；Python CDK 与 low-code YAML manifest 让 REST API 类 connector 只需声明 stream、auth、pagination 等 [AB8]。协议本身还明确承认受 Singer 启发 [AB1]。

对 go-datax 的含义：Airbyte 用“进程边界 + 序列化协议”换来了语言自由与强隔离，代价是每个 connector 都要打包镜像、每次调用都有进程/容器启动成本，且平台侧需要 orchestrator/Bookkeeper 这样的中间件。go-datax 是单二进制、Go 插件、DataX 兼容，走 in-process 是合理选择；但要保留“未来可外置 connector”的可能性，接口就应该像 Airbyte Protocol 一样只依赖可序列化的数据（record batch、schema、state），而不是 Go 内部对象图。这一点与 F5 的建议一致。

### 2.4 转换层的位置与表达力

Airbyte Protocol 本身没有 transform 方法或 transform 消息类型：Source 只发 RECORD/STATE，Destination 只负责写 [AB1][AB9]。转换发生在两个协议之外的地方：

1. Connection 级 Mappings：按 stream 配置 hash（MD5/SHA-256/SHA-512）、RSA 加密、字段改名、按 string/number 过滤行 [AB6]。官方明确列出限制：不要 map cursor/primary key；对已有连接加 mapping 需要 refresh/clear，否则历史数据不会重写 [AB6]。
2. Destination 内的 Typing & Deduping（Destinations V2）：先落 raw 表（`airbyte_internal` schema），再在目标仓库里做类型转换、去重、生成 final 表；类型错误写入 `_airbyte_meta.changes` 而不是 fail sync，正在被 Direct Load 取代 [AB7]。

这是一种 ELT 取向：转换尽量下推到目标仓库的 SQL/引擎，Airbyte 只做轻量、与合规相关的行/字段级 mapping。

对 go-datax 的含义：go-datax 是 ETL（DataX 兼容），Transform 必须在写入前完成，不能照搬 Airbyte 的“仓内转换”。但 Airbyte 的 Mappings 给出了一个有用的边界：hash/encrypt/rename/filter 这类“每行独立、无 schema 变更”的变换可以做成轻量内置算子；SQL/join/聚合则应明确留给可选的 SQL transform 插件或下游，而不是塞进核心。这与 issue #5 的“内嵌 SQL 只作可选插件”结论一致（见 [issue #5](https://github.com/maousan/go-datax/issues/5) 的 Decisions）。

## 3. Singer（含 Meltano）

### 3.1 Pipeline/Job 领域模型与版本化

Singer 没有平台级的 Job/Connection 实体，Spec 只定义一个 Tap 的命令行契约：

```
tap --config CONFIG [--state STATE] [--catalog CATALOG]
```

`CONFIG` 是必需的 JSON 配置，`STATE` 是可选的断点文件，`CATALOG` 是可选的 stream 选择文件 [SG1]。Tap 把 `RECORD`、`SCHEMA`、`STATE` 消息逐行写到 stdout，日志写 stderr，成功退出码 0、失败非 0 [SG2][SG3]。`STATE` 的语义完全由 Tap 自己定义，Spec 不规定其结构 [SG1][SG3]。

版本化：Spec 只要求 Tap 对自己“输入/输出/state 解释/schema”遵循 SemVer，破坏性变更升 MAJOR [SG4]。没有 pipeline 定义版本、没有 run 历史、没有中心化 connector registry。`singer-python` 里有一个实验性的 `ACTIVATE_VERSION` 消息，用于 Target 在收到某个 version 的全部 RECORD 后删除旧数据并切换，但文档明确说大多数 Tap/Target 不需要用它 [SG7]。

对 go-datax 的含义：Singer 是“最小契约”的极端：config + state + catalog 三个文件就能跑，没有中心化元数据。它的优点是极其容易实现与调试，缺点是运行不可观测、不可重试、不可管理——这正是 go-datax 管理台存在的价值。go-datax 的 `SyncJob` + `JobRun` + `Run Snapshot` 已经比 Singer 多了定义与运行记录 [GD2][GD3]；需要补的是定义级 revision（见 F2），而不是退回文件契约。

### 3.2 单机执行模型

Singer 的标准运行方式是两个独立进程加 Unix pipe：

```bash
tap --config tap_config.json --catalog catalog.json \
  | target --config target_config.json >> state.json
tail -1 state.json > state.json.tmp && mv state.json.tmp state.json
```

以上命令来自官方 RUNNING_AND_DEVELOPING 文档 [SG5]。Tap 与 Target 通常还跑在各自独立的 virtualenv 里以避免依赖冲突 [SG6]。因此：

- 执行单元是 OS process；没有 worker、没有调度器、没有重试 [SG5]。
- 内存边界主要是 OS pipe 缓冲区；Tap 必须边读边写，Target 必须边读边写，任何一方阻塞都会形成 backpressure [SG5]。
- 状态交接靠文件：target 的 stdout 被追加到 `state.json`，运行结束后取最后一行作为下一次的 state [SG5]。

对 go-datax 的含义：Singer 证明“source 进程 | sink 进程”是最低成本的隔离方式，但它把编排、重试、监控全部推给外部（Airflow/Meltano）。go-datax 的 ADR 0001 已经在做进程隔离，管理台负责生命周期 [GD1]；不要为了“简单”退回 Singer 式无管理面。

### 3.3 Source/Sink 接口与连接器复用

Singer 的“接口”就是消息格式：

- `SCHEMA`：`stream`、`schema`（JSON Schema）、`key_properties`（必填，可为空数组）、`bookmark_properties`（可选）[SG3]。
- `RECORD`：`stream`、`record`、可选 `time_extracted` [SG3]。
- `STATE`：`value`，语义由 Tap 决定 [SG3]。

Target 侧没有正式的 Spec 接口，只约定从 stdin 读这些消息、把 STATE 写回 stdout 或文件。`singer-python` 提供 `RecordMessage`/`SchemaMessage`/`StateMessage`/`ActivateVersionMessage` 等类与 `write_record`/`write_schema`/`write_state` 辅助函数 [SG7]。Spec 允许 Tap 用任何语言实现 [SG1]；生态上 tap/target 以独立包分发（Meltano 通过 `pip_url` 安装插件，并在 Meltano Hub 目录中索引）[ME2]。

对 go-datax 的含义：Singer 的消息模型（schema + record + state）比 DataX 的“列数组 + transformer”更接近“可外置 connector”的方向；如果 go-datax 未来要定义可序列化的 connector 边界，Singer 的三类消息是最小集合。但 Singer 缺少 schema 演进、类型系统、错误语义，不能直接作为生产契约。

### 3.4 转换层的位置与表达力

Singer 协议没有独立的 transform 阶段。`singer-python` 里的 `transform.py` 是 Tap 代码内部使用的库（把 record 按 schema 做类型转换、日期解析、字段选择等），不是 pipeline 中的独立层 [SG8]。

Meltano 在编排层补了 Mapper：mapper 是独立插件，插在 tap 与 target 之间，用法是 `meltano run tap-csv <MAPPER> target-jsonl`；每个 mapping 在独立的 mapper 进程实例中执行 [ME1][ME2]。Mapper 支持 stream 级 alias/filter/duplicate，property 级 rename/transform/exclude/add/flatten，但明确不支持 aggregation、join、外部 API lookup [ME1]。并且 standalone mapper 只支持 `meltano run`，不支持 `meltano el` [ME1]。

对 go-datax 的含义：Meltano 的 mapper 是“转换即独立进程”的典型：灵活、可插拔，但每一级都要 JSON 序列化/反序列化，且只能做无状态的行级变换。go-datax 的 Transform 已经在引擎进程内、且与 reader/writer 共享 channel [GD9]，性能和部署都更优；应保留 in-process transform，不要引入 per-stage 进程。

## 4. Benthos / Redpanda Connect

### 4.1 Pipeline/Job 领域模型与版本化

Benthos（现 Redpanda Connect）的配置是一个 YAML，根节包括 `input`、`buffer`、`pipeline`（含 `processors`、`threads`）、`output`，以及 `*_resources`、`logger`、`metrics`、`tracer` 等 [RC1]。它没有 Job/Connection 实体：一个进程通常就是一个 stream；运行 `streams` 模式时，一个进程可以承载多个相互隔离的 stream，每个 stream 仍由 input/buffer/pipeline/output 四段组成 [RC4]。

版本化方面没有内建机制：配置就是文件，靠外部 VCS；提供 `lint`、`echo`、`test`（配置级单元测试）与 `-w/--watcher` 热重载来降低配置错误 [RC1][RC9]。`streams` 模式的 REST API 支持按 id 创建/读取/更新/删除 stream，`POST /streams` 会用请求体整体替换集合；文档只定义 CRUD，没有 revision/历史/回滚接口 [RC5]。

对 go-datax 的含义：Benthos 的“配置即代码 + lint/test + watcher”非常适合开发者工作流，但缺少管理台需要的定义版本、运行历史、权限与审计。go-datax 已经有 DB 作为事实源与 Run Snapshot [GD2]，不应退回纯文件；可以把 lint/test 作为 Raw mode 的编辑期能力补上。

### 4.2 单机执行模型

Benthos 的核心卖点是单二进制：官方提供 cgo-disabled 的标准二进制，覆盖 Linux/macOS/Windows 的 amd64/arm64 [RC6]。一个进程通常跑一个 stream；`streams` 模式在一个进程里跑多个隔离 stream，共享 resources，metrics 带 `stream` 标签 [RC4]。

执行模型是 goroutine + 有界 in-flight：

- `pipeline.threads` 控制 pipeline 段的并行执行线程数，默认 `-1` 表示匹配逻辑 CPU 数 [RC2]。
- Input/Output 接口通过 `AckFunc` 实现 at-least-once：消息在被成功发送或有意过滤后 ack，失败则 nack；`AutoRetryNacks` 可以给不支持 nack 的 input 自动重试 [RC10]。
- 默认 `buffer: none`，即不缓冲消息，官方称这是“默认且最 resilient”的配置 [RC8]；官方介绍也强调它用 transaction-based resiliency + back pressure 在“不持久化 transit 消息”的前提下保证 at-least-once [RC3]。

对 go-datax 的含义：Benthos 是 go-datax 在“单二进制 + goroutine + 有界 in-flight + ack/backpressure”方向上最接近的参照。它证明单进程可以做到 at-least-once 而不需要外部状态存储 [RC3][RC10]；代价是没有跨进程/跨机的恢复，进程崩溃时未 ack 的 in-flight 消息只能依赖上游重投。go-datax 是批处理，失败重跑整 job 的语义更简单，但“有界 in-flight + ack”仍适用于 writer 侧的批量提交与错误重试。

### 4.3 Source/Sink 接口与连接器复用

Benthos 的公开扩展 API 在 `public/service` 包里，接口非常小：

- `Input`：`Connect(ctx)`、`Read(ctx) (*Message, AckFunc, error)`、`Close(ctx)`；`BatchInput` 对应 `ReadBatch(ctx) (MessageBatch, AckFunc, error)` [RC10]。
- `Output`：`Connect(ctx)`、`Write(ctx, *Message) error`、`Close(ctx)`；`BatchOutput` 对应 `WriteBatch(ctx, MessageBatch) error` [RC11]。
- `Processor`：`Process(ctx, *Message) ([]*Message, error)`；`BatchProcessor` 对应 `ProcessBatch(ctx, MessageBatch) ([]MessageBatch, error)` [RC12]。

组件通过 `Environment.RegisterInput` / `RegisterBatchInput` / `RegisterOutput` / `RegisterBatchOutput` / `RegisterProcessor` / `RegisterBatchProcessor` 注册，配置里按名字引用 [RC13]；注册接口位于 `public/service`，供进程内扩展使用。

Benthos 没有 schema/table 抽象：组件的输入输出是 `Message`/`MessageBatch`（payload + metadata），connector 是“按消息/批”的，而不是“按表/分片”的 [RC7][RC10]。

对 go-datax 的含义：Benthos 的 `BatchInput`/`BatchOutput` + `AckFunc` 是“批式 Source/Sink”最简洁的契约，和 issue #5 的“row batch + 对象复用”方向一致（见 [issue #5](https://github.com/maousan/go-datax/issues/5) 的 Decisions）。go-datax 可以在现有 `ReaderTask.StartRead(RecordSender)` 之上增加 batch 版本（`ReadBatch`/`WriteBatch`），并让 ack 语义对应“writer flush/commit 成功”。同时要保留 schema/table 元数据，因为 DataX 插件需要列定义，这一点 Benthos 不能照搬。

### 4.4 转换层的位置与表达力

Benthos 的 transform 就是 `pipeline.processors` 列表，处理器按顺序作用于每条消息/每个 batch，且与具体 input/output 解耦；`pipeline.threads` 让 CPU 密集型处理器并行执行 [RC2]。

表达力核心是 Bloblang：一种声明式 mapping 语言，支持字段赋值/删除、变量、metadata、coalesce、类型转换、JSON 解析等；既可以作为 `mapping` processor，也可以在字符串插值里使用 [RC7]。官方把整体模型描述为“simple, chained, stateless processing steps” [RC3]——即默认无状态、无 join、无聚合；需要窗口处理时可使用 windowed processing [RC14]。

对 go-datax 的含义：Bloblang 证明“给数据集成工具配一门安全的表达式语言”是可行的，表达力远高于 DataX 的 `dx_substr`/`dx_pad`/`dx_replace`/`dx_filter`/`dx_digest` 固定函数集 [GD11]。但引入一门语言是长期承诺（解析器、类型、错误、IDE、文档）。MVP 可以先做“DataX transformer 兼容 + 少量内置表达式”，把通用表达式语言作为后续可选插件，而不是一次性自研 Bloblang。

## 5. Kestra（编排层）

### 5.1 领域模型与版本化

Kestra 的顶层实体是 Flow：必须包含 `id`、`namespace`、`tasks`，可选 `inputs`、`outputs`、`variables`、`triggers`、`errors`、`finally`、`retries`、`sla`、`concurrency`、`disabled`、`checks` 等；任务是 `type: io.kestra.plugin...` 的插件调用 [KE1]。

版本化是 Kestra 的产品级特性：每次修改 Flow 都会自动创建新 revision，官方称“similar to version control”，可以在 Revisions 标签页查看；还支持 Save as draft，draft 不会被执行，触发器/手动运行会回退到最后一个 published revision，直到发布 draft；`{{ flow.revision }}` 可在表达式中引用当前 revision [KE1]。

对 go-datax 的含义：Kestra 是“定义级版本化”的最直接样本，且是自动 revision（用户不用手动打 tag）。go-datax 目前只有运行级快照（ADR 0002）[GD2]；如果 Pipeline 模型需要回答“这次运行为什么用这份定义”，最小成本方案就是每次保存 SyncJob/Pipeline 时递增 revision，并让 JobRun 绑定 revision。Draft/Published 工作流对单机管理台可能过重，可以后续再加。

### 5.2 执行模型

Kestra 在 standalone 部署下把六种 server role（Executor、Worker Controller、Worker、Scheduler、Webserver、Indexer）作为线程跑在同一个进程里；分布式部署时每个 role 是独立进程 [KE2]。所有 role 通过 queue 通信（默认 JDBC，企业版支持 Kafka/Redis/AMQP），repository 存 flows/executions/logs/triggers/secrets；Worker 不直接访问 queue/DB，而是通过 gRPC 连 Worker Controller [KE2]。

一次执行的过程：Flow 校验后存入 repository；trigger 或手动调用把 execution 放入 queue；Executor 驱动状态机决定下一个 task；Worker Controller 把 job 路由给 Worker；Worker 加载 task plugin、读写 internal storage、执行并回传结果；Executor 合并结果并推进状态机 [KE2]。

对 go-datax 的含义：Kestra 证明了“单机 = 所有角色作为线程跑在一个进程”的模块化单体是可行的，不必一上来就拆微服务。go-datax 目前是 `datax-server` + `datax` 两个二进制、子进程执行 [GD1]；如果未来要减少进程启动成本，Kestra 的“standalone roles as threads”是一个中间形态，但要先解决引擎崩溃隔离问题（见 F3）。

### 5.3 转换层

Kestra 是编排层，不提供 row-level transform 抽象；数据加工发生在 task plugin 内部（SQL、脚本、容器等），task 的输入/输出通过 internal storage 传递 [KE2]。连接器复用体现为 plugin `type` + worker 加载插件 [KE1][KE2]。

对 go-datax 的含义：Kestra 不适合作为 Transform 层的参考；它适合作为“调度/版本/执行历史”的参考。go-datax 本期不做通用编排/DAG，Kestra 的 revision 与 standalone 线程模型是仅有的两个可借鉴点。

## 6. Dagster（编排层）

### 6.1 领域模型与版本化

Dagster 的核心是 software-defined asset：`@dg.asset` 装饰的 Python 函数，带 `AssetKey`、上游 asset keys 和计算函数；`@multi_asset` 可从一次操作产出多个 asset [DG3]。Job 是执行与监控的单位，一次 job 执行产生一个 run [DG4]。

版本化有两个概念：`code_version`（计算 asset 的代码版本，显式字符串）与 `data_version`（asset 数据的版本，默认是 code version + 输入 data version 的 hash）；Dagster 用它们判断 materialization 是否会变化，从而跳过不必要的重算 [DG1]。

执行模型：`DefaultRunLauncher` 在同一个 node 上为每个 run 启动一个新进程；其他 launcher 可以起 K8s Job/ECS task/Docker 容器；run launcher 只负责分配计算资源，真正执行由 run worker 进程内的 executor 接管 [DG2]。

对 go-datax 的含义：Dagster 的 `code_version`/`data_version` 是“可复现性”的另一种表达：不仅记录运行配置，还记录“计算逻辑的版本”。go-datax 的 Run Snapshot 记录的是配置，不记录引擎/插件的二进制版本 [GD2]；可以在 snapshot 里补 `engine_version` 与 `connector_versions`，成本很低。Dagster 的 per-run process 也再次验证 ADR 0001 的方向 [DG2][GD1]。

## 7. 横向对比

| 维度 | SeaTunnel | Airbyte | Singer | Benthos / Redpanda Connect | Kestra | Dagster |
|---|---|---|---|---|---|---|
| 顶层实体 | Job（env/source/transform/sink 配置）[ST1] | Connection（source + destination + catalog + schedule）[AB11] | 无；tap CLI + config/state/catalog [SG1] | Stream（一个 YAML）[RC1][RC4] | Flow（id + namespace + tasks）[KE1] | Asset + Job [DG3] |
| 定义版本化 | 无内建 revision；发行版 + Incompatible Changes [ST1][ST11] | connector image tag + Protocol SemVer + pinned catalog id [AB2][AB10][AB11] | Tap API SemVer；state 自定义 [SG4] | 无；文件 + lint/test/watch [RC1][RC9] | 每次修改自动 revision；draft/published [KE1] | code_version + data_version [DG1] |
| 运行历史 | JobMaster 状态机 + 集群 IMap 历史 [ST4] | job + attempt + 重试/退避 [AB4] | 无（外部编排器负责）[SG5] | 无运行历史；`/streams` 只返回 active/uptime [RC5] | execution 记录 + repository [KE2] | run + materialization 事件 [DG1] |
| 执行单元 | TaskGroup（Source/Transform/Sink 可 fuse）[ST4] | workload pod，内含 connector 容器 + middleware [AB4] | OS process，tap 与 target 用 pipe 连接 [SG5] | goroutine；pipeline threads 默认=CPU 数 [RC2] | task；worker 执行 plugin [KE2] | run worker 进程，executor 在进程内 [DG2] |
| 单机形态 | `-m local`：作业进程内嵌 Zeta，作业结束退出 [ST9] | `abctl`：Docker 内 kind K8s 集群 [AB5] | 两个进程 + shell pipe [SG5] | 单个静态二进制 [RC6] | standalone：六个 role 作为线程 [KE2] | 每 run 一个新进程 [DG2] |
| Source/Sink 契约 | `SeaTunnelSource`/`SeaTunnelSink` 工厂 + Split/Reader/Writer/Committer [ST12][ST13] | Protocol actor 方法 + AirbyteMessage [AB1][AB9] | SCHEMA/RECORD/STATE 消息 [SG3] | `Input`/`BatchInput` + `AckFunc`，`Output`/`BatchOutput` [RC10][RC11] | 无数据面 Source/Sink；task plugin [KE1][KE2] | 无；asset 函数 [DG3] |
| 转换位置 | 引擎内 Transform 链/图，schema-aware [ST7] | 平台侧 mappings + 目标仓内 typing/dedup [AB6][AB7] | 无协议级；Meltano mapper 独立进程 [ME1] | `pipeline.processors`，Bloblang [RC2][RC7] | task 内部逻辑 [KE2] | asset 内部代码 [DG3] |
| 转换表达力 | SQL、Filter、FieldMapper、Metadata、Python 等 [ST7] | hash/encrypt/rename/filter + 目标 SQL [AB6][AB7] | stream maps（无 agg/join）[ME1] | Bloblang + 内置 processors（无 join/agg）[RC7][RC3] | 任意插件代码 [KE2] | 任意 Python [DG3] |
| 背压/内存 | 有界队列 + checkpoint + slot/heap [ST4] | 容器限额；socket 直连或 orchestrator 中转 [AB4] | OS pipe 缓冲 [SG5] | ack + max_in_flight + 默认无 buffer [RC8][RC10] | queue + worker 线程 [KE2] | run worker 进程 + executor [DG2] |

## 8. 结论：对 go-datax 的含义

以下每条发现都对应 go-datax 的一个具体决策点（Pipeline 模型、执行模型、Source/Sink、Transform、DataX 兼容、版本化）。

### F1. “线性 source → transform* → sink”是共同的最小模型，DAG 是后续扩展

Singer 只有 tap → target 的 pipe [SG5]；Benthos 就是 input → buffer → pipeline → output 的单链 [RC1]；Airbyte 的 Connection 也只有一个 source 和一个 destination [AB11]。只有 SeaTunnel 显式支持多 source/multi sink 图，并用 `plugin_output`/`plugin_input` 命名数据集 [ST1][ST7]；Kestra 的 DAG 是编排层的能力 [KE1]。

对 go-datax：MVP 应把 Pipeline 定义为线性 stage 列表（source → transform* → sink），与现有 DataX `content` 单 reader/单 writer 的形状一致（`JobContainer.init` 目前只取 `job.content[0]`）[GD14]。但如果要保留未来多源/多目标的可能性，stage 之间的连接应使用“命名输入/输出”而不是隐式“上一个 stage 的输出”，这样以后加分支不需要改模型。SeaTunnel 的 `plugin_input`/`plugin_output` 是现成的命名方案 [ST1]。不建议本期做通用 DAG，因为 map issue 已把“DAG/依赖与调度语义”列为 Not yet specified（[issue #1](https://github.com/maousan/go-datax/issues/1)），而所有单机优先的同类产品（Singer/Benthos）都不需要它。

### F2. 版本化要分三层：定义 revision、运行快照、兼容契约

- 定义 revision：Kestra 每次修改自动生成 revision，运行可引用 `flow.revision` [KE1]；Dagster 用 `code_version` 标识计算逻辑版本 [DG1]。
- 运行快照：Airbyte 用 job/attempt 记录每次执行 [AB4]；go-datax 用 Run Snapshot 固化完整 DataX JSON [GD2]。
- 兼容契约：Airbyte 把 Protocol 独立 SemVer 化，并在升级前校验活跃 connection [AB2]；SeaTunnel 用发行版级 Incompatible Changes 文档 [ST11]；Singer 只有 Tap 自身 SemVer [SG4]。

对 go-datax：Run Snapshot 已经比 Airbyte/Kestra 的运行记录更“可复现”（它保存了完整配置），这是优势 [GD2]。缺口是定义级 revision：建议 SyncJob/Pipeline 每次保存递增 `revision`，JobRun 同时记录 `pipeline_revision`、`engine_version`、`connector_versions`（后两者可来自构建信息）。这样既保留现有快照，又能回答“这次运行为什么用这份定义、这份二进制”。DataX 兼容层应单独有一个 `datax_compat_version`，在导入 JSON 时校验；这是 Airbyte Protocol SemVer 思路在兼容层上的最小应用 [AB2]。

### F3. 单机执行有三条可行路径，ADR 0001 不必急着推翻

同类产品的单机形态：

1. 进程 per run / per job：SeaTunnel local mode 在提交进程内嵌引擎、作业结束退出 [ST9]；Dagster `DefaultRunLauncher` 每 run 起一个新进程 [DG2]。
2. 单二进制常驻：Benthos 一个进程跑一个或多个 stream [RC4][RC6]。
3. 模块化单体：Kestra standalone 把六个 role 作为线程跑在一个进程 [KE2]。

go-datax 当前是第 1 种（子进程 per JobRun）[GD1]。SeaTunnel 与 Dagster 都证明这条路径在单机是成立的；它的主要代价是启动成本与“暂停/恢复/取消”能力缺口 [ST9]。考虑到 map issue 已把“重开 ADR 0001”列为执行模型议题（[issue #1](https://github.com/maousan/go-datax/issues/1)），建议的评估顺序是：先测子进程启动/退出成本与 job 平均时长（若启动占比很低，保留子进程），再把引擎入口抽象成 `Engine` 接口，使未来可以切换为常驻 worker（Benthos 式）或线程内嵌（Kestra 式），而不是现在就把管理面绑死在 `exec.Command` 上。

### F4. Source/Sink 接口应以 batch 为一等公民，并显式保留 finite/state seam

外部证据：

- Benthos 的 `BatchInput.ReadBatch` + `AckFunc`、`BatchOutput.WriteBatch` 是最简洁的批式契约 [RC10][RC11]；它默认无 buffer、靠 ack/backpressure 保证 at-least-once [RC3][RC8]。
- SeaTunnel 的 `SourceReader.pollNext(Collector)` 是批量拉取，`SourceSplit`/`SourceSplitEnumerator` 把“分片生成”和“分片消费”分开，split state 进 checkpoint [ST5][ST12]。
- Airbyte 的 socket 模式用多条 Unix socket 并行传 record，证明单条 STDIO 序列化会成为瓶颈 [AB1][AB4]。
- Singer 的 state 是 Tap 自定义的 opaque blob，最小但不可分析 [SG1][SG3]。

对 go-datax：现有 `ReaderJob.Split` + `ReaderTask.StartRead(RecordSender)` 已经是“job 级分片 + task 级读取”的两级结构 [GD4]，方向正确；但 `Channel` 是逐条 `Push/Pull` 的 Go channel [GD6][GD7]，且 buffer 按记录数而非字节数配置 [GD9]。建议：

- 在 `ReaderTask`/`WriterTask` 上增加可选 batch 接口（`ReadBatch`/`WriteBatch`），DataX 旧插件通过 adapter 逐条适配；这与 [issue #5](https://github.com/maousan/go-datax/issues/5) 的“row batch + 对象复用”一致。
- `Channel` 的容量改为“字节预算 + 最大记录数”双约束，背压按字节计算；现有 `GetByteSize()` 已能提供每条记录大小 [GD13]。
- Split 结构中预留 `offset`/`state` 字段，Source 接口预留 `Boundedness`（finite/unbounded）能力标记；本期只实现 finite，但接口不假设“读完就结束”，这就是未来流式的 seam。SeaTunnel 的 `getBoundedness()` 与 split state 是最贴近的参考 [ST5][ST12]。

### F5. 连接器复用：in-process 是 go-datax 的正确选择，但契约要可序列化

SeaTunnel 与 Benthos 都是 in-process 接口 + 编译期/SPI 注册 [ST2][ST13][RC13]；Airbyte 与 Singer 是 out-of-process 协议 [AB1][SG5]。

go-datax 是 Go 单二进制 + DataX 插件兼容，in-process 是合理选择：没有序列化成本、部署简单、性能最好。但要注意 Airbyte 的教训：一旦协议只依赖可序列化的消息（record batch、schema、state），connector 就可以外置；反之如果接口暴露 Go 对象图/内部 channel，未来很难外置。建议把新增的 Source/Sink 契约限制在“可序列化的数据 + 明确的错误/ack 语义”上，DataX 旧插件用 adapter 包在内部 [GD4][GD5]。这也是 SeaTunnel 用 `Serializer<SplitT>`/`Serializer<StateT>` 显式序列化 split/state 的原因 [ST12]。

### F6. Transform 应留在引擎内，先做“兼容 + 批式”，不要一次自研表达式语言

外部平台的位置选择：

- SeaTunnel：引擎内 transform 链，schema-aware，可 SQL/Filter/FieldMapper/Python [ST7]。
- Airbyte：协议外 mappings + 目标仓内 typing/dedup（ELT）[AB6][AB7]。
- Singer/Meltano：mapper 是独立进程，只做无状态行级变换，不支持 agg/join [ME1]。
- Benthos：pipeline processors + Bloblang，无状态、无 join/agg [RC2][RC7]。

go-datax 是 ETL，Transform 必须在写入前完成；现有 `Transformer.Evaluate(record)` 是逐条、列下标驱动、无 schema [GD10]，内置算子只有 `dx_substr`/`dx_pad`/`dx_replace`/`dx_filter`/`dx_digest` [GD11]。建议：

- 保留 DataX transformer 兼容层不变，保证 Raw mode 100% 兼容 [GD2]。
- 新增 batch transform 接口（一次处理一批 record），让 CPU 密集型变换可以并行/批量执行；这与 F4 的 batch channel 配套。
- 表达式能力优先做“安全的内置表达式/少量函数”，SQL transform 作为可选插件（与 [issue #5](https://github.com/maousan/go-datax/issues/5) 的结论一致）；不要自研完整 Bloblang。
- 不要采用 Meltano 式 per-stage 进程：序列化成本和运维复杂度都不划算 [ME1]。

### F7. 性能与内存：batch + 有界 in-flight + 字节预算是共同答案

- Benthos：`max_in_flight` + `AckFunc` + 默认 `buffer: none`，不持久化 transit 消息 [RC8][RC10][RC11]。
- SeaTunnel：split 并行 + 每线程 `read_limit.*`，TaskGroup fuse 减少网络/序列化 [ST4][ST10]。
- Airbyte：socket 模式绕过 orchestrator，官方称 4–10 倍提升 [AB1][AB4]；legacy STDIO 是瓶颈。
- Singer/Meltano：pipe/进程边界带来序列化成本 [SG5][ME1]。

对 go-datax：与 [issue #5](https://github.com/maousan/go-datax/issues/5) 的结论一致——行级 channel 不是正确的吞吐单位，应改为 row batch + 对象复用；per-row mutex/map 统计是更大热点；背压按字节预算并支持取消。外部平台的做法支持这个判断：没有一家高性能路径是“逐条经过中心进程/中心 channel”的。具体到代码，`TaskGroupContainer` 已经在 reader/writer 之间用 `TransformerChannel` 做 inline transform [GD9]，方向对；需要把 channel 的“记录数容量”改为“字节预算”，并避免每条记录过锁/过 map 统计。另外，现有 `job.setting.speed.channel` 只控制 channel 数 [GD8]，尚未见 DataX 的 `speed.byte`/`speed.record` 限速配置落地；SeaTunnel 的 `read_limit.*` 提供了可对齐的参照 [ST10]。

### F8. 管理面：JobRun/Run Snapshot 已够用，不要复制 Airbyte 的分布式栈

Airbyte 的 job/attempt/retry/backoff 状态机很完整 [AB4]，但它的代价是 Temporal + K8s + 多微服务 [AB3][AB5]；Kestra 的 execution/revision 模型完整，但需要 queue + repository + 多 role [KE2]；SeaTunnel 的 job 历史依赖集群 IMap [ST4]。

go-datax 的 `SyncJob`/`JobRun`/`RunLog`/`Run Snapshot` 已经覆盖了单机管理台的核心 [GD2][GD3]，Job 本身也有 `CREATED/RUNNING/SUCCEEDED/FAILED/CANCELLED` 状态机 [GD12]，且比 Singer/Benthos 多了可观测性。建议只补两项低成本能力：定义 revision（F2）与运行绑定的 engine/connector 版本（F2）；不要引入 attempt 状态机、分布式队列、K8s launcher。若未来任务量增大，再考虑常驻 worker + 队列（Kestra standalone 的线程模型可作为中间形态），而不是一步跳到 Airbyte 架构 [KE2][AB5]。

### F9. 对 DataX 兼容层的具体建议

SeaTunnel 的 V1→V2 迁移是最接近的先例：V1 与 V2 connector 使用不同 API 但共存，V1 进入维护模式，V2 是未来 [ST3]。Airbyte 则用 Protocol SemVer + 升级前校验来保证旧 connector 可用 [AB2]。Singer 没有兼容层，版本升级完全靠 Tap 自己 [SG4]。

对 go-datax：

- 新 Pipeline 模型与 DataX JSON 之间应是 **adapter/import-export** 关系，而不是让新模型内部复制 DataX 的 `job.content[0].reader/writer/transformer` 形状。现有 Raw mode 已能保存任意 DataX JSON [GD2]，这是兼容层的兜底。
- DataX JSON 的导入应有显式 `compat version` 与校验；导出应保证 round-trip（至少对 Raw mode）。这是 Airbyte Protocol SemVer 思路的最小落地 [AB2]。
- 旧插件（Reader/Writer）通过 adapter 实现新 Source/Sink 接口；先跑通 MySQL/PostgreSQL/文件等核心插件，再逐步迁移。SeaTunnel V1/V2 共存说明“新旧并行 + 旧接口维护模式”是可行路径 [ST3]。

### F10. 明确不建议照搬的部分

- **分布式执行/资源管理**：SeaTunnel 的 slot/Hazelcast/CoordinatorService 是为集群设计的 [ST4]；go-datax 本期不做分布式。
- **K8s/Temporal 平台**：Airbyte 的 workload/launcher/Bookkeeper 是为多租户与弹性设计的 [AB3][AB4][AB5]；单机管理台不需要。
- **DAG 编排**：Kestra 的 flow/task/trigger 模型是通用编排 [KE1]；go-datax 本期只做 Pipeline，不做调度语义。
- **每级进程的转换**：Meltano mapper 的独立进程模型 [ME1] 不适合作为 go-datax 的 Transform 实现。
- **自研完整表达式语言**：Bloblang 的表达力很强 [RC7]，但语言实现与生态成本高；先用有限内置表达式 + 可选 SQL 插件。

## 9. 证据缺口与风险

1. SeaTunnel 引用的是 3.1.0（当前最新发布），与广泛部署的 2.3.x 在 API/配置细节上可能有差异；本文只使用 3.1.0 官方 docs 与 `dev` 分支源码，未对 2.3.x 做回溯核对。
2. Airbyte 的平台代码已从 `airbytehq/airbyte` 拆到 `airbytehq/airbyte-platform`，connector 协议拆到 `airbytehq/airbyte-protocol`，CDK 拆到 `airbytehq/airbyte-python-cdk`；本文分别引用三个仓库的 `main` 分支。部分文档页面描述的是 Cloud 特性（如 queued state），本文未将其作为 OSS 事实。
3. Benthos 已更名为 Redpanda Connect；源码仍在 `redpanda-data/benthos`，文档在 `docs.redpanda.com/connect`。Redpanda Connect 的部分组件/部署能力可能与企业版许可相关，本文只引用核心配置、接口与单二进制安装文档。
4. Singer Spec 版本为 0.3.0，较老；Meltano SDK 的实现可能超出 Spec。本文对 Singer 的判断限定在 Spec 与 `singer-python` 源码。
5. Kestra/Dagster 是编排层，不是数据面连接器；本文只用它们论证“定义版本化/执行模型”的取舍，不把它们当作 Source/Sink 设计的参照。
6. 本文没有做性能基准；所有性能相关表述均来自官方文档（如 Airbyte socket 4–10x、Benthos 单二进制），不是实测。

## 引用清单

### SeaTunnel

- [ST1] Intro To Config File, SeaTunnel 3.1.0. https://seatunnel.apache.org/docs/3.1.0/introduction/concepts/config
- [ST2] Architecture Overview, SeaTunnel 3.1.0. https://seatunnel.apache.org/docs/3.1.0/architecture/overview
- [ST3] Design Philosophy, SeaTunnel 3.1.0. https://seatunnel.apache.org/docs/3.1.0/architecture/design-philosophy
- [ST4] Engine Architecture, SeaTunnel 3.1.0. https://seatunnel.apache.org/docs/3.1.0/architecture/engine/engine-architecture
- [ST5] Source Architecture, SeaTunnel 3.1.0. https://seatunnel.apache.org/docs/3.1.0/architecture/api-design/source-architecture
- [ST6] Sink Architecture, SeaTunnel 3.1.0. https://seatunnel.apache.org/docs/3.1.0/architecture/api-design/sink-architecture
- [ST7] Transform Plugin System, SeaTunnel 3.1.0. https://seatunnel.apache.org/docs/3.1.0/architecture/transform-plugin-system
- [ST8] Configuration And Option System, SeaTunnel 3.1.0. https://seatunnel.apache.org/docs/3.1.0/architecture/configuration-and-option-system
- [ST9] Run Jobs In Local Mode, SeaTunnel 3.1.0. https://seatunnel.apache.org/docs/3.1.0/engines/zeta/local-mode-deployment
- [ST10] Speed Control, SeaTunnel 3.1.0. https://seatunnel.apache.org/docs/3.1.0/introduction/configuration/speed-limit
- [ST11] Incompatible Changes, SeaTunnel 3.1.0. https://seatunnel.apache.org/docs/3.1.0/introduction/concepts/incompatible-changes
- [ST12] `seatunnel-api/src/main/java/org/apache/seatunnel/api/source/SeaTunnelSource.java` L38-L99. https://github.com/apache/seatunnel/blob/dev/seatunnel-api/src/main/java/org/apache/seatunnel/api/source/SeaTunnelSource.java#L38-L99
- [ST13] `seatunnel-api/src/main/java/org/apache/seatunnel/api/sink/SeaTunnelSink.java` L47-L126. https://github.com/apache/seatunnel/blob/dev/seatunnel-api/src/main/java/org/apache/seatunnel/api/sink/SeaTunnelSink.java#L47-L126
- [ST14] `seatunnel-api/src/main/java/org/apache/seatunnel/api/transform/SeaTunnelTransform.java` L29-L49. https://github.com/apache/seatunnel/blob/dev/seatunnel-api/src/main/java/org/apache/seatunnel/api/transform/SeaTunnelTransform.java#L29-L49
- [ST15] `seatunnel-api/src/main/java/org/apache/seatunnel/api/transform/SeaTunnelMapTransform.java` L20-L30. https://github.com/apache/seatunnel/blob/dev/seatunnel-api/src/main/java/org/apache/seatunnel/api/transform/SeaTunnelMapTransform.java#L20-L30

### Airbyte

- [AB1] Airbyte Protocol. https://docs.airbyte.com/platform/understanding-airbyte/airbyte-protocol
- [AB2] Airbyte Protocol Versioning. https://docs.airbyte.com/platform/understanding-airbyte/airbyte-protocol-versioning
- [AB3] Airbyte high-level view. https://docs.airbyte.com/platform/understanding-airbyte/high-level-view
- [AB4] Workloads & jobs. https://docs.airbyte.com/platform/understanding-airbyte/jobs
- [AB5] abctl local install. https://docs.airbyte.com/platform/deploying-airbyte/abctl
- [AB6] Mappings. https://docs.airbyte.com/platform/using-airbyte/mappings
- [AB7] Typing and deduping. https://docs.airbyte.com/platform/using-airbyte/core-concepts/typing-deduping
- [AB8] Low-code CDK overview. https://docs.airbyte.com/platform/connector-development/config-based/low-code-cdk-overview
- [AB9] `airbyte-protocol/protocol-models/src/main/resources/airbyte_protocol/v0/airbyte_protocol.yaml` L14-L63, L151-L184. https://github.com/airbytehq/airbyte-protocol/blob/main/protocol-models/src/main/resources/airbyte_protocol/v0/airbyte_protocol.yaml#L14-L63
- [AB10] `airbyte-config/config-models/src/generated/java/io/airbyte/config/ActorDefinitionVersion.java` L56-L112. https://github.com/airbytehq/airbyte-platform/blob/main/airbyte-config/config-models/src/generated/java/io/airbyte/config/ActorDefinitionVersion.java#L56-L112
- [AB11] `airbyte-api/server-api/src/main/openapi/config.yaml` L17253-L17320 (ConnectionRead). https://github.com/airbytehq/airbyte-platform/blob/main/airbyte-api/server-api/src/main/openapi/config.yaml#L17253-L17320
- [AB12] `airbyte-container-orchestrator/src/main/kotlin/io/airbyte/container/orchestrator/worker/ReplicationWorker.kt` L36-L94. https://github.com/airbytehq/airbyte-platform/blob/main/airbyte-container-orchestrator/src/main/kotlin/io/airbyte/container/orchestrator/worker/ReplicationWorker.kt#L36-L94
- [AB13] `.../worker/io/LocalContainerAirbyteSource.kt` L33-L104. https://github.com/airbytehq/airbyte-platform/blob/main/airbyte-container-orchestrator/src/main/kotlin/io/airbyte/container/orchestrator/worker/io/LocalContainerAirbyteSource.kt#L33-L104
- [AB14] `airbyte_cdk/sources/abstract_source.py` L53-L140. https://github.com/airbytehq/airbyte-python-cdk/blob/main/airbyte_cdk/sources/abstract_source.py#L53-L140
- [AB15] `airbyte_cdk/destinations/destination.py` L30-L70. https://github.com/airbytehq/airbyte-python-cdk/blob/main/airbyte_cdk/destinations/destination.py#L30-L70

### Singer / Meltano

- [SG1] Singer Specification v0.3.0, synopsis/input/state. https://github.com/singer-io/getting-started/blob/master/docs/SPEC.md#L17-L67
- [SG2] Singer Specification, output framing. https://github.com/singer-io/getting-started/blob/master/docs/SPEC.md#L94-L104
- [SG3] Singer Specification, RECORD/SCHEMA/STATE. https://github.com/singer-io/getting-started/blob/master/docs/SPEC.md#L106-L204
- [SG4] Singer Specification, versioning. https://github.com/singer-io/getting-started/blob/master/docs/SPEC.md#L216-L223
- [SG5] RUNNING_AND_DEVELOPING, tap | target pipeline. https://github.com/singer-io/getting-started/blob/master/docs/RUNNING_AND_DEVELOPING.md#L47-L51
- [SG6] Singer FAQ, separate virtualenvs. https://github.com/singer-io/getting-started/blob/master/docs/FAQ.md#L3-L4
- [SG7] `singer/messages.py` L116-L168 (StateMessage, ActivateVersionMessage). https://github.com/singer-io/singer-python/blob/master/singer/messages.py#L116-L168
- [SG8] `singer/transform.py` L1-L50. https://github.com/singer-io/singer-python/blob/master/singer/transform.py#L1-L50
- [ME1] Meltano Mappers guide. https://docs.meltano.com/guide/mappers
- [ME2] Meltano Plugins concepts (mappers section). https://docs.meltano.com/concepts/plugins

### Benthos / Redpanda Connect

- [RC1] Configuration. https://docs.redpanda.com/connect/configuration/about/
- [RC2] Processing Pipelines. https://docs.redpanda.com/connect/configuration/processing_pipelines/
- [RC3] Introduction to Redpanda Connect. https://docs.redpanda.com/connect/get-started/about/
- [RC4] Streams Mode. https://docs.redpanda.com/connect/guides/streams_mode/about/
- [RC5] Streams API. https://docs.redpanda.com/connect/guides/streams_mode/streams_api
- [RC6] Install from Prebuilt Binary. https://docs.redpanda.com/connect/install/prebuilt-binary/
- [RC7] Bloblang. https://docs.redpanda.com/connect/guides/bloblang/about/
- [RC8] Buffer `none`. https://docs.redpanda.com/connect/components/buffers/none/
- [RC9] Unit Testing. https://docs.redpanda.com/connect/configuration/unit_testing/
- [RC10] `public/service/input.go` L20-L96 (AckFunc, Input, BatchInput). https://github.com/redpanda-data/benthos/blob/main/public/service/input.go#L20-L96
- [RC11] `public/service/output.go` L25-L72 (Output, BatchOutput). https://github.com/redpanda-data/benthos/blob/main/public/service/output.go#L25-L72
- [RC12] `public/service/processor.go` L15-L60 (Processor, BatchProcessor). https://github.com/redpanda-data/benthos/blob/main/public/service/processor.go#L15-L60
- [RC13] `public/service/environment.go` L255-L472 (RegisterInput/RegisterBatchInput/RegisterOutput/RegisterBatchOutput/RegisterProcessor/RegisterBatchProcessor). https://github.com/redpanda-data/benthos/blob/main/public/service/environment.go#L255-L472
- [RC14] Windowed Processing. https://docs.redpanda.com/connect/configuration/windowed_processing/

### Kestra / Dagster

- [KE1] Flow, Kestra docs. https://kestra.io/docs/workflow-components/flow
- [KE2] Architecture, Kestra docs. https://kestra.io/docs/architecture
- [DG1] Asset versioning and caching, Dagster docs. https://dagster.io/docs/guides/build/assets/asset-versioning-and-caching
- [DG2] Run launchers (Dagster OSS). https://dagster.io/docs/deployment/execution/run-launchers
- [DG3] Defining assets, Dagster docs. https://dagster.io/docs/guides/build/assets/defining-assets
- [DG4] Jobs, Dagster docs. https://dagster.io/docs/guides/build/jobs

### go-datax

- [GD1] ADR 0001: 同步任务由服务端以子进程方式执行. https://github.com/maousan/go-datax/blob/research/comparable-platforms/docs/adr/0001-subprocess-execution.md
- [GD2] ADR 0002: 管理配置以数据库为事实源，并为每次运行保存快照. https://github.com/maousan/go-datax/blob/research/comparable-platforms/docs/adr/0002-db-as-source-of-truth-with-run-snapshots.md
- [GD3] GLOSSARY.md. https://github.com/maousan/go-datax/blob/research/comparable-platforms/GLOSSARY.md
- [GD4] `common/plugin/reader.go` L17-L35. https://github.com/maousan/go-datax/blob/research/comparable-platforms/common/plugin/reader.go#L17-L35
- [GD5] `common/plugin/writer.go` L17-L35. https://github.com/maousan/go-datax/blob/research/comparable-platforms/common/plugin/writer.go#L17-L35
- [GD6] `common/plugin/channel.go` L8-L19. https://github.com/maousan/go-datax/blob/research/comparable-platforms/common/plugin/channel.go#L8-L19
- [GD7] `core/registry/channel.go` L26-L34. https://github.com/maousan/go-datax/blob/research/comparable-platforms/core/registry/channel.go#L26-L34
- [GD8] `core/job/jobcontainer.go` L165-L168 (job.setting.speed.channel). https://github.com/maousan/go-datax/blob/research/comparable-platforms/core/job/jobcontainer.go#L165-L168
- [GD9] `core/task/taskgroupcontainer.go` L55-L76 (buffer size, TransformerChannel). https://github.com/maousan/go-datax/blob/research/comparable-platforms/core/task/taskgroupcontainer.go#L55-L76
- [GD10] `common/transformer/transformer.go` L19-L30. https://github.com/maousan/go-datax/blob/research/comparable-platforms/common/transformer/transformer.go#L19-L30
- [GD11] `common/transformer/builtin.go` L16-L20. https://github.com/maousan/go-datax/blob/research/comparable-platforms/common/transformer/builtin.go#L16-L20
- [GD12] `core/job/job.go` L15-L23 (JobState). https://github.com/maousan/go-datax/blob/research/comparable-platforms/core/job/job.go#L15-L23
- [GD13] `common/element/record.go` L9-L14 (Record interface, GetByteSize). https://github.com/maousan/go-datax/blob/research/comparable-platforms/common/element/record.go#L9-L14
- [GD14] `core/job/jobcontainer.go` L141-L152 (`job.content[0]` 单 content). https://github.com/maousan/go-datax/blob/research/comparable-platforms/core/job/jobcontainer.go#L141-L152

### 版本核对

- SeaTunnel latest release 3.1.0: https://github.com/apache/seatunnel/releases/tag/3.1.0
- Airbyte Platform latest release v2.0.0: https://github.com/airbytehq/airbyte-platform/releases/tag/v2.0.0
- Redpanda Connect latest release v4.119.0: https://github.com/redpanda-data/connect/releases/tag/v4.119.0
- Kestra latest release v2.0.11: https://github.com/kestra-io/kestra/releases/tag/v2.0.11
- Dagster latest release 1.13.31: https://github.com/dagster-io/dagster/releases/tag/1.13.31
- Singer Specification v0.3.0: https://github.com/singer-io/getting-started/blob/master/docs/SPEC.md
