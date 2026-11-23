package oraclewriter

import (
	"database/sql"
	"fmt"
	"log"
	"strings"

	"github.com/longkeyy/go-datax/common/config"
	"github.com/longkeyy/go-datax/common/element"
	"github.com/longkeyy/go-datax/common/plugin"
	"github.com/longkeyy/go-datax/common/factory"
	coreplugin "github.com/longkeyy/go-datax/core/registry"
	"github.com/longkeyy/go-datax/internal/oracledsn"
	_ "github.com/sijms/go-ora/v2"
)

const (
	DefaultBatchSize     = 1024
	DefaultInsertTimeout = 30 // seconds
)

// OracleWriterJob Oracle写入Job
type OracleWriterJob struct {
	username     string
	password     string
	jdbcUrl      string
	tables       []string
	columns      []string
	writeMode    string
	updateKeys   []string
	preSql       []string
	postSql      []string
	session      []string
	batchSize    int
	configuration config.Configuration
	factory      *factory.DataXFactory
}

// OracleWriterTask Oracle写入Task
type OracleWriterTask struct {
	writerJob     *OracleWriterJob
	config        config.Configuration
	db            *sql.DB
	table         string
	insertStmt    *sql.Stmt
	updateStmt    *sql.Stmt
	updateArgs    []int
	columns       []string
	batchSize     int
	insertTimeout int
	factory       *factory.DataXFactory
}

func NewOracleWriterJob() *OracleWriterJob {
	return &OracleWriterJob{
		factory: factory.GetGlobalFactory(),
	}
}

func NewOracleWriterTask() *OracleWriterTask {
	return &OracleWriterTask{
		factory: factory.GetGlobalFactory(),
	}
}

func (job *OracleWriterJob) Init(config config.Configuration) error {
	log.Printf("Initializing Oracle writer job")

	job.configuration = config

	// 解析连接配置
	connections := config.GetListConfiguration("connection")
	if len(connections) == 0 {
		return fmt.Errorf("connection configuration is required")
	}

	// 使用第一个连接配置
	connection := connections[0]
	job.jdbcUrl = connection.GetString("jdbcUrl")
	if job.jdbcUrl == "" {
		return fmt.Errorf("jdbcUrl is required")
	}

	// 解析表配置
	job.tables = connection.GetStringList("table")
	if len(job.tables) == 0 {
		return fmt.Errorf("table configuration is required")
	}

	// 解析认证信息
	job.username = config.GetString("username")
	job.password = config.GetString("password")
	if job.username == "" || job.password == "" {
		return fmt.Errorf("username and password are required")
	}

	// 解析列配置
	job.columns = config.GetStringList("column")
	if len(job.columns) == 0 {
		return fmt.Errorf("column configuration is required")
	}

	// 解析写入模式
	job.writeMode = strings.TrimSpace(config.GetStringWithDefault("writeMode", "INSERT"))
	if err := job.parseWriteMode(job.writeMode); err != nil {
		return err
	}

	// 解析SQL配置
	job.preSql = config.GetStringList("preSql")
	job.postSql = config.GetStringList("postSql")
	job.session = config.GetStringList("session")

	// 解析批次大小
	job.batchSize = config.GetIntWithDefault("batchSize", DefaultBatchSize)

	log.Printf("Oracle writer job initialized, jdbcUrl: %s, tables: %v, columns: %v, writeMode: %s, updateKeys: %v, batchSize: %d",
		job.jdbcUrl, job.tables, job.columns, job.writeMode, job.updateKeys, job.batchSize)

	return nil
}

