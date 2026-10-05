# resourceledger107

并发安全的内存型资源计量账本（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

- **索引**：账本主体为 `map[string]Account` 哈希索引，按名称 O(1) 定位账户；
  不维护有序索引，`Top`/`Snapshot` 在读取时物化并排序，写路径保持 O(1)。
- **候选事务**：`Apply` 先对整个批次做纯结构校验（kind、名称字符集与字节上限），
  不触碰状态；随后在账户表的克隆（候选事务）上按输入顺序执行 Add/Set/Delete，
  任何 `ErrValue`/`ErrNotFound`/`ErrCapacity` 直接丢弃克隆，实现整体回滚；
  全部成功且批次末容量检查通过后才原子换入主表，并一次性递增 generation。
- **所有权**：所有公开方法返回的切片均为新建副本，调用方修改不会影响内部状态；
  `Ledger` 内部状态绝不逃逸。并发安全由单一 `sync.Mutex` 保证，写操作串行化。
- **复杂度**：设批次长度为 B、账户数为 N。`Apply` 校验 O(B)，候选克隆 O(N)，
  执行 O(B)，容量检查 O(1)，合计 O(N+B)；`Top(k)` 为 O(N log N)；
  `Snapshot` 为 O(N log N)（按名称排序）；`New` 为 O(1)。
- **数值安全**：Add/Set 在任何算术前拒绝 `math.MinInt64` 及超出 `MaxAbsValue`
  的绝对值，并用边界比较检测 int64 加减溢出，溢出与越限均返回 `ErrValue`。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
