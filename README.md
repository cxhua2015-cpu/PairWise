# resourceledger197

并发安全的内存型资源计量账本。原子批次按输入顺序执行 `Add`/`Set`/`Delete`，
`Add`/`Set` 分配连续 revision，失败整体回滚。仅依赖标准库，Go 1.22+。

## 设计要点

- **索引**：账户存于 `map[string]Account`（按名 O(1) 查找）。`Top` 与
  `Snapshot` 在读取时按（值降序、名升序）或名称排序后返回，不维护额外有序索引，
  以避免写路径的额外开销与一致性负担。
- **候选事务**：`Apply` 先对整批做结构校验（kind、名称字符集与长度），再在
  当前状态的拷贝（候选 map）上顺序执行全部操作；算术前检测 int64 溢出并执行
  `MaxAbsValue` 绝对值上限，账户容量仅在批次末检查。任一步失败直接丢弃候选，
  原状态零改动；全部成功才一次性提交，且非空批次 `generation` 只增一次。
- **所有权**：所有公开方法返回的切片（`Result.Changed`、`Top`、`Snapshot`）
  均为新分配的拷贝，与内部状态完全隔离；调用方修改返回值不影响账本。
- **并发**：单把 `sync.RWMutex`；`Apply` 持写锁，`Top`/`Snapshot` 持读锁。
  结构校验在加锁前完成，缩短临界区。
- **复杂度**：`Apply` 为 O(k)（k 为批内操作数，外加 O(n) 的候选拷贝）；
  `Top` 为 O(n log n)；`Snapshot` 为 O(n log n)（按名排序）；空间 O(n)。

## 使用

```go
l, _ := resourceledger197.New(resourceledger197.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
r, _ := l.Apply(resourceledger197.Batch{Ops: []resourceledger197.Op{
    {Kind: resourceledger197.Add, Name: "alpha", Delta: 7},
}})
top, _ := l.Top(10)
snap := l.Snapshot()
```

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
