# resourceledger117

并发安全的内存型资源计量账本（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

**索引**：账本主索引为 `map[string]Account`（按名称 O(1) 定位）。`Top` 与 `Snapshot`
不维护有序索引，而是在读取时对账户快照切片按需排序——写路径保持 O(1)，读路径付出
O(n log n)，适合写多读少的计量场景。

**候选事务**：`Apply` 分两阶段。阶段一在加锁前做完整结构校验（kind 合法、名称字符集
与字节上限），不触碰任何状态；阶段二在互斥锁内把当前账户表克隆为候选副本，按输入顺序
在副本上执行 Add/Set/Delete，Add/Set 各自分配连续 revision，算术前先检测 int64 溢出再
执行绝对值上限（`ErrValue`），最终账户容量仅在批次末检查（`ErrCapacity`）。任何一步失败
直接丢弃候选副本，实现整体回滚；全部通过才一次性提交副本、推进 revision 并使
generation 恰好加一（空批次不变）。

**所有权**：所有公开方法由单个 `sync.Mutex` 保护，可并发调用。`Result.Changed`、
`Top`、`Snapshot` 返回的切片均为新建副本，调用方修改不影响内部状态；内部也从不保留
调用方传入的切片。

**复杂度**：设批次长度为 m、账户数为 n。
- `Apply`：结构校验 O(m)，候选克隆 O(n)，执行 O(m)，合计 O(n + m) 时间、O(n) 额外空间。
- `Top(k)`：O(n log n) 时间、O(n) 空间（全量排序后取前 k）。
- `Snapshot`：O(n log n) 时间、O(n) 空间（按名称排序的副本）。
- `New`：O(1)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
