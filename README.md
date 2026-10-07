# balanceledger397

并发安全的内存型余额账本（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计说明

- **索引**：账户主存为 `map[string]Account`（按名称 O(1) 定位）。`Top` 与
  `Snapshot` 不维护持久有序索引，而是在读取时复制账户切片并排序——写路径
  保持 O(1)，读路径以一次性排序换取实现的简单与无锁竞争外的正确性。
- **候选事务**：`Apply` 先做整批结构校验（kind、名称字符集与字节上限），
  再在账户表的克隆（候选状态）上按输入顺序执行 Add/Set/Delete。任何
  溢出、绝对值越限、Delete 缺失或批次末容量两年检失败都直接丢弃候选，
  实现零成本整体回滚；成功时才用候选原子替换主存并提交。
- **所有权**：所有公开方法返回的切片（`Result.Changed`、`Top`、`Snapshot.
  Accounts`）均为新建副本，调用方修改不会影响账本内部状态；内部状态仅在
  持锁期间被访问。
- **并发**：单把 `sync.RWMutex` 保护全部状态；`Apply` 持写锁，`Top`/
  `Snapshot` 持读锁，可并发执行。
- **复杂度**：设批次含 k 个 op、账本有 n 个账户。`Apply` 校验 O(k·L)
  （L 为名称长度），执行 O(k)，候选克隆 O(n)，容量检查 O(1)；`Top` 与
  `Snapshot` 均为 O(n log n)（复制 + 排序）。

## 校验

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
