# resourceledger182

并发安全的内存型资源计量账本（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计说明

**索引**
- 主索引为 `map[string]Account`，按名称 O(1) 定位账户。
- `Top` 与 `Snapshot` 不维护有序索引，而是按需对当前账户做一次性排序：
  `Top` 按值降序、名称升序；`Snapshot` 按名称升序。账户数为 n 时排序成本
  O(n log n)，避免了写路径上维护堆/树的开销，写操作保持近似 O(1)。

**候选事务（candidate transaction）**
- `Apply` 先对整个批次做纯结构校验（kind 合法、名称字符集与字节上限），
  不触碰任何状态。
- 随后在互斥锁内把主索引浅拷贝为候选 map，按输入顺序在其上执行
  Add/Set/Delete：Add/先做 int64 溢出预检再求和，Add/Set 立即执行绝对值
  上限检查并分配连续 revision；批次末才检查最终账户容量。
- 任一步失败直接丢弃候选 map，主状态、generation、revision 完全不变，
  实现整体回滚；全部成功才一次性提交，非空成功批次 generation 恰好 +1。

**所有权与并发**
- 所有公开方法经同一把 `sync.Mutex` 串行化，可任意并发调用。
- 返回的 `[]Account`（`Result.Changed`、`Top`、`Snapshot.Accounts`）均为
  新分配的切片与值拷贝，调用方修改不会影响账本内部状态，反之亦然。

**复杂度**（n = 账户数，b = 批次内 op 数）
- `Apply`：时间 O(n + b)，空间 O(n + b)（候选拷贝 + changed 列表）。
- `Top(k)`：时间 O(n log n)，空间 O(n)。
- `Snapshot`：时间 O(n log n)，空间 O(n)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
