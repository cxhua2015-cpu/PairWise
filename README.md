# balanceledger307

并发安全的内存型余额账本（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

**索引**
- 账户主索引为 `map[string]Account`，按名称 O(1) 定位。
- `Top` 与 `Snapshot` 不维护有序索引，而是在读锁下复制条目后排序：
  `Top` 按值降序、名称升序；`Snapshot` 按名称升序。写路径因此保持 O(1) 摊销。

**候选事务（candidate transaction）**
- `Apply` 先做完整结构校验（kind、名称字符集与字节上限），不触碰状态。
- 随后在写锁内把当前账户表浅拷贝为候选 map，按输入顺序在其上执行
  Add/Set/Delete；Add/Set 从局部 revision 计数器分配连续 revision。
- 溢出在算术之前检测（`base > MaxInt64-d` 等），Delta/结果值均受
  `MaxAbsValue` 绝对值上限约束；账户容量仅在批次末对候选表检查。
- 任一步失败直接返回，候选表与计数器被丢弃，实现整体回滚；
  全部成功才一次性提交（替换 map、推进 generation 与 nextRevision）。
- 非空成功批次 generation 恰好 +1；空批次不改变任何计数器。

**所有权**
- 所有公开方法通过 `sync.RWMutex` 保护，可并发调用。
- 返回值（`Result.Changed`、`Top`、`Snapshot.Accounts`）均为新分配的切片，
  账户为值类型，调用方修改不会影响内部状态；内部 map 只在写锁下整体替换。

**复杂度**（n = 账户数，k = 批次数，m = Top 请求数）
- `Apply`：O(n + k)，候选拷贝 O(n)，逐 op O(1)。
- `Top`：O(n log n) 排序后取前 m。
- `Snapshot`：O(n log n) 按名称排序。
- 空间：O(n)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
