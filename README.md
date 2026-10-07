# balanceledger327

并发安全的内存型余额账本（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 实现说明

- **索引**：账户存于 `map[string]Account`（按名称 O(1) 定位）。`Top`/`Snapshot` 在读取时拷贝并排序，不维护额外有序索引，保证写路径轻量。
- **候选事务**：`Apply` 先在无锁状态下做完整结构校验（kind、名称字符集与字节上限、Delta/Value 绝对值上限），再在写锁内把当前 map 拷贝为候选副本，按输入顺序应用 Add/Set/Delete；任一步失败（ErrNotFound/ErrValue/ErrCapacity）直接丢弃候选，状态、generation、revision 均不变，实现整体回滚。容量上限仅在批次末尾对候选副本检查，因此"先删后建"的批次可通过。
- **所有权**：所有返回的切片（`Result.Changed`、`Top`、`Snapshot.Accounts`）均为新分配的副本，调用方修改不影响账本内部状态；内部 map 只在持写锁时整体替换，旧副本不被复用。
- **并发**：单把 `sync.RWMutex` 保护全部状态；`Apply` 持写锁，`Top`/`Snapshot` 持读锁，所有公开方法可安全并发调用。
- **revision / generation**：Add/Set 各消耗一个连续 revision（Delete 不消耗）；非空成功批次 generation 恰好 +1，空批次与失败批次不变。
- **复杂度**：结构校验 O(L)（L 为批次字节量）；`Apply` 拷贝候选 O(A)、应用 O(O)（A 为账户数、O 为操作数）；`Top` 为 O(A log A)；`Snapshot` 为 O(A log A)。空间 O(A)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
