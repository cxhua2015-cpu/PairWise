# balanceledger377

并发安全的内存型“余额账本”，Go 1.22+，仅依赖标准库。语义详见 `SPEC.md`。

## 设计说明

### 索引
账本核心是一个 `map[string]Account` 哈希索引，按账户名 O(1) 定位。
`Top` 与 `Snapshot` 不维护有序索引，而是在读锁内对当前账户快照按需排序：
账户数通常远小于操作频率，避免了写路径上维护堆/树的开销，写操作保持 O(1) 均摊。

### 候选事务（candidate transaction）
`Apply` 先做**完整结构校验**（kind 合法、名称字符集与长度），不触碰任何状态；
随后在写锁内把账户表浅拷贝为候选副本，按输入顺序在副本上执行 Add/Set/Delete：

- Add/Set 在算术前检测 int64 溢出（`math.MaxInt64/MinInt64` 边界比较），
  并对结果执行 `MaxAbsValue` 绝对值上限；
- 每个 Add/Set 从 `nextRevision` 起分配连续 revision，Delete 不消耗 revision；
- 账户容量 `MaxAccounts` 仅在批次末对候选副本检查（允许批内先增后删的净零churn）；
- 任一步失败直接返回，候选副本被丢弃——天然整体回滚，主状态、generation、
  revision 计数器均不变。

全部成功才一次性交换候选副本为主状态，并将 generation 递增一次（空批次不变）。

### 所有权与并发
`Ledger` 内嵌 `sync.RWMutex`：`Apply` 持写锁，`Top`/`Snapshot` 持读锁可并行。
所有返回的切片（`Result.Changed`、`Top`、`Snapshot.Accounts`）都是新分配的拷贝，
调用方对返回值的任何修改不会泄漏进账本内部状态；`Account` 为纯值类型，无共享指针。

### 复杂度
设批次含 k 个 op，账户总数 n：

- `Apply`：结构校验 O(k)，候选拷贝 O(n)，执行 O(k)，总计 O(n + k)；
- `Top(m)`：O(n log n) 排序 + O(m) 拷贝；
- `Snapshot`：O(n log n) 排序；
- 空间：O(n)，候选事务期间瞬时 O(n) 额外副本。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
