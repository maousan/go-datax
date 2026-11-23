# 06: SyncJob（raw 模式）+ 导入 + Monaco

**What to build:** AdminUser 可以用完整 DataX JSON 创建与编辑 raw 模式 SyncJob。编辑器提供语法高亮、错误提示与格式化；保存前校验结构（`job.content[]`、reader/writer 的 name 与 parameter）。可以把现有 DataX JSON 直接导入为新任务；任务的编写模式创建后不可切换。

**Blocked by:** 05

**Status:** ready-for-agent

- [ ] 新建任务时可选择 raw 模式并粘贴完整 DataX JSON
- [ ] 编辑器对 JSON 语法错误实时提示，并支持一键格式化
- [ ] 保存前校验结构：缺少 `job`、`content`、reader/writer 的 name 或 parameter 时拒绝并指出问题
- [ ] 校验失败时不创建任务
- [ ] 导入现有 DataX JSON 文件内容即可创建 raw 任务
- [ ] raw 任务可以编辑并保存新的 JSON
- [ ] 任务创建后编写模式固定，编辑页不提供切换
- [ ] raw 任务导出与原内容语义一致
- [ ] 列表与详情正确标识 raw 模式
- [ ] API 层测试覆盖 raw 创建、结构校验失败、导入与导出
