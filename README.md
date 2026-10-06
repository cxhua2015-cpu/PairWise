# balanceledger202

并发安全的内存型余额账本（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

**索引**：账户状态存放在 `map[string]Account` 哈希索引中，按名称 O(1) 定位。`Top` 与 `Snapshot` 不加额外有序索引，而是在读锁内对当前账户快照排序（`Top` 按数值降序、名称升序；`Snapshot` 按名称升序），以换取写入路径的常数复杂度。

**候选事务**：`Apply` 先在无锁状态下对整个批次做结构校验（kind、名称字符集与字节上限），再在写锁内把账户表浅拷贝为候选副本，按输入顺序在其上执行 Add/Set/Delete。Add 在算术前检测 int64 溢出，Add/Set 立即执行绝对值上限检查；Delete 缺失账户返回 `ErrNotFound`；最终账户容量仅在批次末尾检查。任一步失败直接丢弃候选副本，实现整体回滚，generation 与 revision 均不前进。

**所有权**：所有公开方法由一把 `sync.RWMutex` 保护，可并发调用。`Result.Changed`、`Top`、`Snapshot` 返回的切片均为新分配的副本，调用方修改不会影响内部状态。非空成功批次 generation 只增加一次，revision 连续分配；空批次与失败批次不改变任何状态。

**复杂度**：设批次含 k 个 op、当前 n 个账户。`Apply` 为 O(n + k)（候选拷贝 + 顺序执行）；`Top` 为 O(n log n)；`Snapshot` 为 O(n log n)；`New` 为 O(1)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
