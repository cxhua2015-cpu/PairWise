# metacatalog236

并发安全的内存型元数据目录（Go 1.22+，仅标准库）。原子批次按输入顺序执行 Put/Delete；Put 分配连续 revision，Delete 不分配；失败时完整回滚状态、generation 与 revision。

## 架构

- `servicecatalog.go` — 核心事务引擎：`New`/`Apply`/`Get`/`Snapshot`。
- `validation.go` — 无副作用的批次结构预检 `ValidateBatch`，与 `Apply` 共享同一套结构语义（先完整校验，再读取状态）。
- `stats.go` — 线性一致的状态统计 `Stats`（读锁下的原子快照）。
- `clone.go` — 保留逻辑时钟（generation/nextRevision）且所有权完全隔离的深拷贝 `Clone`。

## 索引

主索引为 `map[string]Record`（按名称哈希索引），`Get` 为 O(1)。`Snapshot`/`Result.Changed` 按名称排序输出，排序为 O(n log n)。

## 候选事务

`Apply` 在单个写锁内：先对批次做完整结构校验（不读状态），再在候选副本 `next`（当前记录的浅拷贝 + 按序应用操作）上执行 Put/Delete。Delete 命中不存在键即返回 `ErrNotFound`；最终记录数与 Value 总字节容量只在批次末检查，超限返回 `ErrCapacity`。任何失败直接丢弃候选，状态与逻辑时钟零变更；成功才一次性提交并令 generation 递增一次（空批次不递增）。

## 所有权

写入时拷贝调用方传入的 Value；`Get`/`Snapshot`/`Result.Changed` 均返回深拷贝，返回切片与内部状态完全隔离。`Clone` 深拷贝全部记录与逻辑时钟，克隆体与原 store 互不影响。

## 并发与复杂度

- 单把 `sync.RWMutex`：写操作互斥，读操作（`Get`/`Snapshot`/`Stats`）并行；`ValidateBatch` 只读配置、无锁。
- `Apply`：O(b + n)，b 为批大小、n 为当前记录数（候选复制）。
- `Get`：O(1)；`Snapshot`/`Clone`：O(n log n) / O(n)；`Stats`：O(n)。
- 校验：名称非空、仅 `[a-z0-9_-]`、受 `MaxNameBytes` 限制；Delete 不得携带 Value；未知 kind 返回 `ErrInvalidInput`。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