// parseWriteMode 支持 INSERT、REPLACE 与 update(col1, col2)。
// update 模式按给定键列更新，未命中时插入。
func (job *OracleWriterJob) parseWriteMode(raw string) error {
	upper := strings.ToUpper(strings.TrimSpace(raw))

	switch {
	case upper == "INSERT", upper == "REPLACE":
		return nil

	case upper == "UPDATE":
		return fmt.Errorf("writeMode UPDATE requires key columns, use update(col1, col2)")

	case strings.HasPrefix(upper, "UPDATE(") && strings.HasSuffix(upper, ")"):
		inner := strings.TrimSpace(raw[len("update(") : len(raw)-1])
		for _, part := range strings.Split(inner, ",") {
			key := strings.TrimSpace(part)
			if key != "" {
				job.updateKeys = append(job.updateKeys, key)
			}
		}
		if len(job.updateKeys) == 0 {
			return fmt.Errorf("writeMode update(...) requires at least one key column")
		}
		if !(len(job.columns) == 1 && job.columns[0] == "*") {
			for _, key := range job.updateKeys {
				found := false
				for _, column := range job.columns {
					if strings.EqualFold(column, key) {
						found = true
						break
					}
				}
				if !found {
					return fmt.Errorf("update key %s is not in the column list", key)
				}
			}
		}
		return nil

	default:
		return fmt.Errorf("unsupported writeMode: %s, only INSERT/REPLACE/update(col1, col2) are supported", raw)
	}
}

func (job *OracleWriterJob) Split(mandatoryNumber int) ([]config.Configuration, error) {
	log.Printf("Splitting Oracle writer job, mandatoryNumber: %d", mandatoryNumber)

	var taskConfigs []config.Configuration

	// 为每个表创建一个任务配置
	for _, table := range job.tables {
		taskConfig := job.configuration.Clone()

		// 设置单表连接配置
		connectionConfig := map[string]interface{}{
			"jdbcUrl": job.jdbcUrl,
			"table":   []string{table},
		}
		taskConfig.Set("connection", []map[string]interface{}{connectionConfig})
		taskConfigs = append(taskConfigs, taskConfig)
	}

	log.Printf("Oracle writer job split completed, taskCount: %d", len(taskConfigs))
	return taskConfigs, nil
}

func (job *OracleWriterJob) Prepare() error {
	log.Printf("Preparing Oracle writer job")

	// 建立连接进行准备工作
	db, err := job.connect()
	if err != nil {
		return fmt.Errorf("failed to connect to Oracle database: %v", err)
	}
	defer db.Close()

	// 执行preSql
	for _, sql := range job.preSql {
		if sql != "" {
			log.Printf("Executing preSql: %s", sql)
			if _, err := db.Exec(sql); err != nil {
				return fmt.Errorf("failed to execute preSql '%s': %v", sql, err)
			}
		}
	}

	log.Printf("Oracle writer job prepared successfully")
	return nil
}

func (job *OracleWriterJob) Post() error {
	log.Printf("Post-processing Oracle writer job")

	// 建立连接进行后处理工作
	db, err := job.connect()
	if err != nil {
		return fmt.Errorf("failed to connect to Oracle database: %v", err)
	}
	defer db.Close()

	// 执行postSql
	for _, sql := range job.postSql {
		if sql != "" {
			log.Printf("Executing postSql: %s", sql)
			if _, err := db.Exec(sql); err != nil {
				return fmt.Errorf("failed to execute postSql '%s': %v", sql, err)
			}
		}
	}

	log.Printf("Oracle writer job post-processing completed")
	return nil
}

func (job *OracleWriterJob) Destroy() error {
	log.Printf("Destroying Oracle writer job")
	return nil
}

func (job *OracleWriterJob) connect() (*sql.DB, error) {
	dsn, err := oracledsn.Build(job.jdbcUrl, job.username, job.password)
	if err != nil {
		return nil, err
	}

	db, err := sql.Open("oracle", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open Oracle connection: %v", err)
	}

	// 测试连接
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping Oracle database: %v", err)
	}

	return db, nil
}

func (task *OracleWriterTask) Init(config config.Configuration) error {
	log.Printf("Initializing Oracle writer task")

	task.config = config

	// 创建writerJob实例并初始化
	task.writerJob = NewOracleWriterJob()
	if err := task.writerJob.Init(config); err != nil {
		return err
	}

	// 获取表信息
	connections := config.GetListConfiguration("connection")
	if len(connections) > 0 {
		tables := connections[0].GetStringList("table")
		if len(tables) > 0 {
			task.table = tables[0]
		}
	}

	// 设置批次大小和超时时间
	task.batchSize = task.writerJob.batchSize
	task.insertTimeout = config.GetIntWithDefault("insertTimeout", DefaultInsertTimeout)

	// 建立数据库连接
	var err error
	task.db, err = task.writerJob.connect()
	if err != nil {
		return err
	}

	// 执行session SQL
	err = task.executeSessionSql()
	if err != nil {
		return err
	}

	// 准备插入语句
	err = task.prepareInsertStatement()
	if err != nil {
		return err
	}

	if len(task.writerJob.updateKeys) > 0 {
		if err := task.prepareUpdateStatement(); err != nil {
			return err
		}
	}

	log.Printf("Oracle writer task initialized, table: %s, batchSize: %d", task.table, task.batchSize)

	return nil
}

