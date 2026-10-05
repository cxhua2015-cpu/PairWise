# resourceledger197

并发安全的内存型资源计量账本（Go 1.22+，仅标准库）。原子批次按输入顺序执行
`Add`/`Set`/`Delete`，失败整体回滚；`Top` 按数值降序、名称升序，`Snapshot` 按名称排序。

## 设计说明

- **索引**：账本主体是 `map[string]Account` 哈希索引，按名称 O(1) 定位账户；
  不维护有序索引，`Top`/`Snapshot` 在读取时物化并排序快照，写路径保持 O(1)。
- **候选事务**：`Apply` 先做整批结构校验（kind、名称字符集与字节上限），再在
  候选副本（账户表的浅拷贝）上按序执行操作。`Add`/`Set` 从 `nextRevision` 分配连续
  revision；int64 溢出在算术之前检测，绝对值上限逐操作执行，账户容量上限仅在批次末
  检查。任一步失败直接丢弃候选，原状态零改动，实现整体回滚；成功时一次性换入候选，
  非空批次 `generation` 恰好加一。
- **所有权**：所有公开方法返回的切片均为新分配的副本，调用方修改返回值不会影响
  内部状态；`Changed`/`Top`/`Snapshot` 之间互不共享底层数组。
- **并发**：单把 `sync.RWMutex` 保护全部状态，`Apply` 取写锁，`Top`/`Snapshot` 取读锁，
  所有公开方法可安全并发调用。
- **复杂度**：`Apply` 为 O(k + a)，k 为批内操作数、a 为当前账户数（候选拷贝）；
  `Top`/`Snapshot` 为 O(a log a)；空间 O(a)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
