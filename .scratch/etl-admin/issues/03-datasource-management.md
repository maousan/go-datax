# 03: DataSource 管理

**What to build:** AdminUser 可以在 UI 中管理 DataSource：列表、搜索、新建、编辑、删除；支持 MySQL、PostgreSQL、SQLite 三种类型。所有响应中的密码一律脱敏，编辑时提交空密码表示保持原值；重名被拒绝；数据持久化在管理库中，重启后仍在。

**Blocked by:** 02

**Status:** ready-for-agent

- [ ] 列表展示名称、类型、JDBC 地址与更新时间，并可按名称搜索
- [ ] 可创建 MySQL、PostgreSQL、SQLite 三种类型的 DataSource，必填字段有中文校验提示
- [ ] 创建成功后列表立即出现该 DataSource
- [ ] 编辑 DataSource 时可以修改名称、地址、用户名与密码
- [ ] 编辑时密码留空不会清空或改写已保存的密码
- [ ] 所有列表与详情响应中的密码都是固定掩码，不返回明文
- [ ] 重名创建或改名返回明确的重名错误
- [ ] 删除需要二次确认，删除成功后列表移除
- [ ] 数据写入管理库，服务重启后仍可读取
- [ ] API 层测试覆盖 CRUD、脱敏、留空密码语义与重名冲突
