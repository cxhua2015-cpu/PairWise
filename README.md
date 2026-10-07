# balanceledger317

并发安全的内存型余额账本（Go 1.22+，仅标准库）。原子批次按输入顺序执行
Add/Set/Delete，失败整体回滚；Top 按数值降序、名称升序，Snapshot 按名称排序。

## 设计说明

- **索引**：账本主体是 `map[string]Account` 哈希索引，按名称 O(1) 定位账户。
  不做有序索引；Top/Snapshot 在读取时物化切片并排序，以换取写入路径的常数复杂度。
- **候选事务**：`Apply` 先做整批结构校验（kind、名称字符集与字节上限），再在
  账户表的候选副本上按序执行操作。Add 在加法前检测 int64 溢出，Add/Set 立即执行
  绝对值上限检查并分配连续 revision；最终账户容量仅在批次末检查。任一步失败直接
  丢弃候选副本，generation、nextRevision 与账户表全部保持原状，实现整体回滚。
- **所有权**：所有公开方法由单个 `sync.Mutex` 串行化，可并发调用。返回的切片
  （`Result.Changed`、`Top`、`Snapshot.Accounts`）均为新建副本，调用方修改不会
  影响内部状态；空批次成功但不推进 generation。
- **复杂度**：批次校验与执行 O(k)（k 为操作数），候选复制 O(n)（n 为账户数）；
  `Top` 与 `Snapshot` 为 O(n log n) 排序；空间 O(n + k)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
