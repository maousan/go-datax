package sqlserverreader

import (
	"database/sql"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/longkeyy/go-datax/common/config"
	"github.com/longkeyy/go-datax/common/element"
	"github.com/longkeyy/go-datax/common/plugin"
	"github.com/longkeyy/go-datax/common/factory"
	"github.com/longkeyy/go-datax/internal/sqlserverdsn"
	"gorm.io/driver/sqlserver"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const (
	DefaultFetchSize = 1024
)

// SQLServerReaderJob SQL Server读取作业
type SQLServerReaderJob struct {
	config    config.Configuration
	username  string
	password  string
	jdbcUrls  []string
	tables    []string
	columns   []string
	where     string
	splitPk   string
	querySql  string // 支持自定义查询SQL
	fetchSize int
	factory   *factory.DataXFactory
}

func NewSQLServerReaderJob() *SQLServerReaderJob {
	return &SQLServerReaderJob{
		fetchSize: DefaultFetchSize,
		factory:   factory.GetGlobalFactory(),
	}
}

func (job *SQLServerReaderJob) Init(config config.Configuration) error {
	job.config = config

	// 获取数据库连接参数
	job.username = config.GetString("parameter.username")
	job.password = config.GetString("parameter.password")

	if job.username == "" || job.password == "" {
		return fmt.Errorf("username and password are required")
	}

	// 获取连接信息
	connections := config.GetListConfiguration("parameter.connection")
	if len(connections) == 0 {
		return fmt.Errorf("connection configuration is required")
	}

	// 处理第一个连接配置
	conn := connections[0]
	job.jdbcUrls = conn.GetStringList("jdbcUrl")

	if len(job.jdbcUrls) == 0 {
		return fmt.Errorf("jdbcUrl is required")
	}

	// 获取可选参数
	job.where = config.GetString("parameter.where")
	job.splitPk = config.GetString("parameter.splitPk")
	job.querySql = config.GetString("parameter.querySql")
	if job.querySql == "" {
		// 兼容部分平台把 querySql 放在 connection 里的配置
		if values := conn.GetStringList("querySql"); len(values) > 0 {
			job.querySql = values[0]
			if len(values) > 1 {
				log.Printf("Multiple querySql entries found, using the first one")
			}
		} else if value := conn.GetString("querySql"); value != "" {
			job.querySql = value
		}
	}

	// 获取fetchSize
	if fetchSize := config.GetInt("parameter.fetchSize"); fetchSize > 0 {
		job.fetchSize = fetchSize
	}

	// 检查配置模式：querySql 或 table/column 模式
	if job.querySql != "" {
		// querySql模式：直接使用用户提供的SQL查询
		log.Printf("SQL Server Reader initialized with querySql mode")
	} else {
		// table/column模式：需要table和column配置
		job.tables = conn.GetStringList("table")
		job.columns = config.GetStringList("parameter.column")

		if len(job.tables) == 0 {
			return fmt.Errorf("table is required when querySql is not specified")
		}
		if len(job.columns) == 0 {
			return fmt.Errorf("column is required when querySql is not specified")
		}
		log.Printf("SQL Server Reader initialized with table/column mode: tables=%v, columns=%v", job.tables, job.columns)
	}

	return nil
}

func (job *SQLServerReaderJob) Split(adviceNumber int) ([]config.Configuration, error) {
	taskConfigs := make([]config.Configuration, 0)

	// 如果使用querySql模式或没有设置splitPk，则不进行分片，使用单个任务
	if job.querySql != "" || job.splitPk == "" {
		taskConfig := job.config.Clone()
		taskConfig.Set("taskId", 0)
		taskConfigs = append(taskConfigs, taskConfig)
		if job.querySql != "" {
			log.Printf("Using querySql mode, single task")
		} else {
			log.Printf("No splitPk specified, using single task")
		}
		return taskConfigs, nil
	}

	// 如果设置了splitPk，尝试进行分片
	ranges, err := job.calculateSplitRanges(adviceNumber)
	if err != nil {
		log.Printf("Failed to calculate split ranges, fallback to single task: %v", err)
		taskConfig := job.config.Clone()
		taskConfig.Set("taskId", 0)
		taskConfigs = append(taskConfigs, taskConfig)
		return taskConfigs, nil
	}

	// 创建分片任务配置
	for i, splitRange := range ranges {
		taskConfig := job.config.Clone()
		taskConfig.Set("taskId", i)
		taskConfig.Set("parameter.splitRange", splitRange)
		taskConfigs = append(taskConfigs, taskConfig)
	}

	log.Printf("Split into %d tasks based on splitPk: %s", len(taskConfigs), job.splitPk)
	return taskConfigs, nil
}

func (job *SQLServerReaderJob) calculateSplitRanges(adviceNumber int) ([]map[string]interface{}, error) {
	// 连接数据库获取splitPk的最小值和最大值
	db, err := job.connect()
	if err != nil {
		return nil, err
	}
	defer func() {
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	}()

	table := job.tables[0]
	query := fmt.Sprintf("SELECT MIN([%s]), MAX([%s]) FROM [%s]", job.splitPk, job.splitPk, table)
	if job.where != "" {
		query += fmt.Sprintf(" WHERE %s", job.where)
	}

	var minVal, maxVal sql.NullInt64
	err = db.Raw(query).Row().Scan(&minVal, &maxVal)
	if err != nil {
		return nil, err
	}

	if !minVal.Valid || !maxVal.Valid {
		return nil, fmt.Errorf("splitPk column contains null values")
	}

	ranges := make([]map[string]interface{}, 0)
	step := (maxVal.Int64 - minVal.Int64 + int64(adviceNumber) - 1) / int64(adviceNumber)

	if step <= 0 {
		step = 1
	}

	for i := 0; i < adviceNumber; i++ {
		rangeStart := minVal.Int64 + int64(i)*step
		rangeEnd := minVal.Int64 + int64(i+1)*step - 1

		if i == adviceNumber-1 {
			rangeEnd = maxVal.Int64
		}

		ranges = append(ranges, map[string]interface{}{
			"start": rangeStart,
			"end":   rangeEnd,
		})
	}

	return ranges, nil
}

func (job *SQLServerReaderJob) connect() (*gorm.DB, error) {
	// 构建连接字符串
	jdbcUrl := job.jdbcUrls[0]

	// 转换JDBC URL为SQL Server连接字符串
	dsn, err := job.convertJdbcUrl(jdbcUrl)
	if err != nil {
		return nil, err
	}

	// 连接数据库
	db, err := gorm.Open(sqlserver.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %v", err)
	}

	return db, nil
}

func (job *SQLServerReaderJob) convertJdbcUrl(jdbcUrl string) (string, error) {
	return sqlserverdsn.Build(jdbcUrl, job.username, job.password)
}

func (job *SQLServerReaderJob) Post() error {
	return nil
}

func (job *SQLServerReaderJob) Destroy() error {
	return nil
}

// SQLServerReaderTask SQL Server读取任务
type SQLServerReaderTask struct {
	config    config.Configuration
	readerJob *SQLServerReaderJob
	db        *gorm.DB
	factory   *factory.DataXFactory
}

func NewSQLServerReaderTask() *SQLServerReaderTask {
	return &SQLServerReaderTask{
		factory: factory.GetGlobalFactory(),
	}
}

func (task *SQLServerReaderTask) Init(config config.Configuration) error {
	task.config = config

	// 创建ReaderJob来重用连接逻辑
	task.readerJob = NewSQLServerReaderJob()
	err := task.readerJob.Init(config)
	if err != nil {
		return err
	}

	// 建立数据库连接
	task.db, err = task.readerJob.connect()
	if err != nil {
		return fmt.Errorf("failed to connect to database: %v", err)
	}

	return nil
}

func (task *SQLServerReaderTask) StartRead(recordSender plugin.RecordSender) error {
	defer func() {
		if sqlDB, err := task.db.DB(); err == nil {
			sqlDB.Close()
		}
	}()

	// 构建查询SQL
	query, err := task.buildQuery()
	if err != nil {
		return err
	}

	log.Printf("Executing query: %s", query)

	// 执行查询
	rows, err := task.db.Raw(query).Rows()
	if err != nil {
		return fmt.Errorf("failed to execute query: %v", err)
	}
	defer rows.Close()

	// 获取列信息
	columns, err := rows.Columns()
	if err != nil {
		return fmt.Errorf("failed to get columns: %v", err)
	}

	// 创建扫描目标
	values := make([]interface{}, len(columns))
	valuePtrs := make([]interface{}, len(columns))
	for idx := range values {
		valuePtrs[idx] = &values[idx]
	}

	recordCount := 0
	for rows.Next() {
		// 扫描行数据
		err := rows.Scan(valuePtrs...)
		if err != nil {
			return fmt.Errorf("failed to scan row: %v", err)
		}

		// 创建记录
		record := task.factory.GetRecordFactory().CreateRecord()
		for _, value := range values {
			column := task.convertToColumn(value)
			record.AddColumn(column)
		}

		// 发送记录
		if err := recordSender.SendRecord(record); err != nil {
			return fmt.Errorf("failed to send record: %v", err)
		}

		recordCount++
		if recordCount%1000 == 0 {
			log.Printf("Read %d records", recordCount)
		}
	}

	log.Printf("Total records read: %d", recordCount)
	return nil
}

func (task *SQLServerReaderTask) buildQuery() (string, error) {
	// 如果配置了querySql，直接使用用户提供的SQL
	if task.readerJob.querySql != "" {
		return task.readerJob.querySql, nil
	}

	// 构建SELECT语句（table/column模式）
	columns := task.readerJob.columns
	table := task.readerJob.tables[0]

	var columnStr string
	if len(columns) == 1 && columns[0] == "*" {
		columnStr = "*"
	} else {
		// SQL Server使用方括号包围列名和表名
		quotedColumns := make([]string, len(columns))
		for i, col := range columns {
			quotedColumns[i] = fmt.Sprintf("[%s]", col)
		}
		columnStr = strings.Join(quotedColumns, ", ")
	}

	query := fmt.Sprintf("SELECT %s FROM [%s]", columnStr, table)

	// 添加WHERE条件
	var whereConditions []string

	// 原始WHERE条件
	if task.readerJob.where != "" {
		whereConditions = append(whereConditions, task.readerJob.where)
	}

	// 分片范围条件
	if splitRange := task.config.Get("parameter.splitRange"); splitRange != nil {
		if rangeMap, ok := splitRange.(map[string]interface{}); ok {
			start := rangeMap["start"]
			end := rangeMap["end"]
			condition := fmt.Sprintf("[%s] >= %v AND [%s] <= %v",
				task.readerJob.splitPk, start, task.readerJob.splitPk, end)
			whereConditions = append(whereConditions, condition)
		}
	}

	if len(whereConditions) > 0 {
		query += " WHERE " + strings.Join(whereConditions, " AND ")
	}

	return query, nil
}

func (task *SQLServerReaderTask) convertToColumn(value interface{}) element.Column {
	columnFactory := task.factory.GetColumnFactory()
	if value == nil {
		return columnFactory.CreateNullColumn(element.TypeString)
	}

	switch v := value.(type) {
	case int64:
		return columnFactory.CreateLongColumn(v)
	case int32:
		return columnFactory.CreateLongColumn(int64(v))
	case int:
		return columnFactory.CreateLongColumn(int64(v))
	case float64:
		return columnFactory.CreateDoubleColumn(v)
	case float32:
		return columnFactory.CreateDoubleColumn(float64(v))
	case string:
		return columnFactory.CreateStringColumn(v)
	case []byte:
		// go-mssqldb 把 DECIMAL/NUMERIC/MONEY 等以 []byte 返回，
		// 这里按内容还原为数值或文本，避免下游把它当成二进制。
		text := strings.TrimSpace(string(v))
		if text != "" {
			if intValue, err := strconv.ParseInt(text, 10, 64); err == nil {
				return columnFactory.CreateLongColumn(intValue)
			}
			if floatValue, err := strconv.ParseFloat(text, 64); err == nil {
				return columnFactory.CreateDoubleColumn(floatValue)
			}
			if utf8.Valid(v) && isPrintableText(text) {
				return columnFactory.CreateStringColumn(text)
			}
		}
		return columnFactory.CreateBytesColumn(v)
	case time.Time:
		return columnFactory.CreateDateColumn(v)
	case bool:
		return columnFactory.CreateBoolColumn(v)
	default:
		// 其他类型转换为字符串
		return columnFactory.CreateStringColumn(fmt.Sprintf("%v", v))
	}
}

func isPrintableText(value string) bool {
	for _, char := range value {
		if char == '\n' || char == '\r' || char == '\t' {
			continue
		}
		if !unicode.IsPrint(char) {
			return false
		}
	}
	return true
}

func (task *SQLServerReaderTask) Post() error {
	return nil
}

func (task *SQLServerReaderTask) Destroy() error {
	if task.db != nil {
		if sqlDB, err := task.db.DB(); err == nil {
			sqlDB.Close()
		}
	}
	return nil
}