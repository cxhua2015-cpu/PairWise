# metacatalog266

并发安全的内存型元数据目录（Go 1.22+，仅标准库）。原子批次按输入顺序执行
Put/Delete，Put 分配连续 revision，Delete 不分配；失败回滚全部状态、
generation 与 revision。详见 `SPEC.md`。

## 多文件架构

- `servicecatalog.go`：核心事务引擎（`Store`、`Apply`、`Get`、`Snapshot`）。
- `validation.go`：无副作用的批次结构预检，`Apply` 与 `ValidateBatch`
  共享同一套 `Options.validateOp/validateName` 语义，保证“先完整校验、
  再读取状态”。
- `stats.go`：`Stats` 在读锁下一次性汇总，提供线性一致的状态统计。
- `clone.go`：`Clone` 保留逻辑时钟（generation、nextRevision），并深拷贝
  全部记录，所有权完全隔离。

## 索引

记录存储于 `map[string]entry`（名称为键，entry 含 Value 与 Revision）。
Snapshot/Changed 按名称排序输出，排序在导出时进行，不维护额外有序索引。

## 候选事务

`Apply` 在写锁内先把当前 map 浅拷贝为候选状态，按顺序在候选上应用
Put/Delete；仅当全部操作成功且批次末容量检查（记录数、Value 总字节）
通过时，才整体替换 `s.records` 并推进 generation 与 nextRevision。任何
失败直接丢弃候选，原状态、时钟保持不变，天然回滚。

## 所有权

- 写入时拷贝调用方传入的 Value，存储不别名外部切片。
- `Get`/`Snapshot`/`Result.Changed`/`Clone` 均返回深拷贝的 Value，
  调用方对返回切片的修改不影响内部状态。
- 所有公开方法通过 `sync.RWMutex` 保护，可并发调用；`Stats` 与
  `Clone` 在读锁下与并发事务保持线性一致。

## 复杂度

- `Apply`：O(R + B)，R 为当前记录数（候选拷贝），B 为批次操作数。
- `Get`：O(1) 平均；`Snapshot`：O(R log R)（排序）；`Stats`：O(R)；
  `Clone`：O(R + V)，V 为 Value 总字节；`ValidateBatch`：O(B)，无状态访问。
