# metacatalog261

并发安全的内存型元数据目录（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 架构

实现按职责拆分为四个联动文件，共享同一套结构语义与逻辑时钟：

- `servicecatalog.go`：核心事务引擎。`Store` 持有 `sync.RWMutex`、记录索引、`generation`/`revision` 逻辑时钟与 Value 总字节计数；`Apply` 执行原子批次，`Get`/`Snapshot` 返回深拷贝。
- `validation.go`：无副作用的批次预检。`ValidateBatch` 与 `Apply` 共用 `validateBatch`/`validateName`，只读取不可变的 `Options`，绝不触碰状态，保证“完整结构校验先于状态读取”。
- `stats.go`：`Stats` 在读锁下一次性采样 generation、NextRevision、记录数与 Value 总字节，提供线性一致的摘要。
- `clone.go`：`Clone` 在读锁下复制全部记录（Value 逐条深拷贝）与逻辑时钟，产出所有权完全独立的 `Store`。

## 索引

记录存储为 `map[string]Record` 哈希索引，按名称 O(1) 定位；`Snapshot`/`Result.Changed` 在读取时按名称排序，不维护额外的有序结构。`totalValue` 计数器随每次 Put/Delete 增量维护，使容量检查与 `Stats` 均为 O(1)。

## 候选事务

`Apply` 在写锁内先运行共享预检，再在候选副本（`map` 浅拷贝 + 新 Value 分配）上按输入顺序执行 Put/Delete：Put 分配连续 revision，Delete 不分配。记录数与 Value 总字节容量只在批次末检查；任何失败（`ErrNotFound`/`ErrCapacity`）直接丢弃候选，状态、generation、revision 全部回滚。成功时一次性提交候选并将 generation 恰好加一；空批次不改变 generation。

## 所有权

所有跨越 API 边界的 Value 都做深拷贝：Put 输入在提交时拷贝，`Get`/`Snapshot`/`Result.Changed`/`Clone` 的输出各自持有独立切片，调用方对返回值的修改不会影响内部状态，反之亦然。`Clone` 保留逻辑时钟（generation、revision），与原 Store 完全隔离。

## 复杂度

- `Apply`：O(n + m + k log k)，n 为批次数，m 为现有记录数（候选拷贝），k 为变更名数。
- `Get`：O(1)（加 Value 拷贝）；`ValidateBatch`：O(n)，无状态读取。
- `Stats`：O(1)；`Snapshot`：O(m log m)；`Clone`：O(m)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
