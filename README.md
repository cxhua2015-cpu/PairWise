# resourceledger092

并发安全的内存型资源计量账本（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计说明

- **索引**：账户主存为 `map[string]Account` 哈希索引，按名 O(1) 定位；`Top`/`Snapshot` 在读取时现排序，不维护冗余有序结构，避免写路径额外开销。
- **候选事务**：`Apply` 先对整批做纯结构校验（kind、名称字符集与字节上限），再在写锁内把 Add/Set/Delete 暂存到覆盖层（`staged` 写集 + `deleted` 删集），读取先查覆盖层再回落到已提交状态。任一步失败（`ErrValue`/`ErrNotFound`/`ErrCapacity`）直接丢弃暂存，已提交状态零改动，实现整体回滚；全部成功才一次性提交。revision 在暂存阶段连续分配，提交时推进 `nextRevision`；int64 溢出在加法前用边界比较检测，绝对值上限逐步执行，账户容量仅按批次末净额检查。
- **所有权**：`Ledger` 内部状态不出借。`Result.Changed`、`Top`、`Snapshot` 均返回新建切片与值拷贝，调用方修改不影响账本；`Ledger` 零值不可用，必须经 `New` 构造。
- **并发**：单把 `sync.RWMutex`；`Apply` 持写锁，`Top`/`Snapshot` 持读锁可并行。
- **复杂度**：批次 m 条 op、当前 n 个账户时，`Apply` 为 O(m) 期望时间 + O(m) 暂存空间（容量检查 O(m)）；`Top(k)` 与 `Snapshot` 为 O(n log n) 时间、O(n) 空间。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
