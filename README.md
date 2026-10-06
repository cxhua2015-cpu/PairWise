# metacatalog271

并发安全的内存型元数据目录，实现 `SPEC.md` 中的原子批次语义，仅依赖标准库（Go 1.22+）。

## 架构

实现按职责拆分为四个联动文件：

- `servicecatalog.go`：核心事务引擎。`Apply` 先调用无副作用预检，再在写锁内构建候选事务、提交或整体回滚；`Get`/`Snapshot` 深拷贝返回。
- `validation.go`：`ValidateBatch` 与 `Apply` 共享同一套结构语义（名称字符集与长度、Value 上限、未知 kind、Delete 携带 Value 均为 `ErrInvalidInput`），不读取任何状态。
- `stats.go`：`Stats` 在读锁下返回线性一致的记录数、Value 总字节数与逻辑时钟。
- `clone.go`：`Clone` 保留 generation 与 nextRevision 逻辑时钟，逐条深拷贝记录，所有权完全隔离。

## 索引

记录存储在 `map[string]entry` 哈希索引中，按名称 O(1) 定位；`entry` 持有 Value 副本与分配时的 revision。`totalValue` 作为运行总量随事务增量维护，避免每次统计都全表扫描。

## 候选事务

`Apply` 在写锁内将当前 map 浅拷贝为候选状态，按输入顺序在其上执行 Put/Delete：Put 分配连续 revision（Delete 不分配），Delete 缺失键即返回 `ErrNotFound`。记录数与 Value 总字节容量只在批次末检查，允许中间状态超限。任一步失败直接丢弃候选状态，generation 与 nextRevision 保持不变，实现零成本回滚；成功时整体换入候选 map，非空批次 generation 恰好加一。

## 所有权

所有进出边界的 Value 都做深拷贝：Put 提交前复制输入，`Get`/`Snapshot`/`Result.Changed`/`Clone` 返回独立副本，调用方对返回切片的修改不会污染内部状态，反之亦然。`Clone` 的产物与原 Store 无任何共享内存。

## 复杂度

- `Apply`：O(B + N)，B 为批内操作数，N 为当前记录数（候选拷贝）。
- `Get`：O(1)；`ValidateBatch`：O(B)；`Stats`：O(1)。
- `Snapshot`/`Clone`：O(N log N)（按名称排序）/ O(N)。
- 并发：读写锁保护，读路径（`Get`/`Snapshot`/`Stats`/`Clone`）可并行，写路径（`Apply`）串行化且线性一致。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
