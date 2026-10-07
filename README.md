# balanceledger307

并发安全的内存型余额账本（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 用法

```go
l, _ := balanceledger307.New(balanceledger307.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
res, _ := l.Apply(balanceledger307.Batch{Ops: []balanceledger307.Op{{Kind: balanceledger307.Add, Name: "alpha", Delta: 7}}})
top, _ := l.Top(10)
snap := l.Snapshot()
```

## 设计说明

**索引**：账本主体是 `map[string]Account`，按名称 O(1) 定位。`Top` 与 `Snapshot`
在读取时对账户做一次性收集并排序（分别为值降序/名称升序、纯名称升序），
不维护额外的有序索引——账户数受 `MaxAccounts` 上限约束，排序成本可控。

**候选事务**：`Apply` 先对整批 Op 做纯结构校验（kind、名称字符集与字节长度），
不触碰状态；随后在互斥锁内把当前账户表浅拷贝为候选 map，按输入顺序在其上
执行 Add/Set/Delete。Add/Set 从 `nextRevision` 分配连续 revision；加法在算术前
检测 int64 溢出，结果立即做绝对值上限检查；Delete 缺失账户即 `ErrNotFound`。
任一步失败直接丢弃候选 map，状态零改动（整体回滚）。账户容量仅在批次末对
候选表检查，因此“先删后建”的批次可以成功。全部通过后候选表原子替换正式表，
非空批次 generation 恰好加一。

**所有权**：所有公开方法由一把 `sync.Mutex` 保护，可并发调用。`Result.Changed`、
`Top`、`Snapshot` 返回的切片与 `Account` 值均为新分配的副本，调用方修改不影响
内部状态；内部 map 在提交后不再被旧批次引用。

**复杂度**（n = 账户数，b = 批内 Op 数，k = Top 参数）：
- `Apply`：结构校验 O(b·L)（L 为名称长度），执行 O(b·n) 拷贝 + O(b) 操作，空间 O(n)。
- `Top`：O(n log n) 排序 + O(k) 拷贝。
- `Snapshot`：O(n log n)。
- `New`：O(1)。

## 验证

```
go test ./...
go test -race ./...
go run ./cmd/demo
```
