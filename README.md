# balanceledger362

并发安全的内存型余额账本（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计说明

- **索引**：账户主索引为 `map[string]Account`（按名称 O(1) 定位）。`Top` 与 `Snapshot` 不维护有序索引，而是按需对当前账户集合排序——写入路径因此保持 O(1) 摊销，读路径为 O(n log n)。
- **候选事务**：`Apply` 分三阶段。先在锁外对整批做完整结构校验（kind、名称字符集与字节上限），再在互斥锁内把主索引克隆为候选 map，按输入顺序在其上执行 Add/Set/Delete（Add/Set 分配连续 revision，算术前检测 int64 溢出与绝对值上限），最后仅在批次末检查账户容量。任一阶段失败直接丢弃候选 map，主状态零改动，实现整体回滚；成功则原子换入候选 map，非空批次 generation 恰好加一。
- **所有权**：`Ledger` 独占其内部 map；`Result.Changed`、`Top`、`Snapshot` 返回的切片均为新建副本，调用方修改不会影响内部状态。所有公开方法由同一把 `sync.Mutex` 保护，可并发调用。
- **复杂度**：设批次长度 b、账户数 n。`Apply` 为 O(n + b)（克隆候选 map + 顺序执行）；`Top(k)` 与 `Snapshot` 为 O(n log n)；`New` 为 O(1)。

## 验证

```
go test ./...
go test -race ./...
go run ./cmd/demo
```
