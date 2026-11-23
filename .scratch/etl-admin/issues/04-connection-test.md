# 04: 连接测试

**What to build:** AdminUser 可以在保存前测试正在编辑的 DataSource 连接，也可以在列表中测试已保存的 DataSource。支持 MySQL、PostgreSQL、SQLite 与 SQL Server（含 jTDS URL）；成功时显示往返延迟，失败时显示可理解的底层错误；测试本身不写入数据库。

**Blocked by:** 03

**Status:** done

- [x] 新建 DataSource 表单中可以直接测试当前输入，无需先保存
- [x] 已保存的 DataSource 可以从列表触发测试
- [x] MySQL、PostgreSQL、SQLite 与 SQL Server 都可以测试
- [x] SQL Server 的 jTDS 与 Microsoft JDBC 两种 URL 都可以测试
- [x] 用户名或密码包含 `@`、`:` 等特殊字符时不影响连接
- [x] 测试成功时显示成功提示与耗时
- [x] 测试失败时显示失败提示与底层错误摘要（例如认证失败、主机不可达）
- [x] 连接超时会在合理时间内返回明确错误，而不是一直挂起
- [x] 测试过程不创建、修改任何 DataSource 记录
- [x] SQLite 连接测试以只读方式打开，不会创建数据库文件
- [x] API 层测试覆盖 SQLite 成功、不可达主机失败、已保存数据源与参数缺失
