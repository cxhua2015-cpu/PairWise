# metacatalog416

并发安全的内存型元数据目录（Go 1.22+，仅标准库）。原子批次按输入顺序执行 Put/Delete，Put 分配连续 revision，Delete 不分配；失败时完整回滚状态、generation 与 revision。

## 多文件架构

- `servicecatalog.go` — 核心事务引擎：`Store`、`Apply`、`Get`、`Snapshot`。
- `validation.go` — 无副作用的批次预检：`ValidateBatch` 与 `Apply` 共享同一套结构语义（`validateOp` / `validName`），只做纯结构检查，不读取、不修改状态。
- `stats.go` — 线性一致统计：`Stats` 在读锁内汇总 generation、nextRevision、记录数与 Value 总字节，与并发事务状态一致。
- `clone.go` — 所有权安全的深拷贝：`Clone` 复制全部记录与逻辑时钟（generation、nextRevision），克隆体与原 Store 完全隔离。

## 索引

记录存储于 `map[string]entry`（名称 → 值+revision），点查与更新均为 O(1) 均摊。`Snapshot`/`Apply` 的 `Changed` 在返回前按名称排序，排序成本 O(n log n)。

## 候选事务

`Apply` 先在候选 map（当前记录的浅拷贝，Value 切片在 Put 时重新分配）上按顺序重放整个批次：Delete 缺失键即返回 `ErrNotFound`；仅在批次末检查最终记录数与 Value 总字节容量（`ErrCapacity`）。任何失败直接丢弃候选，原状态、generation、revision 不变；成功时整体换入候选，非空批次 generation 恰好加一。

## 所有权

所有进入 Store 的 Value 在 Put 时拷贝，所有离开 Store 的 Value（`Get`、`Snapshot`、`Changed`）均为深拷贝；返回切片与内部状态完全隔离。`Clone` 逐条复制 Value，克隆体后续事务不影响原 Store。

## 并发与复杂度

单把 `sync.RWMutex` 保护全部状态：`Apply` 取写锁，`Get`/`Snapshot`/`Stats`/`Clone` 取读锁，全部公开方法并发安全且线性一致。`Apply` 为 O(n + b)（n 为当前记录数，b 为批次大小），`Get` O(1) 均摊，`Snapshot`/`Stats`/`Clone` O(n)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
