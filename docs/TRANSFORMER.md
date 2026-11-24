# 数据转换(Transformer)模块

go-datax 支持在数据传输过程中实时转换和过滤记录。本实现与
[Addax Transformer](https://wgzhao.github.io/Addax/develop/transformer/) 的 UDF
语义和"计量和脏数据"规则保持一致，并额外提供 `dx_digest`。

## 功能概述

Transformer 在数据流中的位置：

```
Reader → Transformer Chain → Writer
```

- 每个 Transformer 按配置顺序依次执行，支持链式处理。
- 返回 `null`（过滤）或抛出错误的记录不会进入 Writer。
- 转换只影响数据，不改变 Reader/Writer 的分片逻辑。

## 内置 Transformer

所有 Transformer 的参数都通过 `parameter.columnIndex` 指定列（从 0 开始），
其余参数放在 `parameter.paras` 数组中。列索引对应当前 Record 的字段顺序。

### dx_substr - 字符串截取

```text
dx_substr(idx, pos, length)
```

从 `pos`（包含，从 0 开始）截取 `length` 个字符。

- 按 Unicode 字符（rune）计数，中文/emoji 不会被按字节截断。
- 字段为 `null` 时直接返回，不参与本 Transformer。
- `pos` 或 `length` 为负数在启动时报错；`pos` 超过字符串长度时该记录成为脏数据。
- 截取长度超出剩余字符时自动截到末尾。

```json
{
  "name": "dx_substr",
  "parameter": {
    "columnIndex": 1,
    "paras": ["0", "4"]
  }
}
```

### dx_pad - 字符串填充

```text
dx_pad(idx, flag, length, chr)
```

- `flag`：`l`/`left` 表示头部填充，`r`/`right` 表示尾部填充。
- 源字符串短于 `length` 时用 `chr` 补齐；长于 `length` 时**一律从右侧截断**。
- 字段为 `null` 时按空字符串处理，因此结果全部由 `chr` 组成。
- `length` 必须为非负整数，`chr` 不能为空。

```json
{
  "name": "dx_pad",
  "parameter": {
    "columnIndex": 1,
    "paras": ["l", "8", "0"]
  }
}
```

| 输入 | 参数 | 输出 |
|---|---|---|
| `xyz` | `l, 4, A` | `Axyz` |
| `xyz` | `r, 4, A` | `xyzA` |
| `xyzzzzz` | `l, 4, A` | `xyzz` |
| `null` | `l, 4, A` | `AAAA` |

### dx_replace - 字符串替换

支持两种写法：

#### 1. Addax 位置替换（推荐）

```text
dx_replace(idx, pos, length, str)
```

从 `pos`（从 0 开始）开始，用 `str` 替换 `length` 个字符。

- 按 Unicode 字符计数。
- `pos` 超过字符串长度时该记录成为脏数据；`pos + length` 超出末尾时替换到末尾。
- 字段为 `null` 时直接返回。

| 输入 | 参数 | 输出 |
|---|---|---|
| `dataxTest` | `2, 4, ****` | `da****est` |
| `dataxTest` | `5, 10, ****` | `datax****` |
| `addaxTest` | `2, 4, ****` | `ad****est` |

```json
{
  "name": "dx_replace",
  "parameter": {
    "columnIndex": 1,
    "paras": ["2", "4", "****"]
  }
}
```

#### 2. 全局查找替换（兼容旧配置）

`paras` 为 2 个元素时，表示把列中所有 `search` 替换为 `replacement`：

```json
{
  "name": "dx_replace",
  "parameter": {
    "columnIndex": 1,
    "paras": ["@old-domain.com", "@new-domain.com"]
  }
}
```

`search` 不能为空字符串。两种形式靠参数个数区分，旧作业文件无需修改。

### dx_filter - 数据过滤

```text
dx_filter(idx, operator, expr)
```

**支持的操作符**：`like`、`not like`、`>`、`=`、`<`、`>=`、`!=`、`<=`。

- 条件成立时返回 `null`，该行被过滤。
- `like` / `not like` 将字段转换为字符串，并把 `expr` 当作**正则表达式进行全匹配**
  （等价于 `^expr$`）。这里不是 SQL LIKE，`%`、`_` 没有通配含义。
- 数值类型按大小比较；字符串和布尔类型按字典序比较；日期按毫秒时间戳比较。
- `expr` 与列类型不匹配（例如数值列使用 `expr="abc"`）会产生脏数据。
- `null` 处理与 Addax 一致：
  - `= null` 命中并过滤；`!= null` 不命中，记录保留。
  - `like` 不命中，记录保留；`not like` 命中并过滤。
  - 其他比较操作符不命中，记录保留。
- 不支持多个字段的联合过滤。

```json
{
  "name": "dx_filter",
  "parameter": {
    "columnIndex": 2,
    "paras": [">", "18"]
  }
}
```

```json
{
  "name": "dx_filter",
  "parameter": {
    "columnIndex": 3,
    "paras": ["like", ".*@example\\.com"]
  }
}
```

### dx_digest - 数据摘要（go-datax 扩展）

```text
dx_digest(idx, algorithm)
```

对字段值计算摘要并写回该列，支持 `md5`、`sha1`、`sha256`。字段为 `null`
时直接返回。Addax 本身不包含此函数，保留它是为了兼容 go-datax 既有文档与配置。

```json
{
  "name": "dx_digest",
  "parameter": {
    "columnIndex": 5,
    "paras": ["md5"]
  }
}
```

### dx_script - 自定义脚本（javascript / starlark / expr）

`dx_script` 是记录级 Transformer：脚本可以访问当前记录的所有列并进行修改。
它基于 [go-scripts](https://github.com/tx7do/go-scripts) 的纯 Go 引擎，不是
`dx_groovy` 的语法兼容实现——存量 Groovy 脚本需要按下面的 API 重写。

```json
{
  "name": "dx_script",
  "parameter": {
    "language": "javascript",
    "code": "function transform() { set_string(0, get_string(0).toUpperCase()); return true; }"
  }
}
```

参数：

| 参数 | 必填 | 说明 |
|---|---|---|
| `language` | 是 | `javascript`、`starlark` 或 `expr` |
| `code` | 二选一 | 内联脚本 |
| `codeFile` | 二选一 | 脚本文件；相对路径按 `$GO_DATAX_HOME`（兼容 `$ADDAX_HOME`）解析 |
| `timeoutMs` | 否 | 单条记录执行超时，仅 `javascript` 支持 |

#### 入口与返回值

| 语言 | 入口 | 说明 |
|---|---|---|
| `javascript` | `function transform() { ... }` | 顶层代码在每个引擎初始化时执行一次（配置校验和各 TaskGroup 都会初始化），请保持无副作用 |
| `starlark` | `def transform(): ...` | 同上 |
| `expr` | 表达式本身 | 每次对一条记录求值，不支持语句和循环 |

返回 `false` 表示过滤该记录（不写入 Writer）；其他返回值（包括 `true`、`null`、
`None`、无返回值）都表示保留。过滤语义不再依赖 `null`，可以放心使用。

#### 记录 API

脚本通过以下全局函数操作当前记录（列索引从 0 开始）：

| 函数 | 说明 |
|---|---|
| `column_count()` | 列数 |
| `get_type(i)` | 列类型：`long`/`double`/`string`/`date`/`bool`/`bytes`/`null` |
| `is_null(i)` | 是否为空 |
| `get_string(i)` | 按字符串读取，null 返回 `""` |
| `get_long(i)` | 按整数读取（date 返回毫秒时间戳） |
| `get_double(i)` | 按浮点数读取 |
| `get_bool(i)` | 按布尔值读取 |
| `get_bytes(i)` | 按原始字节读取（以字符串形式传入脚本） |
| `set_string(i, v)` | 写入字符串 |
| `set_long(i, v)` | 写入整数 |
| `set_double(i, v)` | 写入浮点数 |
| `set_bool(i, v)` | 写入布尔值 |
| `set_date(i, millis)` | 写入日期（Unix 毫秒） |
| `set_bytes(i, v)` | 写入原始字节 |
| `set_null(i)` | 写入同类型的 null |
| `log(msg)` | 写入任务日志（debug 级别），返回 `true` |

读取时的类型转换失败、列索引越界、脚本异常或超时都会让该记录成为**脏数据**，
并按 `errorLimit` 处理；脚本正常返回 `false` 则计入过滤数。

#### 示例

JavaScript（截取前 3 个字符并给年龄加 1）：

```javascript
function transform() {
  set_string(0, get_string(0).substring(0, 3));
  set_long(1, get_long(1) + 1);
  return true;
}
```

过滤掉状态为 `deleted` 的记录：

```javascript
function transform() {
  return get_string(2) !== "deleted";
}
```

Starlark：

```python
def transform():
    set_string(0, get_string(0) + "-starlark")
    return True
```

Expr（表达式式转换；setter 返回 true，因此表达式结果即保留）：

```text
set_string(0, get_string(0) + "-expr")
```

Expr 过滤：

```text
get_long(1) > 100
```

#### 说明

- 脚本引擎常驻内存，顶层变量会跨记录保留；请避免依赖不确定的跨记录状态。
- `get_*` 读取 null 列时返回零值，请先用 `is_null(i)` 判断；空字符串在
  go-datax 的 element 模型中同样视为 null。
- `javascript` 的 `timeoutMs` 同时约束顶层代码和单条记录执行，通过中断运行时实现；
  `starlark`/`expr` 暂不支持超时，也无法被中断，请确保脚本能够终止。
- 脚本运行在纯 Go 沙箱中，没有文件、网络或进程访问能力；但仍拥有与作业配置相同的
  信任级别，请勿执行不可信来源的脚本。

### 暂不支持

- `dx_groovy`：Go 不带 Groovy/JVM 运行时，配置该名称会在启动时报错；请改用
  `dx_script` 并按上面的 API 重写脚本。
- `dx_map`：Addax 文档中未定义其语义，暂不支持。

启动阶段会校验 Transformer 名称、参数个数、数值参数、填充方向、正则表达式、摘要算法
以及 `dx_script` 的语言/脚本来源，尽量在读取/写入开始前暴露配置错误。

## 配置方式

`transformer` 是 `job.content[0]` 下的数组，按顺序执行：

```json
{
  "job": {
    "setting": {
      "speed": {"channel": 2},
      "errorLimit": {"record": 10, "percentage": 0.02}
    },
    "content": [{
      "reader": { "name": "mysqlreader", "parameter": { "...": "..." } },
      "transformer": [
        {
          "name": "dx_substr",
          "parameter": {"columnIndex": 1, "paras": ["0", "10"]}
        },
        {
          "name": "dx_filter",
          "parameter": {"columnIndex": 2, "paras": [">", "100"]}
        },
        {
          "name": "dx_replace",
          "parameter": {"columnIndex": 3, "paras": ["2", "4", "****"]}
        }
      ],
      "writer": { "name": "mysqlwriter", "parameter": { "...": "..." } }
    }]
  }
}
```

`paras` 中的数字可以直接写成 JSON 数字，例如 `["0", 10]`，构建时会统一转换为字符串。

## 执行机制与脏数据

1. Reader 读取一条记录。
2. 按配置顺序执行 Transformer 链。
3. 任一 Transformer 返回 `null`：记录被过滤，跳过剩余 Transformer。
4. 任一 Transformer 报错：记录记为**脏数据**，**跳过剩余 Transformer**，且不写入 Writer。
5. 通过全部 Transformer 的记录写入 Writer。

脏数据默认不会中断作业，但会计入 `job.setting.errorLimit`：

- `record`：允许的脏数据条数上限（超过后任务失败）。
- `percentage`：允许的脏数据比例上限，支持两种写法：
  - 小数比例：`0.02` 表示 2%（DataX/Addax 惯例）；
  - 百分数：`5` 表示 5%。
- 缺省或非法值视为 100%（不限制）。

当前只有 Transformer 脏数据会纳入 `errorLimit`；Reader/Writer 自身的失败路径暂未接入该限制。

## 计量

每次任务都会统计以下指标（多 TaskGroup 自动累加）：

| 指标 | 含义 |
|---|---|
| Transform Input Records/Bytes | 进入 Transformer 链的记录数与字节数 |
| Transform Output Records/Bytes | 通过全部 Transformer、写入 Writer 的记录数与字节数 |
| Transformer Dirty Records/Bytes | 产生脏数据的记录数与出错时记录的字节数 |
| Transformer Success Records | 转换成功的记录数（等于 Output） |
| Transformer Failed Records | 转换失败的记录数（等于 Dirty） |
| Transformer Filter Records | 被过滤的记录数 |
| Transformer Used Time | 所有 Transformer 的累计执行耗时 |

运行中的进度日志会额外输出入参/出参，例如：

```text
Total 1000000 records, 22000000 bytes | Transform 1000000 records(in), 990000 records(out) |
Transformer Success 990000 records | Transformer Error 5000 records | Transformer Filter 5000 records |
Transformer usedTime 2.1s | Percentage 100.00%
```

作业结束时的最终统计包含：

```text
Transform Input Records    : 1000000
Transform Input Bytes      : 22000000
Transform Output Records   : 990000
Transform Output Bytes     : 20000000
Transformer Dirty Records  : 5000
Transformer Dirty Bytes    : 110000
Transformer Used Time      : 2.1s
```

每个 TaskGroup 结束时还会按 Transformer 名称输出 success/failed/filtered/usedTime 明细，
便于定位是哪一个环节产生了脏数据。

## 注意事项

- `columnIndex` 从 0 开始，必须对应 Reader 中 `column` 的字段顺序。
- 字符串位置和长度均按 Unicode 字符计算，不是字节。
- 过多的 Transformer 会影响吞吐；`dx_filter` 建议放在链的前部，尽早减少后续处理量。
- 正则匹配相对较慢，`like` / `not like` 请尽量使用简单表达式。
