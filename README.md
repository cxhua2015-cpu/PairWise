# balanceledger212

并发安全的内存型余额账本（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

**索引**：账本以 `map[string]Account` 为主索引，按名称 O(1) 定位账户。`Top` 与 `Snapshot` 在读取时物化切片并排序，不维护额外的有序结构，因此写入路径保持 O(1) 摊销。

**候选事务**：`Apply` 先对整个批次做纯结构校验（kind、名称字符集与字节上限），不触碰状态；随后在互斥锁内把账户表浅拷贝为候选 map，按输入顺序应用 Add/Set/Delete。Add/Set 在算术前检测 int64 溢出并执行 `MaxAbsValue` 绝对值上限；Delete 缺失即 `ErrNotFound`。最终账户容量仅在批次末对候选 map 检查。任一步失败直接丢弃候选，状态、generation、revision 完全不变（整体回滚）；全部成功才一次性提交候选并令 generation 增一（空批次不增）。

**所有权**：所有公开方法由 `sync.RWMutex` 保护，可并发调用。写操作独占锁，读操作（`Top`/`Snapshot`）共享读锁。返回的切片（`Result.Changed`、`Top`、`Snapshot.Accounts`）均为每次调用新建的副本，调用方修改不会影响内部状态，内部后续变更也不会影响已返回的切片。

**复杂度**：设批次含 k 个 op、账户总数为 n、Top 参数为 m。`Apply` 结构校验 O(k)，候选拷贝 O(n)，应用 O(k)，总计 O(n+k) 时间、O(n) 额外空间；`Top` O(n log n) 时间、O(n) 空间，返回前 m 条；`Snapshot` O(n log n) 时间、O(n) 空间；`New` O(1)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
