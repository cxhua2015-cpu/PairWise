# metacatalog276

并发安全的内存型元数据目录（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 架构

实现按职责拆分为四个联动文件，共享同一套结构语义与逻辑时钟：

- `servicecatalog.go` — 核心事务引擎：`Store`、`Apply`、`Get`、`Snapshot`。
- `validation.go` — 无副作用的批次预检：`ValidateBatch` 与 `Apply` 共用 `validateOp`/`validateName`，保证“完整结构校验先于状态读取”。
- `stats.go` — `Stats` 在读锁下一次性汇总 generation、nextRevision、记录数与 Value 总字节，结果相对并发事务线性一致。
- `clone.go` — `Clone` 在读锁下深拷贝全部记录与逻辑时钟（generation/nextRevision），副本与原库所有权完全隔离。

## 索引

主索引为 `map[string]entry`（名称 → 值+revision），点查 O(1)。`Snapshot`/`Result.Changed` 的名称排序在读取时按需进行，不维护冗余有序结构。

## 候选事务

`Apply` 先在 `ValidateBatch` 中做纯结构预检（不读状态），然后在写锁内把当前 map 浅拷贝为**候选事务**，按输入顺序执行 Put/Delete：Put 从 `nextRevision` 起连续分配 revision，Delete 不分配、目标缺失即 `ErrNotFound`。记录数与 `MaxTotalValueBytes` 只在批次末对候选终态检查。任一失败直接丢弃候选——原 map、generation、revision 天然回滚；成功则整体换入候选，非空批次 generation 恰好加一。

## 所有权

写入时拷贝调用方传入的 Value；`Get`/`Snapshot`/`Result`/`Clone` 均返回深拷贝。任何返回切片与内部状态互不影响，调用方对返回值的修改不会污染目录。

## 复杂度

- `Get`：O(1)；`Stats`：O(n)。
- `Apply`：O(n + k + c log c)，k 为批内操作数，c 为变更名数（排序）。
- `Snapshot`/`Clone`：O(n log n) / O(n)，含 Value 字节拷贝。
- 并发：读写锁；`Apply` 独占，`Get`/`Snapshot`/`Stats`/`Clone`/`ValidateBatch` 可并行。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
