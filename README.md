# tokenledger

并发安全的内存型令牌计数账本（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

- **索引**：账户存储为 `map[string]Account`，按名称 O(1) 定位。`Top`/`Snapshot` 在读取时物化切片并排序，不维护有序索引——写路径保持 O(1) 均摊，读路径按结果规模付费。
- **候选事务**：`Apply` 先在持锁状态下把当前 map 浅拷贝为候选副本，按输入顺序在副本上执行 Add/Set/Delete；任一步失败（`ErrInvalidInput`/`ErrNotFound`/`ErrValue`/`ErrCapacity`）直接丢弃副本，实现整体回滚。容量检查只在批次末尾对候选副本执行一次，因此批次中间允许临时超容。全部通过后原子换入副本，generation 恰好加一，revision 连续分配（Delete 不消耗 revision）。
- **所有权**：`Ledger` 内部状态只通过互斥锁保护下的私有 map 持有；`Result.Changed`、`Top`、`Snapshot` 返回的切片均为新建副本，调用方修改不会影响账本，可安全跨 goroutine 使用。所有公开方法共用一把 `sync.Mutex`，可并发调用。
- **校验顺序**：批次先做完整结构校验（kind、名称字符集与字节上限），不读取任何状态；随后才在候选副本上执行。Add 在算术前检测 int64 溢出，Add/Set 的结果立即执行绝对值上限（`math.MinInt64` 不可表示，一律拒绝）。
- **复杂度**：设批次含 `k` 个 op、账户总数 `n`。`Apply` 为 O(n + k)（候选拷贝 + 顺序执行），`Changed` 排序 O(k log k)；`Top(m)` 与 `Snapshot` 为 O(n log n)；`New` 为 O(1)。空批次 O(1) 且不改变 generation。

## 使用

```go
l, _ := tokenledger.New(tokenledger.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
r, _ := l.Apply(tokenledger.Batch{Ops: []tokenledger.Op{{Kind: tokenledger.Add, Name: "alpha", Delta: 7}}})
top, _ := l.Top(10)
snap := l.Snapshot()
```

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
