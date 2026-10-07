# balanceledger342

并发安全的内存型余额账本（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

**索引**：核心状态为 `map[string]Account` 哈希索引，按名称 O(1) 定位账户；
revision 为单调递增计数器（从 1 开始），不维护额外有序结构，`Top`/`Snapshot`
在读取时按需排序，避免写路径上的堆/树维护开销。

**候选事务**：`Apply` 先做整批结构校验（kind、名称字符集与字节上限），再在锁内
把当前账户表浅拷贝为候选副本，按输入顺序在副本上执行 Add/Set/Delete；Add/Set
分配连续 revision。Add 在算术前检测 int64 溢出，结果立即执行绝对值上限
（`ErrValue`）；Delete 缺失账户返回 `ErrNotFound`；账户容量仅在批次末对最终
集合检查（`ErrCapacity`）。任一步失败直接丢弃候选副本，实现整体回滚；成功时
原子换入副本，非空批次 generation 恰好加一。

**所有权**：`Result.Changed`、`Top`、`Snapshot` 返回的切片及其中的 `Account`
值均为新分配的副本，与内部状态完全隔离；调用方修改返回值不影响账本。
`Changed` 按账户首次被触及的顺序去重，记录该账户在批次中的最终状态。

**并发**：所有公开方法共用一把 `sync.Mutex`，写（Apply）与读（Top/Snapshot）
互斥，可安全并发调用。

**复杂度**（n = 批次 op 数，N = 账户总数）：
- `Apply`：结构校验 O(n·L)（L 为名称长度），执行 O(n + N)（候选副本拷贝 O(N)），空间 O(N)。
- `Top(k)`：O(N log N) 排序，空间 O(N)。
- `Snapshot`：O(N log N) 按名称排序，空间 O(N)。
- `New`：O(1)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
