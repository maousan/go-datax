package server

import (
	"fmt"
	"strings"
)

// 数据集的 SQL 会作为读取器的 querySql 使用。为了让「结果字段」可以裁剪，
// 执行时会包一层投影：SELECT <字段> FROM (<模板 SQL>) dx_template_src。
// 因此模板 SQL 必须是单条 SELECT，且最外层不能有 ORDER BY（部分数据库不允许
// 子查询里出现 ORDER BY），不能再跟 LIMIT/OFFSET/FETCH。

type templateSQLToken struct {
	text  string
	upper string
	start int
}

// scanTemplateSQL 收集括号外的单词记号，并跳过引号、注释里的内容。
func scanTemplateSQL(sql string) (tokens []templateSQLToken, semicolons []int) {
	depth := 0
	for i := 0; i < len(sql); {
		switch c := sql[i]; {
		case c == '\'' || c == '"' || c == '`':
			i = skipQuotedTemplateSQL(sql, i, c)
			continue
		case c == '[':
			i = skipBracketTemplateSQL(sql, i)
			continue
		case c == '-' && i+1 < len(sql) && sql[i+1] == '-':
			for i < len(sql) && sql[i] != '\n' {
				i++
			}
			continue
		case c == '/' && i+1 < len(sql) && sql[i+1] == '*':
			i += 2
			for i+1 < len(sql) && !(sql[i] == '*' && sql[i+1] == '/') {
				i++
			}
			if i+1 < len(sql) {
				i += 2
			}
			continue
		case c == '(':
			depth++
			i++
			continue
		case c == ')':
			if depth > 0 {
				depth--
			}
			i++
			continue
		case c == ';':
			if depth == 0 {
				semicolons = append(semicolons, i)
			}
			i++
			continue
		}

		if isTemplateSQLWordStart(sql[i]) {
			start := i
			for i < len(sql) && isTemplateSQLWordPart(sql[i]) {
				i++
			}
			if depth == 0 {
				word := sql[start:i]
				tokens = append(tokens, templateSQLToken{
					text:  word,
					upper: strings.ToUpper(word),
					start: start,
				})
			}
			continue
		}
		i++
	}
	return tokens, semicolons
}

func skipQuotedTemplateSQL(sql string, start int, quote byte) int {
	for i := start + 1; i < len(sql); i++ {
		if sql[i] != quote {
			continue
		}
		if i+1 < len(sql) && sql[i+1] == quote {
			i++
			continue
		}
		return i + 1
	}
	return len(sql)
}

func skipBracketTemplateSQL(sql string, start int) int {
	for i := start + 1; i < len(sql); i++ {
		if sql[i] != ']' {
			continue
		}
		if i+1 < len(sql) && sql[i+1] == ']' {
			i++
			continue
		}
		return i + 1
	}
	return len(sql)
}

func isTemplateSQLWordStart(c byte) bool {
	return c == '_' || c == '$' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

func isTemplateSQLWordPart(c byte) bool {
	return isTemplateSQLWordStart(c) || c >= '0' && c <= '9'
}

// normalizeTemplateSQL 校验模板 SQL 并去掉结尾多余的空格和分号。
func normalizeTemplateSQL(raw string) (string, error) {
	sql := strings.TrimSpace(raw)
	if sql == "" {
		return "", nil
	}
	if len([]rune(sql)) > 20000 {
		return "", fmt.Errorf("SQL 不能超过 20000 个字符")
	}
	sql = strings.TrimRight(sql, " \t\r\n;")
	if sql == "" {
		return "", fmt.Errorf("SQL 不能为空")
	}

	tokens, semicolons := scanTemplateSQL(sql)
	if len(semicolons) > 0 {
		return "", fmt.Errorf("一次只能保存一条 SQL 语句")
	}
	if len(tokens) == 0 {
		return "", fmt.Errorf("SQL 不能为空")
	}
	switch tokens[0].upper {
	case "SELECT":
	case "WITH":
		return "", fmt.Errorf("数据集暂不支持 WITH 语句，请改写为普通 SELECT")
	default:
		return "", fmt.Errorf("数据集只支持 SELECT 语句")
	}
	for _, token := range tokens {
		if token.upper == "INTO" {
			return "", fmt.Errorf("数据集不支持 SELECT INTO")
		}
	}
	return sql, nil
}

// templateExecutionSQL 返回可以安全嵌入子查询的 SQL：去掉最外层 ORDER BY。
func templateExecutionSQL(raw string) (string, error) {
	sql, err := normalizeTemplateSQL(raw)
	if err != nil {
		return "", err
	}
	if sql == "" {
		return "", fmt.Errorf("SQL 不能为空")
	}

	tokens, _ := scanTemplateSQL(sql)
	for index := 0; index+1 < len(tokens); index++ {
		if tokens[index].upper != "ORDER" || tokens[index+1].upper != "BY" {
			continue
		}
		for _, token := range tokens[index+2:] {
			switch token.upper {
			case "LIMIT", "OFFSET", "FETCH":
				return "", fmt.Errorf("数据集不支持最外层 ORDER BY 之后再使用 LIMIT/OFFSET/FETCH")
			}
		}
		stripped := strings.TrimSpace(sql[:tokens[index].start])
		if stripped == "" {
			return "", fmt.Errorf("SQL 格式不正确")
		}
		return stripped, nil
	}
	return sql, nil
}

// buildTemplateReaderQuery 按模板输出字段拼装读取器的 querySql。
func buildTemplateReaderQuery(raw string, columns []string, dataSourceType string) (string, error) {
	if len(columns) == 0 {
		return "", fmt.Errorf("读取列不能为空")
	}
	executionSQL, err := templateExecutionSQL(raw)
	if err != nil {
		return "", err
	}

	quoted := make([]string, 0, len(columns))
	for _, column := range columns {
		column = strings.TrimSpace(column)
		if column == "" {
			return "", fmt.Errorf("读取列不能为空")
		}
		quoted = append(quoted, quoteTemplateIdentifier(dataSourceType, column))
	}
	// 换行是为了避免模板 SQL 末尾的行注释把右括号一起注释掉。
	return fmt.Sprintf("SELECT %s FROM (%s\n) dx_template_src",
		strings.Join(quoted, ", "), executionSQL), nil
}

// quoteTemplateIdentifier 用数据源对应方言引用查询结果里的列名。
func quoteTemplateIdentifier(dataSourceType, name string) string {
	switch dataSourceType {
	case "mysql":
		return "`" + strings.ReplaceAll(name, "`", "``") + "`"
	case "mssql":
		return "[" + strings.ReplaceAll(name, "]", "]]") + "]"
	default:
		return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
	}
}
