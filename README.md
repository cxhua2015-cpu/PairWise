# shardbalance

并发安全的内存型分片负载账本。原子批次按输入顺序执行 Add/Set/Delete，失败整体回滚。详见 `SPEC.md`。

## 设计说明

- **索引**：账户主存储为 `map[string]Account`（按名称 O(1) 定位）。不维护持久有序索引；`Top` 与 `Snapshot` 在读取时把 map 拷入切片并排序（分别按 值降序/名称升序 与 名称升序），保证返回切片与内部状态完全隔离。
- **候选事务**：`Apply` 先对整批 Op 做纯结构校验（kind、名称字符集与字节上限），不触碰状态；随后在账户 map 的副本（候选状态）上按序执行。Add 在算术前检测 int64 溢出并施加 `MaxAbsValue` 绝对值上限，Set 直接校验上限，Delete 缺失即 `ErrNotFound`。账户容量仅在批次末对候选状态检查。任何失败直接丢弃候选，原状态零改动，实现整体回滚；成功时一次性换入候选，非空批次 generation 恰好加一，Add/Set 分配连续 revision。
- **所有权**：`Ledger` 所有公开方法由一把 `sync.RWMutex` 保护（写操作独占，Top/Snapshot 共享读）。返回值（`Result.Changed`、`Top`、`Snapshot.Accounts`）均为新建切片，调用方修改不影响账本；输入的 `Batch` 只读，不会被保留或修改。
- **复杂度**：结构校验 O(批大小 × 名称长度)；候选执行 O(批大小)，候选拷贝 O(账户数)；`Top`/`Snapshot` 为 O(n log n)，n 为账户数；空间 O(账户数 + 批大小)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
