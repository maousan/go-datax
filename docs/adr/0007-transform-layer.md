# 转换层是编译后的列级表达式链，不做 SQL 引擎

新 Pipeline 的 Transform 是 Pipeline 内的有序步骤（ADR-0004），在 `etl-worker` 中对带显式 Schema 的 Batch 逐批执行（ADR-0006）。v1 提供 project、filter、cast、rename、drop 五类步骤，配合固定的字符串/日期/数值/摘要/脱敏函数库；表达式用 `expr-lang/expr` 编译一次、按行求值，函数库固定、可类型检查。每个 step 声明输入/输出 schema，运行时校验；错误 fail-fast，带 step 与行号。join/aggregation/window 不在转换层实现，重计算走 Source 的 querySql 下推或下游数仓；不做 ELT。

**Considered Options**: worker 内嵌 SQL 引擎（SQLite/DataFusion）vs 列级表达式链（选表达式链：Target C 的 5 个 Transform ≤10% 回退与 CPU ≤25% 更容易达标，也不引入额外执行引擎）；脚本 UDF（JS/Starlark）vs 固定函数库（选固定函数库，类型检查与性能更可控，脚本留作 v2 可选插件）；Transform 节点下推给 Source（选只允许 Source 配置里的 querySql，不下推 Transform，保持语义可测试）；ELT（出 scope）。

**Consequences**: v1 不支持任意 join/聚合/窗口与脚本 UDF；表达式函数库需要版本化并做兼容测试；输出 schema 由步骤推导，发布前校验；Transform 的编辑器 UX 归 #11；函数库扩展需要发版。