func (task *OracleWriterTask) executeSessionSql() error {
	for _, sessionSql := range task.writerJob.session {
		if sessionSql != "" {
			_, err := task.db.Exec(sessionSql)
			if err != nil {
				return fmt.Errorf("failed to execute session SQL '%s': %v", sessionSql, err)
			}
		}
	}
	return nil
}

func (task *OracleWriterTask) prepareInsertStatement() error {
	// 处理列配置
	columns := task.writerJob.columns
	if len(columns) == 1 && columns[0] == "*" {
		// 获取表的实际列名
		actualColumns, err := task.getTableColumns()
		if err != nil {
			return fmt.Errorf("failed to get table columns: %v", err)
		}
		columns = actualColumns
	}
	task.columns = columns

	// 构建INSERT语句
	placeholders := make([]string, len(columns))
	for i := range placeholders {
		placeholders[i] = fmt.Sprintf(":col%d", i+1)
	}

	insertSql := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
		task.table,
		strings.Join(columns, ", "),
		strings.Join(placeholders, ", "))

	var err error
	task.insertStmt, err = task.db.Prepare(insertSql)
	if err != nil {
		return fmt.Errorf("failed to prepare insert statement: %v", err)
	}

	return nil
}

// buildUpdateStatement 生成按指定键列更新的 SQL，并返回参数在记录中的顺序。
func buildUpdateStatement(table string, columns, keys []string) (string, []int, error) {
	keySet := make(map[string]bool, len(keys))
	for _, key := range keys {
		keySet[strings.ToLower(key)] = true
	}

	var setParts []string
	var whereParts []string
	var argsOrder []int

	for index, column := range columns {
		if keySet[strings.ToLower(column)] {
			continue
		}
		setParts = append(setParts, fmt.Sprintf("%s = :col%d", column, index+1))
		argsOrder = append(argsOrder, index)
	}

	for _, key := range keys {
		index := -1
		for i, column := range columns {
			if strings.EqualFold(column, key) {
				index = i
				break
			}
		}
		if index < 0 {
			return "", nil, fmt.Errorf("update key %s is not in the column list", key)
		}
		whereParts = append(whereParts, fmt.Sprintf("%s = :col%d", columns[index], index+1))
		argsOrder = append(argsOrder, index)
	}

	if len(setParts) == 0 {
		return "", nil, fmt.Errorf("update mode requires at least one non-key column")
	}

	statement := fmt.Sprintf("UPDATE %s SET %s WHERE %s",
		table,
		strings.Join(setParts, ", "),
		strings.Join(whereParts, " AND "),
	)
	return statement, argsOrder, nil
}

func (task *OracleWriterTask) prepareUpdateStatement() error {
	statement, argsOrder, err := buildUpdateStatement(task.table, task.columns, task.writerJob.updateKeys)
	if err != nil {
		return err
	}

	task.updateStmt, err = task.db.Prepare(statement)
	if err != nil {
		return fmt.Errorf("failed to prepare update statement: %v", err)
	}
	task.updateArgs = argsOrder
	log.Printf("Oracle update statement prepared: %s", statement)
	return nil
}

