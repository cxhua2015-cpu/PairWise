# balanceledger302

并发安全的内存型余额账本（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

- **索引**：核心状态为 `map[string]Account` 哈希索引，按名称 O(1) 定位账户；revision 与 generation 为单调计数器。`Top`/`Snapshot` 不维护有序索引，而是每次调用时对账户快照即时排序，以换取写入路径 O(1)。
- **候选事务**：`Apply` 先在写锁内把当前账户表浅拷贝为候选 map（`Account` 为值类型，拷贝即隔离），按输入顺序在候选上执行 Add/Set/Delete 并分配连续 revision；任一步失败直接丢弃候选，已提交状态零改动，实现整体回滚。账户容量上限仅在候选提交前检查一次。
- **所有权**：`Account` 切片在 `Top`/`Snapshot`/`Result.Changed` 中均为新建副本，调用方修改返回值不影响内部状态；内部 map 在提交时整体替换，旧 map 不再被账本引用。
- **并发**：单把 `sync.RWMutex` 保护全部状态；`Apply` 持写锁，`Top`/`Snapshot` 持读锁，所有公开方法可并发调用（`-race` 验证通过）。
- **复杂度**：`Apply` 为 O(n + a)，n 为批内操作数、a 为当前账户数（候选拷贝）；`Top` 为 O(a log a)，`Snapshot` 为 O(a log a)；空间 O(a)。溢出在算术前用边界比较检测，绝对值上限在每步算术后立即校验。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
