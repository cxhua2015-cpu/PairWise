# resourceledger182

并发安全的内存型资源计量账本（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计说明

- **索引**：唯一索引是 `map[string]Account`，按名称 O(1) 定位账户。`Top` 与
  `Snapshot` 不维护有序索引，而是每次调用时对当前账户快照排序（分别按
  值降序/名称升序、名称升序），以换取写入路径 O(1) 与实现的简单性。
- **候选事务**：`Apply` 分两阶段。第一阶段在加锁前做纯结构校验（kind、名称
  字符集与长度），不读取任何状态；第二阶段在写锁内把 `accounts` 复制为候选
  map，按输入顺序执行 Add/Set/Delete，revision 从候选计数器连续分配。任何
  失败（`ErrValue`/`ErrNotFound`/`ErrCapacity`）直接丢弃候选，原状态、
  generation 与 revision 计数器完全不变，实现整体回滚。账户容量上限只在
  批次末对候选集检查一次；int64 溢出在每次算术前检测，绝对值上限逐操作
  生效。成功后候选整体换入，generation 恰好加一。
- **所有权**：`Ledger` 内部状态绝不外借。`Result.Changed`、`Top`、`Snapshot`
  返回的切片均为新建副本，调用方修改返回值不影响账本；`New` 复制
  `Options`，之后外部修改亦无效。
- **并发**：单把 `sync.RWMutex` 保护全部状态。`Apply` 持写锁，`Top` 与
  `Snapshot` 持读锁，可并发执行。
- **复杂度**：设批次含 `k` 个操作、账户数为 `n`。`Apply` 结构校验 O(k)，
  候选复制 O(n)，执行 O(k)，合计 O(n+k) 时间、O(n) 额外空间；`Top` 与
  `Snapshot` 均为 O(n log n) 时间、O(n) 额外空间。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
