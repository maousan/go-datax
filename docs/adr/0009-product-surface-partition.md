# 管理台分为 DataX 区、ETL 区与共享运维区

管理台现在同时承载冻结的 DataX 功能与新的 Pipeline 平台。顶层菜单定为：概览 / 数据源 / DataX / ETL / 运维 / 系统设置。DataX 菜单只放 DataX 专属功能：datax任务、数据集、采集任务、运行历史；ETL 菜单放 Pipelines、源与目标（Sources/Sinks 用 tab）、调度、运行记录；数据源作为共享连接单独放顶层；运维菜单聚合服务监控、告警配置、告警日志；系统设置保持独立。概览是单页，带“全部 / DataX / ETL”区域筛选，统计与最近运行合并。Pipeline 详情页用“概览 / 草稿 / 版本 / 调度 / 运行”五个 tab，发布是草稿 tab 的动作，版本列表支持 pin/回滚，不单独做发布页。Transform 编辑器是有序步骤列表：加步骤 → 选类型 → 表单 + 表达式输入，并实时显示输出 schema 预览。

v1 只做全量批处理 + 调度：不做参数化、增量抽取、数据质量/血缘；Source 的 where/querySql 仍可写静态条件。DataX 区冻结，只允许菜单/命名调整与缺陷修复；UI 路由尽量保持，后端 API 按 ADR-0008 分区。

**Considered Options**: 三层菜单（数据管理 > DataX/ETL）vs 顶层 DataX/ETL（选顶层，避免深层导航）；Sources/Sinks 分离 vs 合并为“源与目标”（选合并，保持对称与菜单简洁）；概览分区 vs 单页筛选（选单页，运维需要总览）；单独发布页 vs 详情页 tab（选 tab，发布是版本的一部分）；JSON/SQL Transform 编辑器 vs 步骤列表（选步骤列表，与 ADR-0007 一致）；v1 支持参数化/增量/质量（全部后置，避免重新拉开 frontier）。

**Consequences**: web-admin 菜单/路由分组与页面需要重构；旧 UI 路由保留，避免书签失效；Transform 编辑器需要 schema 预览与表达式校验；v1 的全量批处理可以用 Source 的静态 where 满足一部分增量场景；参数化/增量/质量/血缘作为后续独立 effort。
