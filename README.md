# balanceledger347

并发安全的内存型余额账本（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

**索引**：账本以 `map[string]Account` 作为主索引，按名称 O(1) 定位账户。`Top` 与 `Snapshot` 在读取时全量拷贝并排序，不维护辅助有序结构——写路径保持 O(1)，读路径以排序换取实现的简单与无锁竞争下的正确性。

**候选事务**：`Apply` 分两阶段。阶段一在加锁前完成全部结构校验（kind、名称字符集与长度、输入绝对值上限），不读取任何状态。阶段二在写锁内把当前账户表浅拷贝为候选 map，按输入顺序在其上执行 Add/Set/Delete：Add/Set 分配连续递增的 revision，Add 在算术前检测 int64 溢出，每步执行绝对值上限检查，Delete 缺失账户即失败。任一步失败直接丢弃候选 map，主状态零改动，实现整体回滚；仅在批次末尾检查最终账户容量，全部通过后才以候选 map 原子替换主索引，generation 恰好加一。

**所有权**：所有公开方法返回的切片（`Result.Changed`、`Top`、`Snapshot.Accounts`）均为新分配的副本，调用方修改不会影响内部状态；内部状态仅在持有 `sync.RWMutex` 写锁时被替换，读路径（`Top`/`Snapshot`）使用读锁并发执行。

**复杂度**：`Apply` 为 O(a + k)，其中 a 为当前账户数（候选拷贝）、k 为批内操作数；`Top` 与 `Snapshot` 为 O(a log a)；`New` 为 O(1)。空间为 O(a)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
