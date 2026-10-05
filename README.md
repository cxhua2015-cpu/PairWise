# resourceledger107

并发安全的内存型资源计量账本。语义见 `SPEC.md`，公开契约见
`resourceledger107/contract_test.go`。

## 设计说明

- **索引**：账本主体是 `map[string]Account` 哈希索引，按名称 O(1) 定位账户。
  `Top` 与 `Snapshot` 在读取时物化切片并排序（分别按值降序/名称升序、按名称升序），
  不维护额外的有序结构，写入路径因此保持 O(1)。
- **候选事务**：`Apply` 先做整批结构校验（kind、名称字符集与字节上限），再在
  当前状态的候选副本（map 浅拷贝）上按输入顺序执行 Add/Set/Delete。Add/Set 在
  候选上分配连续 revision；算术前检测 int64 溢出并执行绝对值上限；最终账户容量
  仅在批次末检查。任一步失败直接丢弃候选，账本状态、generation 与 revision 完全
  不变（整体回滚）；成功时一次性用候选替换内部 map，非空批次 generation 只加一。
- **所有权**：所有公开方法返回的切片（`Result.Changed`、`Top`、`Snapshot.Accounts`）
  均为新分配的副本，调用方修改不影响内部状态；内部 map 只在持锁期间被替换或读取。
- **并发**：单个 `sync.Mutex` 保护全部内部状态。结构校验在锁外完成，临界区仅覆盖
  候选事务与状态替换，读（Top/Snapshot）与写（Apply）互斥，满足并发安全。
- **复杂度**：设批次含 k 个 op、账本含 n 个账户。`Apply` 为 O(n + k)
  （候选拷贝 O(n)，每个 op O(1)，Changed 去重 O(k)）；`Top` 为 O(n log n)；
  `Snapshot` 为 O(n log n)。空间 O(n + k)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