func (task *OracleWriterTask) getTableColumns() ([]string, error) {
	query := `
		SELECT column_name
		FROM user_tab_columns
		WHERE table_name = UPPER(?)
		ORDER BY column_id`

	rows, err := task.db.Query(query, task.table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var columns []string
	for rows.Next() {
		var columnName string
		if err := rows.Scan(&columnName); err != nil {
			return nil, err
		}
		columns = append(columns, columnName)
	}

	return columns, rows.Err()
}

func (task *OracleWriterTask) Prepare() error {
	return nil
}

func (task *OracleWriterTask) StartWrite(recordReceiver plugin.RecordReceiver) error {

	defer func() {
		if task.insertStmt != nil {
			task.insertStmt.Close()
		}
		if task.updateStmt != nil {
			task.updateStmt.Close()
		}
		if task.db != nil {
			task.db.Close()
		}
	}()

	var batch []element.Record
	recordCount := int64(0)

	for {
		record, err := recordReceiver.GetFromReader()
		if err != nil {
			if err == coreplugin.ErrChannelClosed {
				break
			}
			return fmt.Errorf("failed to get record from reader: %v", err)
		}

		batch = append(batch, record)

		// 当达到批次大小时，执行批量写入
		if len(batch) >= task.batchSize {
			if err := task.writeBatch(batch); err != nil {
				return err
			}
			recordCount += int64(len(batch))
			batch = batch[:0]

			// 输出进度
			log.Printf("Writing progress, records: %d", recordCount)
		}
	}

	// 写入剩余的记录
	if len(batch) > 0 {
		if err := task.writeBatch(batch); err != nil {
			return err
		}
		recordCount += int64(len(batch))
	}

	log.Printf("Write task completed, totalRecords: %d", recordCount)
	return nil
}

func (task *OracleWriterTask) writeBatch(batch []element.Record) error {

	// 开始事务
	tx, err := task.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %v", err)
	}
	defer tx.Rollback()

	// 在事务中使用prepared statement
	stmt := tx.Stmt(task.insertStmt)
	defer stmt.Close()

	var updateStmt *sql.Stmt
	if task.updateStmt != nil {
		updateStmt = tx.Stmt(task.updateStmt)
		defer updateStmt.Close()
	}

	for _, record := range batch {
		// 转换记录数据
		values := make([]interface{}, len(task.columns))
		for i := 0; i < len(task.columns) && i < record.GetColumnNumber(); i++ {
			column := record.GetColumn(i)
			values[i] = task.convertValue(column)
		}

		// update 模式：先按键列更新，未命中再插入
		if updateStmt != nil {
			args := make([]interface{}, len(task.updateArgs))
			for i, index := range task.updateArgs {
				args[i] = values[index]
			}
			result, err := updateStmt.Exec(args...)
			if err != nil {
				log.Printf("Failed to execute update statement, table: %s, error: %v", task.table, err)
				return fmt.Errorf("failed to execute update: %v", err)
			}
			affected, err := result.RowsAffected()
			if err != nil {
				return fmt.Errorf("failed to read update result: %v", err)
			}
			if affected > 0 {
				continue
			}
		}

		// 执行插入
		if _, err := stmt.Exec(values...); err != nil {
			log.Printf("Failed to execute insert statement, table: %s, error: %v", task.table, err)
			return fmt.Errorf("failed to execute insert: %v", err)
		}
	}

	// 提交事务
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %v", err)
	}

	return nil
}

func (task *OracleWriterTask) convertValue(column element.Column) interface{} {
	if column == nil {
		return nil
	}

	// 使用新的Column接口方法
	switch column.GetType() {
	case element.TypeString:
		return column.GetAsString()
	case element.TypeLong:
		val, _ := column.GetAsLong()
		return val
	case element.TypeDouble:
		val, _ := column.GetAsDouble()
		return val
	case element.TypeDate:
		val, _ := column.GetAsDate()
		return val
	case element.TypeBool:
		// Oracle中布尔值通常用NUMBER(1)表示
		val, _ := column.GetAsBool()
		if val {
			return 1
		}
		return 0
	case element.TypeBytes:
		val, _ := column.GetAsBytes()
		return val
	default:
		// 默认转换为字符串
		return column.GetAsString()
	}
}

func (task *OracleWriterTask) Post() error {
	return nil
}

func (task *OracleWriterTask) Destroy() error {
	log.Printf("Destroying Oracle writer task")

	if task.insertStmt != nil {
		task.insertStmt.Close()
	}
	if task.db != nil {
		task.db.Close()
	}
	return nil
}