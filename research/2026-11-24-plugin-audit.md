# DataX 插件适配成本审计

> 调研日期：2026-11-24
> 关联 issue：[maousan/go-datax#6](https://github.com/maousan/go-datax/issues/6)
> 代码锚点：commit [`95a8feb`](https://github.com/maousan/go-datax/commit/95a8feb19073e8576be6c14f67e8567198a8ed93)，即 `research/plugin-audit` 分支的基线
> 审计范围：`plugins/reader/*`（20 个）与 `plugins/writer/*`（22 个），共 42 个插件、27,373 行非测试代码
> 用途：为 issue #8（Source/Sink 接口与连接器复用策略）、issue #7（批处理 Pipeline 执行模型）和 issue #10（DataX 兼容契约）提供输入

## 0. 结论摘要

1. **42 个插件中，只有 7 个可以直接适配，31 个需要重写，4 个建议放弃（不进入新平台，保留 DataX 兼容层）。** 比例约为 16.7% / 73.8% / 9.5%。可直接适配的是 SQLite 读写、PostgreSQL writer、本地 TXT/JSON writer 和 Stream 读写；需要重写的是绝大多数 JDBC、文件、NoSQL 插件；建议放弃的是 Sybase 读写与 HDFS 读写。
2. **Reader 侧没有任何共享实现，Writer 侧的共享实现只被 1 个插件使用。** `common/database/rdbms/writer` 有 1,266 行，仅 `postgresqlwriter` 真正委托给它；其余 21 个 writer 各自复制了 JDBC URL 解析、建表列查询和 insert/replace/update 逻辑。Reader 侧的分片算法在 10 个文件中重复了约 1,350 行，其中 MySQL、PostgreSQL、OceanBase、StarRocks 四份实现几乎同源，每份 250–279 行。
3. **最大的适配障碍不是协议实现，而是生命周期契约不完整。** 当前 `JobContainer` 只调用 writer job 的 `Prepare`，从不调用 reader job 的 `Prepare`，也从不调用 job 级 `Post` / `Destroy`（`core/job/jobcontainer.go:194-209`、`core/job/jobcontainer.go:321-324`）。直接后果是：FTP reader、OSS reader、HDFS reader 在 `Split` 时拿不到 `Prepare` 填充的文件/对象列表；HDFS writer 的临时文件永远不 rename 到最终路径；所有 writer 的 `postSql` 永不执行。
4. **分片正确性存在硬伤，且并发模型与 DataX 不同。** Mongo reader 的 `Split` 克隆 N 份配置，但每个 task 都执行全量 `collection.Find`，channel>1 时数据成倍重复（`plugins/reader/mongoreader/mongo_reader.go:103-115`、`:222`）；MySQL 文本分片的边界值同时被相邻 task 以 `<=` 和 `>=` 包含；Sybase、GaussDB、TDengine 的 `splitPk` 实际未生效。更严重的是，Go 引擎为每个 split task 启动一个 goroutine（`core/job/jobcontainer.go:263-308`），`channel` 只是 `Split` 的建议值，不是并发上限，文件/对象/表较多的 reader 可能同时拉起成百上千个 reader/writer 连接。
5. **类型与错误语义被静默弱化。** `element.Column` 只有 Long/Double/String/Date/Bool/Bytes 六种类型，且 `StringColumn("")` 被当作 null，空字符串与 NULL 不可区分（`common/element/column.go:126-131`）。文件 reader 在数值/日期解析失败时返回 0/0.0/false，Mongo reader 返回 0/false/当前时间，TDengine reader 吞掉查询和发送错误，Neo4j writer 跳过转换失败记录。`errorLimit` 只接入了 Transformer 脏数据，Reader/Writer 失败不计入该限制（`docs/TRANSFORMER.md:353`）。
6. **推荐的迁移方式是“按协议族重写”，不是“42 个插件逐个包 adapter”。** 首批只做 MySQL、PostgreSQL、SQLite 的 generic JDBC Source/Sink，本地 CSV/TXT 与 Stream 的 Source/Sink，共 5 对连接器；随后按方言扩展到 Oracle、SQL Server、ClickHouse、Doris、StarRocks、OceanBase、GaussDB；再进入 Mongo、Elasticsearch、Cassandra、Neo4j、TDengine 等独立协议；Sybase 与 HDFS 不进入新平台。
7. **最大的风险是“旧插件看起来兼容、实际不可用”。** 除生命周期和分片问题外，还存在插件名与 Java DataX 不一致（`mongoreader` vs `mongodbreader`、`oceanbasereader` vs `oceanbasev10reader`）、上游有 `osswriter` 而本仓库没有、OSS reader 未注册进 `cmd/datax/main.go`、HDFS reader 的 TEXT 读取是返回 0 行的 stub、PostgreSQL writer 隐式 upsert 与 DataX 的 insert-only 语义不一致等。这些都会在“先包一层”的迁移路线中变成线上事故。

---

## 1. 审计方法与分类口径

### 1.1 方法

- 静态阅读全部 42 个插件的 Job/Task 实现、共享包 `common/*`、执行引擎 `core/*`、管理面 `internal/*`，并用 `go build ./...` 验证基线可编译。
- `plugins/` 下只有 4 个测试文件、24 个测试函数、582 行测试代码（其中 3 个文件属于 42 个 reader/writer，1 个属于 transformer），对 27,373 行非测试代码几乎没有覆盖。
- 以 Java 版 DataX 的官方文档与源码作为行为基准，重点核对 `splitPk`、多表、`querySql`、`writeMode`、`errorLimit`、reader/writer task 配对和 job 生命周期。
- 未连接真实数据库做端到端对拷；本报告对“能否工作”的判断来自代码路径和生命周期调用关系，不涉及吞吐实测。

### 1.2 分类定义

| 分类 | 含义 | 迁移动作 |
|---|---|---|
| **A 可直接适配** | 核心读写逻辑可以保留 70% 以上；只需薄 adapter、补 lifecycle/context/schema 描述，不需要推翻内部结构 | 在新 Source/Sink 下包一层，纳入首批或测试连接器 |
| **B 需重写** | 协议知识有价值，但 split/schema/batch/error 与 Job/Task 配置深度耦合，或存在正确性/覆盖缺口 | 保留协议知识，按新契约重写为 generic core 的 dialect 或独立 connector |
| **C 建议放弃** | 代码覆盖严重不足、协议已边缘化或维护成本高于价值 | 不进入新平台；保留在 DataX 兼容路径，不再投入 |

> 说明：这里的“放弃”不是立即删除代码，而是“不移植到新的 Source/Sink 抽象”。在 DataX 兼容层存续期间，这些插件仍可由旧引擎运行。

---

## 2. 现有扩展点与执行模型

### 2.1 插件接口

`common/plugin` 定义了 Reader/Writer 的 Job 与 Task 双层接口（`common/plugin/reader.go:10-41`、`common/plugin/writer.go:10-41`）：

- Job：`Init`、`Split(adviceNumber)`、`Prepare`、`Post`、`Destroy`。
- Task：`Init`、`StartRead` / `StartWrite`、`Prepare`、`Post`、`Destroy`。
- Task 可选实现 `ReaderTaskWithContext` / `WriterTaskWithContext`，但**42 个插件中没有任何 reader/writer task 实现 `StartReadWithContext` 或 `StartWriteWithContext`**；只有 `common/database/rdbms/writer` 内部实现了 `StartWriteWithContext`，而它只被 PostgreSQL writer 使用。
- 数据通道是 `chan element.Record`，关闭用 sentinel error `ErrChannelClosed`；部分插件用 `err.Error() == "channel is closed"` 比较字符串（如 `plugins/writer/clickhousewriter/clickhouse_writer.go`），错误判断不一致。

### 2.2 生命周期：Go 引擎 vs Java DataX

Java DataX 的 `JobContainer` 会依次调用 reader/writer job 的 `init`、`prepare`、`split`、`post`、`destroy`，并且 writer 的 `post`/`destroy` 先于 reader。Go 引擎的实际调用如下：

| 阶段 | Go 引擎实际行为 | Java DataX 行为 | 直接后果 |
|---|---|---|---|
| job init | reader job 在 `split` 阶段才 `Init`；writer job 在 `prepare` 阶段 `Init` | reader init 先于 writer init | reader 配置错误可能在 writer `preSql`（如 DROP TABLE）之后才暴露 |
| job prepare | 只调用 `writerJob.Prepare()`（`core/job/jobcontainer.go:194-209`） | reader `prepare()` + writer `prepare()` | FTP/OSS/HDFS reader 的文件列表为空，`Split` 失败或返回空 |
| job split | `readerJob.Split(channel)`，再 `writerJob.Split(readerTaskCount)` | 相同，但 split 后把 channel 收敛到 task 数 | Go 的并发数等于 task 数，不是 channel 数 |
| task prepare | 只调用 `writerTask.Prepare()` | writer task prepare | reader task 无 prepare 阶段 |
| task post/destroy | 调用 reader/writer task 的 `Post` / `Destroy`（`core/task/taskgroupcontainer.go:207-222`） | 调用 | task 级清理可执行 |
| job post | **从不调用**，`JobContainer.post()` 为空（`core/job/jobcontainer.go:321-324`） | writer `post()` 后 reader `post()` | HDFS writer 临时文件不 rename；`postSql` 永不执行 |
| job destroy | **从不调用** | writer `destroy()` 后 reader `destroy()` | FTP 连接、job 级资源泄漏 |

这个表是本次审计最关键的发现之一。任何“把旧 plugin 包进新 Source/Sink”的方案，都必须先补上缺失的 `prepare/post/destroy`，否则会继承这些缺陷。

### 2.3 并发模型

- `JobContainer.schedule` 为每个 reader split task 启动一个 goroutine，每个 goroutine 内创建一个 `TaskGroupContainer`，一对一配对 reader/writer task（`core/job/jobcontainer.go:263-308`）。
- `needChannels` 只作为 `readerJob.Split` 的建议值（`core/job/jobcontainer.go:166`、`:220`），并未限制同时运行的 task 数。HDFS reader 按文件返回 task、OSS reader 按对象返回 task、FTP reader 按文件返回 task、Sybase reader 按表/JDBC URL 返回 task、Cassandra reader 按 token range 返回 task；这些数量都可能远大于 `channel`。
- 每个 task group 的 channel buffer 默认 10,000 行，offset split 时按 `limit/5` 放大到最多 200,000 行（`core/task/taskgroupcontainer.go:261-294`）。没有全局内存预算，task 数一多，常驻内存会线性增长。
- Java DataX 在 `schedule()` 中把 `needChannelNumber` 收敛为 `min(needChannelNumber, taskNumber)`，并用固定数量的 task group 承担所有 split task；Go 的模型是“task 即 goroutine”，两者不是同一语义。

### 2.4 数据模型与配置模型

- `element.Column` 只有 6 种类型：Null、Long、Double、String、Date、Bool、Bytes（`common/element/column.go:8-18`）。没有 Decimal、Timestamp 精度、嵌套结构、列名、可空性等 schema 信息。
- `NewStringColumn("")` 会把空字符串标记为 null（`common/element/column.go:126-131`），因此 NULL 与空串在所有 reader/writer 之间不可区分。
- `Configuration.Clone()` 通过 JSON 序列化/反序列化实现（`common/config/configuration.go:279-282`）：深拷贝正确，但大配置有成本，且数字统一变成 `float64`，插件里大量 `convertToInt`、`GetIntWithDefault` 的兼容代码由此而来。
- 42 个插件各有 `init.go`，共 1,137 行注册样板；`cmd/datax/main.go` 用 41 个 blank import 注册插件，**`ossreader` 没有被 import**，因此即使代码存在，datax 二进制里也没有 `ossreader`。

---

## 3. 42 个插件完整清单与分类

> LOC 为该插件目录下非测试 Go 代码行数。`split` 一列描述 Job 的切分依据；“单表”表示只使用 `connection[0].table[0]`，多表配置会被静默丢弃。

| # | 插件 | 家族 / 实现 | LOC | Split 与并发 | Schema / 类型 | 批处理 / 错误语义 | 分类 | 主要理由 |
|---:|---|---|---:|---|---|---|---|---|
| 1 | `mysqlreader` | JDBC / gorm+mysql | 815 | `splitPk` 数值/文本字典/offset/hash；单表；失败回退单 task | 驱动类型到 6 种 Column；解析失败静默 0/空 | 逐行读，无 fetchSize；无 context；无脏数据计数 | **B** | 分片/类型/查询内嵌，文本分片边界重复；高价值，首批重写为 generic JDBC source |
| 2 | `postgresqlreader` | JDBC / gorm+postgres | 967 | 同 MySQL 四策略，另有文本采样；单表 | 类型较全，但 NULL 变空串；无 schema 描述 | 逐行读；无 context；fetchSize 未生效 | **B** | 与 MySQL 大量同源；JDBC 参数不支持（`len(parts)!=2`）；首批 generic JDBC 方言 |
| 3 | `oraclereader` | JDBC / go-ora | 596 | 按 splitPk 的 int/float/string 生成范围；string 实际不分片；多表可循环 | 按 Oracle 类型名映射；NUMBER 精度有处理 | 逐行读；session SQL；无 context | **B** | 协议知识有价值，但绑定 Job/Task 与配置；重写为 JDBC dialect |
| 4 | `sqlserverreader` | JDBC / gorm+sqlserver | 481 | 仅数值 splitPk；单表 | `[]byte` 启发式还原数值/文本；NULL 用 NullColumn | 逐行读；fetchSize 只存不用；无 context | **B** | 与通用 JDBC 高度重合；DSN 已有 internal helper；重写为方言 |
| 5 | `sqlitereader` | JDBC / 本地文件 | 435 | 数值 splitPk；单表；逻辑简单 | 基本类型映射；NULL 变空串 | 逐行读；无 context；无事务概念 | **A** | 单文件、依赖少，可直接包成 Source；补 context/schema/tx 说明即可 |
| 6 | `gaussdbreader` | JDBC / gorm+postgres | 455 | 按表分 task；`splitPk` 只透传不生效 | PG 类型映射 | 逐行读；无 context | **B** | 本质是 PostgreSQL 协议变体；作为 PG 方言/别名重写，不做独立实现 |
| 7 | `sybasereader` | JDBC / tds | 402 | 按 JDBC URL × 表分 task；忽略 adviceNumber 与 splitPk | 类型映射较简单 | 逐行读；无 context | **C** | ASE 已边缘化，分片能力缺失；保留兼容层，不投入新平台 |
| 8 | `clickhousereader` | Native / clickhouse-go | 539 | 数值 splitPk；单表（currentTable） | ClickHouse 类型名映射；Array 转字符串 | 逐行读；fetchSize 只存不用；无 context | **B** | Native 协议与类型矩阵值得保留；重写为独立 ClickHouse source |
| 9 | `dorisreader` | MySQL 协议 / gorm | 490 | 数值 splitPk；单表 | 复制 MySQL 类型映射 | 逐行读；无 context | **B** | 本质是 MySQL 方言；复用 generic JDBC，减少一套分片代码 |
| 10 | `starrocksreader` | MySQL 协议 / gorm | 823 | 复制 MySQL 四策略；单表 | 复制 MySQL 类型映射 | 逐行读；无 context | **B** | 与 MySQL 250 行分片代码几乎重复；作为 MySQL 方言/别名 |
| 11 | `oceanbasereader` | MySQL 协议 / gorm | 1,058 | splitPk/分区两种；Oracle/MySQL 兼容模式；单表 | 两套类型列表；关键字转义 | 逐行读；无 context；模式探测有顺序问题 | **B** | 复杂度高、双模式有价值；第二批重写，必须先解决模式/标识符语义 |
| 12 | `tdenginereader` | REST / taosRestful | 559 | 按 JDBC URL/表生成 task；忽略 adviceNumber；splitPk 未用 | 按列类型建 Record | 查询错误 continue；`SendRecord` 错误被忽略 | **B** | 有真实需求但错误语义不可接受；重写或明确放弃 |
| 13 | `hdfsreader` | Native / colinmarc hdfs | 666 | 按文件分 task | 类型映射存在 | **TEXT/CSV 读取是 stub，返回 0 行**；ORC/RC/SEQ/Parquet 未实现；Prepare 未被引擎调用 | **C** | 当前不可用且覆盖严重不足；不进入新平台 |
| 14 | `ossreader` | OSS SDK | 896 | 按对象分 task；未做 range 切分；`Prepare` 未被调用导致空分片 | CSV/二进制；encoding 只存不用 | 二进制路径忽略 `SendRecord` 错误；未注册到 main | **B** | 对象存储是平台必备，但现有实现不可达；按对象存储 source 重写 |
| 15 | `ftpreader` | FTP/SFTP | 1,046 | 按文件分 task；`Prepare` 未被引擎调用，Split 必失败 | CSV 类型映射；encoding 只存不用 | 读流逐行；错误返回 | **B** | SFTP/FTP 代码可复用，但生命周期和 CSV codec 需重写 |
| 16 | `txtfilereader` | Local file | 520 | 按文件分 task；支持 `*` glob | CSV 类型映射；数值/日期失败静默 0/空 | 逐行读；gzip/bzip2；zip 不支持 | **B** | 首个文件连接器，但缺 encoding、原子性和严格类型；抽共享 file codec 重写 |
| 17 | `jsonfilereader` | Local file | 1,372 | 按文件分 task；JSONL 流式 | 独有 schema 推断；嵌套字段路径；类型推断 | JSON 数组整文件读入内存；无 context | **B** | schema inference 是资产，但需抽成独立 schema 组件；整文件内存问题需重写读取路径 |
| 18 | `cassandrareader` | Native / gocql | 648 | Random/Murmur3 token range；较完整 | Cassandra 类型到 Column；复杂类型 JSON 字符串 | 逐行读；无 context；consistency 配置 | **B** | token 分片知识可复用；重写为 Cassandra source |
| 19 | `mongoreader` | Native / mongo-driver | 403 | **克隆 N 份配置，但每 task 全量 Find，数据重复 ×N** | 缺失字段/失败类型静默 0/false/当前时间 | 游标逐行；无 context | **B** | 严重正确性缺陷；重写时必须用 `_id`/范围/分片键做真实 partition |
| 20 | `streamreader` | Test generator | 283 | 克隆 adviceNumber 份，每份生成 `sliceRecordCount` 行，与 Java 语义一致 | 列配置类型 | 无外部依赖；无 context | **A** | 测试/压测连接器，可直接适配，但不应作为生产 Source |
| 21 | `mysqlwriter` | JDBC / gorm+mysql | 603 | 克隆 N 份，所有 task 写同一表 | 通配符 `*` 查表展开 | insert/replace/update 三套 SQL；无事务；无 context | **B** | 三套 DML 逻辑与其他 writer 重复；重写为 generic JDBC sink |
| 22 | `postgresqlwriter` | 共享 RDBMS writer | 100 | 委托 `CommonRdbmsWriterJob`，支持多表 × channel | 通配符展开、PK 查询 | 批量 INSERT；**按 PK 自动 upsert**；有 context 实现 | **A** | 已是薄包装，可直接提升为 generic RDBMS sink；但必须把隐式 upsert 改成显式 writeMode |
| 23 | `oraclewriter` | JDBC / go-ora | 672 | 克隆 task；单表 | 类型转换较完整 | prepared statement + 每批事务；`REPLACE` 实际等同 INSERT | **B** | 事务/批处理可借鉴，但需统一到 JDBC sink；修正 writeMode 语义 |
| 24 | `sqlserverwriter` | JDBC / gorm+sqlserver | 477 | 克隆 task；单表 | `*` 查表展开；类型转换 | 多行 INSERT；无事务；pre/post/session | **B** | 与 Java 一样 insert-only；重写为 SQL Server 方言 |
| 25 | `sqlitewriter` | JDBC / 本地文件 | 492 | 克隆 task；单表 | `*` 查表展开 | insert/replace/update；无事务；建议 channel=1 | **A** | 本地文件、逻辑简单，可直接适配为测试 sink；补事务/并发说明 |
| 26 | `gaussdbwriter` | JDBC / gorm+postgres | 518 | 克隆 task；单表 | 复用 shared writer 的预处理类型 | 事务批量 INSERT；仅 insert | **B** | 作为 PostgreSQL 方言复用 generic sink；不保留独立 DML |
| 27 | `sybasewriter` | JDBC / tds | 478 | 克隆 task；单表 | 基本类型 | 事务批量 INSERT；`REPLACE` 退化为 INSERT | **C** | ASE 低需求，语义不完整；保留兼容层 |
| 28 | `clickhousewriter` | Native / clickhouse-go | 488 | 克隆 task；单表 | CH 类型转换 | native batch，行数+字节阈值；无 context | **B** | Native 批量协议值得保留；重写为 ClickHouse sink |
| 29 | `doriswriter` | HTTP Stream Load | 592 | 克隆 task；单表 | CSV/JSON 序列化 | 行数/字节/时间三阈值；HTTP 重试 | **B** | Stream Load 语义可复用；重写为 Doris sink，统一 batch/error 模型 |
| 30 | `starrockswriter` | HTTP Stream Load | 967 | 克隆 task；单表 | CSV/JSON；列转义 | 行数/字节/时间；label 重试；label 状态检查 TODO | **B** | 功能最完整的 Stream Load 实现之一；重写为 StarRocks sink |
| 31 | `oceanbasewriter` | MySQL 协议 / gorm | 723 | 克隆 task；单表 | Oracle/MySQL 模式关键字转义 | insert/replace/update；无事务；模式探测顺序有缺陷 | **B** | 复制 MySQL writer 且双模式复杂；重写为 OceanBase 方言 |
| 32 | `databendwriter` | JDBC / databend-go | 498 | 克隆 task；单表 | `*` 查表展开 | 每行 Prepare/Exec，所谓 batch 并不批量；无事务 | **B** | 需要真正的批量/COPY 语义；重写 |
| 33 | `tdenginewriter` | REST / taosRestful | 482 | 克隆 task；多表 | 类型转 SQL 字面量 | 多行 INSERT；失败逐行回退并吞错误 | **B** | 错误语义与 errorLimit 冲突；重写或放弃 |
| 34 | `hdfswriter` | Native / colinmarc hdfs | 728 | 按 task 生成临时/最终文件名 | 通配符展开；仅 TEXT 可用 | Parquet/ORC 未实现；**job.Post 未被调用，临时文件永不 rename** | **C** | 当前会留下不可见临时文件且格式覆盖不足；不进入新平台 |
| 35 | `ftpwriter` | FTP/SFTP | 901 | 克隆 task；按 taskId 生成文件名 | CSV 类型转换 | 流式上传；truncate/append/nonConflict；无原子 rename | **B** | 代码可复用但需与本地/对象文件 sink 共享 codec 和提交语义 |
| 36 | `txtfilewriter` | Local file | 420 | 克隆 task；taskId+时间戳命名 | CSV 格式；无 schema | gzip；truncate/append/nonConflict；无原子 rename | **A** | 逻辑简单，可直接适配；补 encoding、原子提交和错误计数 |
| 37 | `jsonfilewriter` | Local file | 569 | 克隆 task；taskId+文件序号命名 | JSON/JSONL；字段映射 | gzip；truncate；无原子 rename | **A** | 可直接适配；补原子提交、schema 校验和错误语义 |
| 38 | `elasticsearchwriter` | Native / go-elasticsearch | 1,505 | 克隆 task；单 index | 动态 mapping、id/routing/version/delete 规则 | bulk 批量；ignoreWriteError/ignoreParseError；bulk item 错误检查 | **B** | 功能完整但体量大、错误开关语义混杂；重写为 ES sink |
| 39 | `mongowriter` | Native / mongo-driver | 485 | 克隆 task；单 collection | BSON 字段转换 | insert/replace upsert；批量；无 context | **B** | 协议逻辑可复用；重写为 Mongo sink |
| 40 | `cassandrawriter` | Native / gocql | 618 | 克隆 task；单表 | Cassandra 类型转换 | UNLOGGED BATCH；批量失败逐条回退；可异步 | **B** | 批处理/回退语义可复用；重写并补 context/errorLimit |
| 41 | `neo4jwriter` | Native / neo4j-go | 633 | 克隆 task | Cypher 参数转换；数组解析 | UNWIND 批量；重试；转换失败静默跳过 | **B** | Cypher/retry 知识可复用；重写为 Neo4j sink |
| 42 | `streamwriter` | Test sink | 295 | 克隆 task；所有 task 写同一文件/stdout | 字符串化 | 1024 行 flush；task Init 会删除目标文件，存在竞态 | **A** | 测试/调试 sink，可直接适配；仅用于开发与验收 |

### 3.1 分类汇总

| 家族 | Reader 数量 | Writer 数量 | A 可直接适配 | B 需重写 | C 建议放弃 |
|---|---:|---:|---:|---:|---:|
| JDBC/SQL（含 MySQL 协议、ClickHouse native、TDengine REST） | 12 | 13 | 3（sqlite reader/writer、postgresql writer） | 20 | 2（sybase reader/writer） |
| 文件/对象（local、HDFS、OSS、FTP） | 5 | 4 | 2（txt/json writer） | 5 | 2（HDFS reader/writer） |
| NoSQL（Cassandra、Mongo、ES、Neo4j） | 2 | 4 | 0 | 6 | 0 |
| 测试/生成器（Stream） | 1 | 1 | 2 | 0 | 0 |
| **合计** | **20** | **22** | **7** | **31** | **4** |

> 上表按插件计数，不按连接器对计数；A 类 7 个为：`sqlitereader`、`streamreader`、`postgresqlwriter`、`sqlitewriter`、`txtfilewriter`、`jsonfilewriter`、`streamwriter`。C 类 4 个为：`sybasereader`、`hdfsreader`、`sybasewriter`、`hdfswriter`。按插件计：A 7/42 = 16.7%，B 31/42 = 73.8%，C 4/42 = 9.5%。

---

## 4. 共享实现、重复度与契约偏差

### 4.1 可量化的重复

| 能力 | 现状 | 重复量 | 影响 |
|---|---|---:|---|
| JDBC URL / DSN 解析 | 每个插件自己解析；MySQL 解析 query 参数，PostgreSQL 只接受无参数 URL；Oracle/SQL Server 另有 `internal/*dsn` | 19 个文件、556 行 | JDBC 参数兼容性不一致；`useSSL`、`characterEncoding` 等 Java 参数可能直接导致 Go 驱动失败 |
| split 算法 | MySQL/PostgreSQL/OceanBase/StarRocks 四份近同源；SQLite/SQLServer/Oracle/ClickHouse/Doris/Cassandra 各一份 | 10 个文件、约 1,350 行 | 边界、类型、where 合并、回退策略各不相同；修一个 bug 要改多处 |
| 类型转换 | Reader 的 `convertValue/convertToColumn` 与 Writer 的 `convertColumnValue` 分散 | 30 个文件、约 1,524 行 | NULL/空串、Decimal、日期精度、二进制判断不一致 |
| 文件 CSV codec | TXT、FTP、HDFS 三份 `convertRecord/convertColumnValue/convertValue` 几乎逐行复制；OSS 另有一套 | 3 份同源 `convertValue`（各约 60–70 行）加各自 record 转换 | 字段越界、nullFormat、日期格式、类型失败回退行为不一致 |
| 表列查询（`getTableColumns`） | MySQL、SQLite、SQL Server、StarRocks、OceanBase、Oracle、PostgreSQL 等各自实现 | 10 个文件 | 大小写、schema、视图、系统列行为不一致 |
| DML 模式 | MySQL/OceanBase/SQLite 各有 insert/replace/update 三套函数；其他 writer 各自拼 INSERT | `insertRecords` 4 份、`replaceRecords` 3 份、`updateRecords` 3 份 | 无法统一事务、重试、幂等和 schema 校验 |
| 插件注册样板 | 每个插件一个 `init.go` | 42 个文件、1,137 行 | 应改为 connector 注册表 + capability 描述 |
| 事务 | 只有 GaussDB、Oracle、Sybase writer 使用 `Begin/Commit/Rollback` | 3 / 22 | 绝大多数 writer 是 autocommit，批量失败会留下部分数据 |
| 重试 | 只有 Doris、StarRocks、Neo4j、FTP writer 有显式重试 | 4 / 42 | 瞬时故障恢复能力弱；无统一 retry 分类 |
| 共享 writer | `common/database/rdbms/writer` 1,266 行 | 仅 1 个插件真正使用 | 共享层没有形成“family”，反而成为单插件特例 |

补充事实：

- `common/database/rdbms/writer` 的 `CommonRdbmsWriterJob/Task` 只有 `postgresqlwriter` 委托使用；`oraclereader` 和 `gaussdbwriter` 只是 import 了 `DoPretreatment` 或 `DatabaseType`，并未复用写入逻辑。
- `internal/connectiontest` 与 `internal/dbexplorer` 已经实现了 MySQL/PostgreSQL/SQLite/SQL Server/Oracle 的安全 DSN 构造与 schema 浏览，但**没有任何插件复用它们**；插件侧是另一套 URL 解析。新平台应合并这两套 DSN/dialect 逻辑。
- 42 个插件中只有 4 个文件有测试；插件测试 24 个函数，基本只覆盖 JSON reader 的 schema 推断、SQL Server reader 的少量转换、Oracle writer 的 SQL 生成和 script transformer。

### 4.2 与 Java DataX 的行为偏差

| 维度 | Java DataX 契约 | 本仓库实际 | 证据 |
|---|---|---|---|
| 多表 reader | MySQL/PostgreSQL reader 的 `table` 数组支持多表，要求同 schema | 多数 reader 只用 `tables[0]`，其余表静默丢弃 | `plugins/reader/mysqlreader/mysql_reader.go:626`、`plugins/reader/postgresqlreader/postgresql_reader.go:742`、`plugins/reader/starrocksreader/starrocks_reader.go:634` |
| 多表 writer | MySQL/PostgreSQL/Oracle/SQL Server writer 支持多表，要求结构一致 | 除 PostgreSQL common writer 外，多数 writer 只写 `tables[0]` | `plugins/writer/mysqlwriter/mysql_writer.go:395`、`plugins/writer/sqlitewriter/sqlite_writer.go`、`plugins/writer/sqlserverwriter/sqlserver_writer.go` |
| JDBC URL 数组 | 按顺序探测，第一个可用地址生效 | 一律 `jdbcUrls[0]`，无 failover | 24 处 `jdbcUrls[0]`，例如 `plugins/reader/mysqlreader/mysql_reader.go:448`、`plugins/reader/postgresqlreader/postgresql_reader.go:577` |
| 列配置 | MySQL reader 的 `column` 支持 `*`、列裁剪/重排、常量和表达式 | MySQL/StarRocks/OceanBase reader 把每个 `column` 都当标识符加反引号，表达式/常量会生成非法 SQL | `plugins/reader/mysqlreader/mysql_reader.go:629-638` |
| `splitPk` | 仅整数类型；非整数报错 | 扩展了字符串/日期/offset/hash；但 Sybase/GaussDB/TDengine 实际未使用 | `plugins/reader/sybasereader/sybase_reader.go:75`、`plugins/reader/gaussdbreader/gaussdb_reader.go:157` |
| Writer `writeMode` | MySQL 支持 insert/replace/update；PostgreSQL/Oracle/SQL Server 不支持 writeMode（仅 insert） | PostgreSQL writer 按主键**自动 upsert**；Oracle writer 接受 `REPLACE` 但等同 insert | `common/database/rdbms/writer/common.go:560-607`、`plugins/writer/oraclewriter/oracle_writer.go:165-185` |
| 错误限制 | `errorLimit` 覆盖 reader/writer 脏数据计数 | 只覆盖 Transformer；reader/writer 失败直接 fail-fast 或吞掉 | `core/task/taskgroupcontainer.go:72-74`、`docs/TRANSFORMER.md:353` |
| Job 生命周期 | reader/writer init、prepare、split、post、destroy 都调用 | 缺 reader prepare、job post、job destroy | `core/job/jobcontainer.go:194-209`、`:321-324` |
| 插件命名 | `mongodbreader`/`mongodbwriter`、`oceanbasev10reader`/`oceanbasev10writer`、`osswriter` | `mongoreader`/`mongowriter`、`oceanbasereader`/`oceanbasewriter`，无 `osswriter` | `plugins/*/init.go` 与 DataX 仓库目录 |
| HDFS 格式 | TEXT/ORC/RC/SEQ/CSV，支持压缩与 Kerberos | 只有 TEXT 入口，且读取实现是 stub；Parquet/ORC/RC/SEQ 未实现 | `plugins/reader/hdfsreader/hdfs_reader.go:497-513`、`plugins/writer/hdfswriter/hdfs_writer.go:566-574` |

这些偏差意味着：**先把旧插件包一层并不能获得 DataX 兼容性**，反而会把“看起来兼容、实际语义不同”的行为带进新平台。兼容层应显式标注支持矩阵，不能只按插件名放行。

---

## 5. 统一 Source/Sink 抽象必须覆盖的能力

> 本节描述“契约必须能表达什么”，不是最终接口设计；具体签名由 issue #8 的 grilling 决定。

### 5.1 能力矩阵

| 能力 | 必须定义 | 当前差距 | 优先级 |
|---|---|---|---|
| 生命周期 | job 级 `Open/Validate/Close`，partition 级 `OpenReader/OpenWriter/Commit/Abort`，明确谁拥有连接、临时文件、事务 | job 级 `Prepare/Post/Destroy` 未调用；HDFS/FTP/OSS 依赖未执行的 `Prepare` | P0 |
| Split / 并行度 | 逻辑 partition 与运行时并发分离；partition 可序列化、无重叠、全覆盖；`Plan` 可取消 | `channel` 只是建议；一 task 一 goroutine；Mongo 重复、MySQL 文本边界重复、Sybase/GaussDB/TDengine splitPk 无效 | P0 |
| Schema | 显式 `Schema{columns, types, nullability, precision/scale}`；source schema 到 sink schema 的映射；通配符展开和类型校验前置 | 只有 6 种 Column；NULL 与空串混淆；无列名/精度；多数 reader 不返回 schema | P0 |
| 批处理 | 按行/字节/时间 flush；batch 边界与事务边界一致；背压与内存上限；失败后可安全重试 | 多数 1024 行、无字节/时间上限；3/22 有事务；无内存预算 | P0 |
| 错误语义 | 区分 fatal（连接/认证/schema/DDL）与 record-level（脏数据）；`fail-fast / skip / dead-letter` 策略；`errorLimit` 覆盖 reader/writer；retry 分类与幂等 | 只统计 Transformer 脏数据；reader/writer 失败直接终止或静默吞掉；无 dead-letter；retry 仅 4 个插件 | P0 |
| 取消与超时 | `context.Context` 贯穿 connect/plan/read/write/flush/close；可中断的 SQL/HTTP/文件 IO | 42 个 task 均未实现 context 接口；大量 `context.Background()` | P0 |
| 观测 | 每 partition 的 rows/bytes/retries/dirty/latency；写失败必须计数 | 统计只覆盖 channel 收发；writer 写失败不计数；无 partition 维度 | P1 |
| Capability 描述 | `canSplit`、`canUpsert`、`transactional`、`schemaEvolution`、`supportsPredicatePushdown`、`maxParallelism` 等 | 配置项被静默忽略（encoding、fetchSize、REPLACE）； planner 无法提前校验 | P1 |
| Streaming seam | Source 声明 finite/unbounded；`Offset` 为 opaque token；sink commit 后 `Ack`；支持 `Resume(offset)`；sink 幂等键 | 完全没有 offset/checkpoint/resume 概念；只有一次性 Job | P1（接口）/ P2（运行时） |
| 配置与密钥 | typed config、secret 引用、DataX JSON 适配层与内部模型解耦 | JSON clone 造成 float64；密码出现在 DSN 日志；DataSource 只支持 5 种 JDBC 类型 | P1 |

### 5.2 生命周期

统一抽象至少需要三段生命周期，且每段都要有明确的失败语义：

1. **Job 级**：`Open(ctx, config)` 完成凭据校验、连接测试、schema 探测；`Close(ctx)` 释放连接与临时资源。这一层不能有数据面副作用；`preSql`/DDL 属于 writer 的 prepare 阶段，必须与 reader 的 validate 分开，避免“reader 配置错误但目标表已被 DROP”。
2. **Plan 级**：`Plan(ctx, hint)` 返回 partition 列表。允许访问数据库/对象存储做 min/max、文件 listing、token range，但必须可取消、可超时，并且不能长期持有连接。
3. **Partition 级**：`OpenReader/OpenWriter(ctx, partition)`，`Commit(ctx)` / `Abort(ctx)`。文件 sink 必须“写临时路径 -> 成功后原子 rename”；HDFS writer 当前的 `Post` 就是反例（且未被调用）。数据库 sink 必须明确 batch 事务边界；没有事务的 sink 要显式标记 `transactional=false`，由上层决定幂等补偿。

### 5.3 Split 与并行度

- **逻辑 partition**：类型化、可序列化，至少覆盖 `NumericRange`、`HashBucket`、`TokenRange`、`File/FileRange`、`ObjectKey/Range`、`Single`。每个 partition 必须可独立执行、可重试、可 checkpoint。
- **运行时并发**：与 partition 数量解耦。`Plan` 可以返回 1,000 个文件 partition，但运行时最多同时打开 `maxParallelism` 个；超出的排队。当前 Go 引擎的“一 task 一 goroutine”必须在新执行模型中改掉（issue #7）。
- **精确覆盖**：分片并集必须等于全量且不重叠。当前 MySQL 文本字典分片在相邻边界同时使用 `<=` 和 `>=`，会产生重复行（`plugins/reader/mysqlreader/mysql_reader.go:707-722`）；Mongo 的“克隆 N 份”会产生 N 倍重复（`plugins/reader/mongoreader/mongo_reader.go:103-115`、`:222`）。
- **Sink 的 split 语义**：多数 sink 没有天然分片；`Split` 应返回“并发写入单元”而不是假装有 partition。文件 sink 应按 task 生成独立文件；数据库 sink 的并发写同一表必须依赖 DB 的并发控制，不能假定有序。
- **确定性**：同样的配置和统计信息应生成稳定 partition，便于重跑和对比。当前依赖采样/数据库统计，部分实现有随机性；需要在 partition 中记录生成依据。

### 5.4 Schema 与类型

统一 schema 至少包含：列名、逻辑类型、物理类型、可空性、精度/scale、字符集/编码、默认值、是否主键/分区键。必须定义：

- **Source schema**：从数据库 information_schema、文件头/采样、JSON 推断、用户显式配置获得；允许“未知 schema”模式，但下游必须显式接受。
- **Sink schema**：目标表列、主键/唯一键、写入模式；用于生成 upsert/冲突策略和列映射。
- **映射规则**：按名或按位置；通配符展开；缺失列/多余列策略；类型不兼容时 fail-fast，而不是静默 0。
- **类型矩阵**：每个 dialect 给出 `source type -> logical type -> sink type` 的映射和舍入/截断规则。当前 6 种 Column 不足以表达 Decimal、Timestamp(n)、JSON、Array/Map、Unsigned、Nullable。
- **NULL 语义**：必须区分 SQL NULL、空字符串、缺失字段。当前 `StringColumn("")` 把空串置为 null，是跨插件数据损坏的根源之一（`common/element/column.go:126-131`）。

### 5.5 批处理与背压

- 每个 sink 声明 batch 触发条件：行数、字节数、时间、显式 flush；`Commit` 必须在最后一个 batch 后调用。
- 事务型 sink：batch 写与事务提交绑定；失败时整个 batch 回滚，不留下半批。当前只有 Oracle、Sybase、GaussDB 这样做；MySQL/SQLite/SQL Server 等都是 autocommit 多行 INSERT。
- 非事务型 sink（Stream Load、ES bulk、对象存储）：必须提供幂等键或可检测的“部分成功”响应。Doris/StarRocks 的 label 是正确方向，但 StarRocks 的 label 状态检查仍是 TODO。
- 背压：channel 不能只用固定 10,000 行；需要按字节/内存预算限流，并让 reader 感知 sink 的慢速。当前 `chan element.Record` 阻塞发送，但 buffer 上限与 task 数相乘后不可控。
- 性能：批量传输、列式/行批、对象复用属于 issue #5/#7 的优化范围；统一抽象应允许 `RecordBatch` 而不是强制单行 `Record`，否则会把现有行级开销带进新平台。

### 5.6 错误语义与重试

- **错误分类**：连接/认证、schema 不匹配、DDL、约束冲突、单条数据转换、网络瞬断、限流。不同类型对应不同策略。
- **记录级策略**：`fail-fast`（默认）、`skip`（计入 dirty，受 errorLimit）、`dead-letter`（写出原始行 + 错误原因）。当前只有 Transformer 有 dirty 统计；reader/writer 的转换失败要么终止，要么静默跳过。
- **errorLimit**：必须覆盖 reader 解析、writer 写入、Transformer；`record` 与 `percentage` 的语义要与 DataX 对齐，并在达到阈值时停止调度、标记失败。
- **重试**：区分可重试（连接重置、限流、死锁）与不可重试（约束冲突、类型错误）。重试必须有次数、退避、抖动和幂等保证；当前只有 Doris/StarRocks/Neo4j/FTP 有局部重试。
- **部分成功**：bulk/Stream Load 的响应可能逐条失败；必须解析 item 级错误并计入 dirty/errorLimit。ES writer 已解析 bulk item，但用 `ignoreParseError` 控制，语义混淆。

### 5.7 流式 offset / checkpoint seam

本期不实现流式运行时，但接口必须能表达：

- **Source capability**：`Finite` / `Unbounded`；当前全部是 finite（Stream 也是有限条数）。
- **Offset**：opaque `[]byte` 或 typed token，由 source 定义（binlog position、Kafka offset、Mongo resume token、文件 offset+inode）。Offset 必须可序列化、可比较、可持久化。
- **Ack/Checkpoint**：sink 成功提交后才 ack；checkpoint = 所有 partition 的最小安全 offset。失败重跑从 checkpoint 恢复，不重复写（需要 sink 幂等键）。
- **Partition 生命周期**：unbounded source 的 partition 可能动态增减；接口应允许 `Plan` 返回增量 partition，而不是一次性全量。
- **Sink 两阶段提交 seam**：`PrepareCommit(epoch)` / `Commit(epoch)` / `Abort(epoch)` 可作为未来 exactly-once 的扩展点；本期 batch sink 可以只实现单阶段。

### 5.8 可观测性、Capability 与配置

- 指标至少：每 partition 读取/写入行数、字节数、batch 数、重试数、dirty 数、耗时、当前 offset。
- Capability 描述用于 planner 和 UI：是否支持 split、最大并发、是否事务、是否 upsert、支持的类型、是否支持 schema evolution、是否支持 predicate pushdown。当前无任何 capability 元数据，配置错误往往在运行期才暴露。
- 配置模型应 typed、可校验、支持 secret 引用；DataX JSON 只作为兼容层输入，不应成为内部领域模型。当前 `Configuration` 是弱类型 map + JSON clone，插件各自解析字符串路径，错误信息质量参差。

---

## 6. 首批连接器与迁移顺序

### 6.1 迁移原则

1. **按协议族重写，不按插件逐个包 adapter。** 42 个插件可收敛为约 8–10 个 connector core：generic JDBC（MySQL/PostgreSQL/SQLite/Oracle/SQL Server 方言）、ClickHouse native、Doris/StarRocks Stream Load、本地/对象文件、Mongo、Elasticsearch、Cassandra、Neo4j、TDengine、Stream。
2. **先建契约与验收，再移植业务连接器。** 没有 schema、partition、error policy 的契约，移植越多返工越多。
3. **新平台不继承旧引擎的生命周期缺陷。** 新 Source/Sink 必须自己实现完整的 `Open/Plan/Read/Write/Commit/Abort/Close`，不复用 `JobContainer` 的调用顺序。
4. **DataX 兼容层独立存在。** 旧插件继续由旧引擎执行；新平台通过映射层导入/导出 DataX JSON，不要求旧插件满足新契约。

### 6.2 首批连接器（P1）

| 连接器 | 角色 | 选择理由 | 验收重点 |
|---|---|---|---|
| `mysqlreader` / `mysqlwriter` | generic JDBC source/sink 的第一个 dialect | 使用最广；能验证 splitPk、多表、writeMode、事务、类型矩阵 | 分片全覆盖；insert/replace/update 语义；batch 失败无半批；JDBC 参数兼容 |
| `postgresqlreader` / `postgresqlwriter` | generic JDBC 的第二个 dialect | 与 MySQL 共享 80% 逻辑，验证 dialect SPI；现有 common writer 可提升 | 不隐式 upsert；`ON CONFLICT` 只在显式模式启用；schema/PK 处理 |
| `sqlitereader` / `sqlitewriter` | 无外部依赖的测试连接器 | 可在 CI 中跑完整链路；验证并发=1、事务、文件 DB | 单 writer 并发；batch/事务；与 MySQL 方言的差异 |
| `txtfilereader` / `txtfilewriter` | 本地 CSV/TXT 文件 | 最常用的文件交换格式；可验证文件 split、gzip、原子提交 | 文件级 partition；encoding；临时文件 rename；错误行处理 |
| `streamreader` / `streamwriter` | 测试/压测连接器 | 无外部依赖，可精确控制行数；验证背压、取消、统计 | N×sliceRecordCount 语义；取消；内存上限；不进入生产目录 |

> JSON/JSONL 建议放在 P1.5：它有独特的 schema inference 价值，但需要先确定“无 schema 源”的契约，否则会把推断逻辑硬编码进 connector。

### 6.3 迁移顺序

| 阶段 | 内容 | 退出标准 |
|---|---|---|
| **P0 契约与 harness** | 定义 Source/Sink、Schema、Partition、ErrorPolicy、Offset seam；建立 conformance test harness；确定运行时并发模型（issue #7） | 接口评审通过；用内存 fake source/sink 跑通 split、schema、batch、error、cancel 五类测试 |
| **P1 generic JDBC + 本地文件 + Stream** | MySQL、PostgreSQL、SQLite、TXT/CSV、Stream（各 source/sink） | 与旧引擎对同一 DataX JSON 的行数/校验和一致；分片并集精确；失败注入通过；性能不低于基线 |
| **P2 SQL 方言与对象/半结构化** | Oracle、SQL Server、ClickHouse、Doris、StarRocks、JSON/JSONL、OSS | 每个 connector 有类型矩阵、schema 映射、错误策略、取消/超时；OSS 支持对象级 partition |
| **P3 NoSQL / 时序 / 远程文件** | OceanBase、GaussDB（PG 别名）、Databend、Mongo、Elasticsearch、Cassandra、Neo4j、TDengine、FTP/SFTP | 协议特性有明确 capability；upsert/幂等/重试语义通过故障注入 |
| **P4 退役评估** | Sybase、HDFS 不移植；评估是否保留旧兼容路径 | 无新增需求；文档标注 legacy-only |

**为什么不先做 Oracle/SQL Server：** 它们的协议知识已有，但差异主要在 DSN、占位符、类型和分页；先做 MySQL/PostgreSQL 可以把 generic JDBC 的 split、schema、batch、error 四件事打磨好，再以方言方式接入，边际成本最低。

**为什么不先做 Mongo/ES/Cassandra：** 它们没有可复用的 SQL 核心，且各自有独立的 batch/错误/一致性语义；应在 JDBC/file 契约稳定后作为独立 connector 实现，避免抽象被单个协议带偏。

**为什么放弃 Sybase/HDFS：** Sybase ASE 的 `splitPk` 未实现、`REPLACE` 退化为 INSERT，维护成本高于价值；HDFS reader 的 TEXT 读取是返回 0 行的 stub，writer 只有 TEXT 且 `Post` 不执行、Parquet/ORC 未实现、Kerberos 未实现。若未来有明确客户需求，应以 WebHDFS/对象存储或新的 Parquet/ORC 库重新实现，而不是移植现有代码。

### 6.4 验收方式

每个 connector 至少通过以下 gate：

1. **DataX JSON 兼容**：同一份 JSON 在旧引擎与新连接器上跑，行数、列值（顺序无关 checksum）、NULL/空串语义一致；已知偏差必须显式记录。
2. **分片精确性**：单 task、多 task、空表、单行、边界值、NULL splitPk、非均匀分布；所有 partition 并集等于全量且无重复。
3. **Schema**：`*` 展开、列重排、类型不兼容、Decimal/日期精度、NULL/空串、缺失字段；错误必须在写入前暴露。
4. **批处理/事务**：batch 边界、最后一批、batch 中途失败、手动 cancel；事务型 sink 无半批，非事务型 sink 有幂等/可检测部分成功。
5. **错误注入**：连接断开、认证失败、约束冲突、单条转换失败、限流；验证 fail-fast/skip/dead-letter 与 errorLimit。
6. **性能/内存**：在 issue #3 的基线上测 rows/s、P95 batch latency、峰值内存、失败恢复时间；目标 2–5× 由 issue #1 定义。
7. **可观测性**：每 partition 的 rows/bytes/dirty/retry 指标可见；写失败不再被漏计。

---

## 7. 主要风险与阻塞

| 风险 | 证据 | 影响 | 缓解 |
|---|---|---|---|
| **Job 生命周期不完整** | `core/job/jobcontainer.go:194-209`、`:321-324`；FTP/OSS/HDFS 的 `Prepare` 与 `Split` 依赖关系 | 3 个 reader 不可用；HDFS writer 不落最终文件；所有 `postSql` 不执行；FTP 连接泄漏 | 新平台不复用旧 `JobContainer`；旧引擎先补齐 reader prepare / job post / destroy，或明确标记这些插件 legacy-broken |
| **Split 正确性** | Mongo 全量重复（`mongoreader.go:103-115`、`:222`）；MySQL 文本边界重复（`mysql_reader.go:707-722`）；Sybase/GaussDB/TDengine splitPk 未用 | 数据重复/缺失，且难以在行数统计中发现 | 新 Partition 契约强制“并集=全量且不重叠”；每个 connector 做分片精确性测试 |
| **并发不受 channel 限制** | `core/job/jobcontainer.go:263-308`；文件/对象/表按数量 split | 目标库连接爆炸、内存爆炸、限流；`channel` 配置失效 | 新执行模型分离 `maxParallelism` 与 partition 数；worker pool + 有界队列 |
| **类型/空值语义** | `common/element/column.go:126-131`；文件 reader 解析失败返回 0；Mongo 默认当前时间；TDengine/Neo4j 吞错 | 静默数据损坏，且下游无法区分 NULL/空串 | 显式 Schema + 严格类型转换；错误行进入 dead-letter 或 fail-fast；禁止静默默认值 |
| **错误限制未覆盖 reader/writer** | `docs/TRANSFORMER.md:353`；`core/task/taskgroupcontainer.go:72-74` | 脏数据策略与 DataX 不一致；任务可能因单条坏数据失败或带病成功 | 新 ErrorPolicy 覆盖全链路；errorLimit 纳入 reader/writer 计数 |
| **DataX 兼容性 drift** | 插件名不一致；无 `osswriter`；OSS reader 未注册；多表/URL failover/列表达式缺失；PG writer 隐式 upsert | 导入旧 JSON 后行为不同；用户以为兼容实际不兼容 | 维护显式支持矩阵；兼容测试按插件名 + 配置项逐条覆盖；差异在 UI 上报错而非静默 |
| **测试与基准缺失** | 4 个插件测试文件、24 个测试函数、582 行测试；无端到端 DB 测试；无 benchmark | 无法证明重写前后等价；性能目标无法验收 | 先建 conformance harness + benchmark（issue #3）；每个 connector 必须有集成测试 |
| **安全** | GaussDB reader 日志打印完整 DSN（含密码）；SQL 标识符直接拼接；DataSource 密码明文存储 | 凭据泄漏、SQL 注入风险 | DSN 日志脱敏；标识符白名单/引用；secret 管理；仅参数化值 |
| **文档与实现漂移** | `README.md` 标 OSS reader 可用但未注册；HDFS/Parquet 文档与实现不符；`docs/ARCHITECTURE.md` 的接口定义已过期 | 用户按文档配置失败；维护者误判 | 文档生成/校验；支持矩阵以代码注册表为准 |

**阻塞项**：Source/Sink 契约（issue #8）和执行模型（issue #7）尚未定稿，因此本报告只能给出能力清单和迁移顺序，不能给出最终接口签名；schema 模型在 `common/element` 中不存在，需要先设计。除此之外，本次审计没有外部阻塞。

---

## 8. 附录

### 8.1 关键本地证据索引

| 主题 | 路径 |
|---|---|
| 插件接口 | `common/plugin/reader.go:10-41`、`common/plugin/writer.go:10-41`、`common/plugin/channel.go` |
| Job 生命周期 | `core/job/jobcontainer.go:141-241`、`:243-324` |
| Task 生命周期 | `core/task/taskgroupcontainer.go:40-239` |
| 并发与 buffer | `core/job/jobcontainer.go:263-308`、`core/task/taskgroupcontainer.go:261-294` |
| 共享 writer | `common/database/rdbms/writer/common.go:21-183`、`:254-347`、`:452-616` |
| 类型模型 | `common/element/column.go:8-18`、`:126-131`；`common/element/record.go` |
| 配置模型 | `common/config/configuration.go:271-286` |
| MySQL 分片 | `plugins/reader/mysqlreader/mysql_reader.go:97-136`、`:279-444`、`:681-745` |
| PostgreSQL 分片 | `plugins/reader/postgresqlreader/postgresql_reader.go:734-865` |
| Mongo 重复 | `plugins/reader/mongoreader/mongo_reader.go:103-115`、`:194-262` |
| TDengine 吞错 | `plugins/reader/tdenginereader/tdengine_reader.go:335-390` |
| HDFS reader stub | `plugins/reader/hdfsreader/hdfs_reader.go:497-513` |
| HDFS writer 临时文件 | `plugins/writer/hdfswriter/hdfs_writer.go:388-436`、`:458-505` |
| OSS reader 未注册 | `cmd/datax/main.go`（无 `ossreader`）；`plugins/reader/ossreader/init.go:26` |
| 文件 reader 重复转换 | `plugins/reader/txtfilereader/txtfile_reader.go:434-493`、`plugins/reader/ftpreader/ftp_reader.go:617-682`、`plugins/reader/hdfsreader/hdfs_reader.go:580-639` |
| 错误限制 | `core/task/taskgroupcontainer.go:72-74`、`docs/TRANSFORMER.md:345-353` |
| 插件测试 | `plugins/reader/jsonfilereader/jsonfile_reader_test.go`、`plugins/reader/sqlserverreader/sqlserver_reader_test.go`、`plugins/writer/oraclewriter/oracle_writer_test.go` |

### 8.2 官方 DataX 来源

- MySQL Reader：<https://github.com/alibaba/DataX/blob/master/mysqlreader/doc/mysqlreader.md>
- MySQL Writer：<https://github.com/alibaba/DataX/blob/master/mysqlwriter/doc/mysqlwriter.md>
- PostgreSQL Reader：<https://github.com/alibaba/DataX/blob/master/postgresqlreader/doc/postgresqlreader.md>
- PostgreSQL Writer：<https://github.com/alibaba/DataX/blob/master/postgresqlwriter/doc/postgresqlwriter.md>
- Oracle Writer：<https://github.com/alibaba/DataX/blob/master/oraclewriter/doc/oraclewriter.md>
- SQL Server Writer：<https://github.com/alibaba/DataX/blob/master/sqlserverwriter/doc/sqlserverwriter.md>
- TxtFile Reader：<https://github.com/alibaba/DataX/blob/master/txtfilereader/doc/txtfilereader.md>
- HDFS Reader：<https://github.com/alibaba/DataX/blob/master/hdfsreader/doc/hdfsreader.md>
- Cassandra Writer：<https://github.com/alibaba/DataX/blob/master/cassandrawriter/doc/cassandrawriter.md>
- Stream Reader 源码：<https://github.com/alibaba/DataX/blob/master/streamreader/src/main/java/com/alibaba/datax/plugin/reader/streamreader/StreamReader.java>
- DataX `JobContainer`：<https://github.com/alibaba/DataX/blob/master/core/src/main/java/com/alibaba/datax/core/job/JobContainer.java>
- DataX 插件目录：<https://github.com/alibaba/DataX>

### 8.3 方法与局限

- 本报告基于静态代码审计、`go build ./...` 与 `go test ./plugins/...`（均通过），未对 42 个插件逐一做真实数据库/文件系统端到端对拷。
- “不可用”的判断基于调用链：例如 FTP/OSS/HDFS reader 的 `Split` 依赖 `Prepare` 填充的列表，而引擎不调用 reader `Prepare`。这不排除某个插件在 `Init` 中做了额外初始化；本报告已逐一检查相关 `Init`。
- 行数为非测试 Go 代码行数，包含 `init.go`、`constants.go` 与 helper，不含测试文件。
- Java DataX 行为以官方文档和 `JobContainer`/`StreamReader` 源码为准；文档未覆盖的行为不臆测。
- 本报告不评价具体数据库版本的兼容性，也不评价性能数字；性能基线见 issue #3，技术选型见 issue #5。
