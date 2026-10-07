# balanceledger387

并发安全的内存型余额账本（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

**索引**
- 主索引为 `map[string]Account`，按账户名 O(1) 定位。
- `Top` 与 `Snapshot` 不维护有序索引，而是在读取时对当前账户集合即时排序：
  `Top` 按数值降序、名称升序；`Snapshot` 按名称升序。账户数通常为控制面规模，
  排序开销可接受，且避免写路径维护额外结构。

**候选事务（candidate transaction）**
- `Apply` 先对整个批次做纯结构校验（kind、名称字符集与字节上限），不读取任何状态。
- 随后在账户映射的克隆（候选副本）上按输入顺序执行 Add/Set/Delete：
  Add/Set 在算术前检测 int64 溢出并执行 `MaxAbsValue` 绝对值上限，
  每次 Add/Set 分配连续递增的 revision；Delete 要求账户存在。
- 仅当全部操作成功且批次末账户数不超过 `MaxAccounts` 时，候选副本整体替换
  正式状态，generation 恰好加一；任一步失败直接丢弃候选副本，实现整体回滚，
  generation 与 revision 均不变。空批次为无操作，不改变 generation。

**所有权与并发**
- `Ledger` 内部状态由单一 `sync.Mutex` 保护，所有公开方法可并发调用。
- 写路径在持锁期间完成候选事务的构建与提交；读路径持锁拷贝后排序。
- 所有返回的切片（`Result.Changed`、`Top`、`Snapshot.Accounts`）均为新分配的
  副本，调用方修改不会影响内部状态；`Account` 为纯值类型，无共享指针。

**复杂度**（n = 账户数，b = 批次内操作数，k = Top 请求数量）
- `New`：O(1)。
- `Apply`：结构校验 O(b)，候选克隆 O(n)，执行 O(b)，合计 O(n + b) 时间、O(n) 额外空间。
- `Top`：O(n log n) 时间、O(n) 空间，返回 O(k)。
- `Snapshot`：O(n log n) 时间、O(n) 空间。

## 使用

```go
l, _ := balanceledger387.New(balanceledger387.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
r, _ := l.Apply(balanceledger387.Batch{Ops: []balanceledger387.Op{{Kind: balanceledger387.Add, Name: "alpha", Delta: 7}}})
```

## 验证

```
go test ./...
go test -race ./...
go run ./cmd/demo
```
