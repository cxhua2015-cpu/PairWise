# resourceledger157

并发安全的内存型资源计量账本（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 用法

```go
l, _ := resourceledger157.New(resourceledger157.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
res, _ := l.Apply(resourceledger157.Batch{Ops: []resourceledger157.Op{
    {Kind: resourceledger157.Add, Name: "alpha", Delta: 7},
}})
top, _ := l.Top(10)
snap := l.Snapshot()
```

## 设计说明

- **索引**：单一 `map[string]Account` 哈希索引，按名称 O(1) 定位账户；`Top`/`Snapshot` 在读取时拷贝并排序，不维护额外的有序结构，避免写路径的额外开销。
- **候选事务**：`Apply` 先对整个批次做完整结构校验（kind、名称字符集与字节上限、多余字段），再在 `staging` 副本上按输入顺序模拟 Add/Set/Delete；任一步失败（溢出、绝对值上限、`ErrNotFound`、批次末容量超限）直接丢弃暂存区，整体回滚，已提交的 generation 与 revision 不受影响。
- **所有权**：所有公开方法共享一把 `sync.Mutex`；返回的切片（`Result.Changed`、`Top`、`Snapshot.Accounts`）均为新建副本，调用方修改不会影响内部状态。
- **revision / generation**：Add/Set 各消耗一个连续 revision；非空成功批次 generation 恰好 +1；空批次与失败批次两者都不变。
- **复杂度**：批次校验与模拟 O(k)（k 为 op 数），容量检查 O(u)（u 为涉及账户数）；`Top` 为 O(n log n)，`Snapshot` 为 O(n log n)，n 为账户总数。

## 测试

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
