# 05: SyncJob（form 模式）+ 展开与导出

**What to build:** AdminUser 可以用表单创建、编辑、复制、删除 SyncJob：选择 reader 的 DataSource、表、列、可选 where 与 splitPk，选择 writer 的 DataSource、表、列与写入模式，并设置 channel 与 errorLimit。保存前校验列数与引用；任务可以导出为标准 DataX JSON。删除被 SyncJob 引用的 DataSource 会被阻止并说明引用者。

**Blocked by:** 03

**Status:** done

- [x] 表单模式新建任务：名称、reader 数据源/表/列/where/splitPk、writer 数据源/表/列/writeMode、channel、errorLimit
- [x] 任务列表展示名称、编写模式、reader/writer 摘要与更新时间
- [x] 可编辑任务名称与表单字段并保存
- [x] 可复制任务生成一个独立的同名副本（名称加后缀）
- [x] 可删除任务，列表移除
- [x] reader 与 writer 的列数量不一致时拒绝保存并给出中文提示
- [x] 引用不存在的 DataSource 时拒绝保存
- [x] 删除被任务引用的 DataSource 返回冲突，并列出引用它的任务名称
- [x] 导出功能输出标准 DataX JSON，三种数据源类型都展开正确（插件名、连接信息、列、channel）
- [x] 导出包含解析后的连接凭据，并在界面上明确提示这一点
- [x] 展开逻辑有表驱动纯函数测试；API 层测试覆盖 CRUD、校验与冲突
