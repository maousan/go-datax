# 04: 连接测试

**What to build:** AdminUser 可以在保存前测试正在编辑的 DataSource 连接，也可以在列表中测试已保存的 DataSource。支持 MySQL、PostgreSQL、SQLite；成功时显示往返延迟，失败时显示可理解的底层错误；测试本身不写入数据库。

**Blocked by:** 03

**Status:** ready-for-agent

- [ ] 新建 DataSource 表单中可以直接测试当前输入，无需先保存
- [ ] 已保存的 DataSource 可以从列表或详情触发测试
- [ ] MySQL、PostgreSQL、SQLite 三种类型都可以测试
- [ ] 测试成功时显示成功提示与耗时
- [ ] 测试失败时显示失败提示与底层错误摘要（例如认证失败、主机不可达）
- [ ] 连接超时会在合理时间内返回明确错误，而不是一直挂起
- [ ] 测试过程不创建、修改任何 DataSource 记录
- [ ] 未支持的连接参数缺失时返回清晰错误
- [ ] API 层测试覆盖 SQLite 成功、不可达主机失败、超时与参数缺失
