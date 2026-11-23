# 03: DataSource 管理

**What to build:** AdminUser 可以在 UI 中管理 DataSource：列表、搜索、新建、编辑、删除；支持 MySQL、PostgreSQL、SQLite、SQL Server（mssql）与 Oracle。所有响应中的密码一律脱敏，编辑时提交空密码表示保持原值；重名被拒绝；数据持久化在管理库中，重启后仍在。

**Blocked by:** 02

**Status:** done

- [x] 列表展示名称、类型、JDBC 地址与更新时间，并可按名称搜索
- [x] 可创建 MySQL、PostgreSQL、SQLite、SQL Server 与 Oracle 类型的 DataSource，必填字段有中文校验提示
- [x] SQL Server 同时接受 `jdbc:sqlserver://` 与 `jdbc:jtds:sqlserver://` 两种连接地址
- [x] Oracle 接受 `jdbc:oracle:thin:@host:port/service`、`@//host:port/service`、`@host:port:SID` 与 `oracle://` 形式
- [x] 创建成功后列表立即出现该 DataSource
- [x] 编辑 DataSource 时可以修改名称、地址、用户名与密码
- [x] 编辑时密码留空不会清空或改写已保存的密码
- [x] 所有列表与详情响应中的密码都是固定掩码，不返回明文
- [x] 重名创建或改名返回明确的重名错误
- [x] 删除需要二次确认，删除成功后列表移除
- [x] 数据写入管理库，服务重启后仍可读取
- [x] API 层测试覆盖 CRUD、脱敏、留空密码语义与重名冲突
