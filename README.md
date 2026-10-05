# resourceledger187

并发安全的内存型资源计量账本（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计说明

- **索引**：`Ledger` 内部以 `map[string]Account` 作为唯一主索引，按账户名 O(1) 定位；`Top` 与 `Snapshot` 在读取时对快照副本排序，不维护额外的有序结构，避免写路径的额外开销。
- **候选事务**：`Apply` 先对整批 Op 做完整结构校验（kind、名称字符集与字节上限），再在账户表的**拷贝**（candidate map）上按输入顺序执行 Add/Set/Delete。溢出（算术前检测）与绝对值上限逐条检查，最终账户容量仅在批次末检查。任一步失败直接丢弃候选 map，实现零成本整体回滚；成功时一次性交换 map 指针并提交。
- **所有权**：所有公开方法由单个 `sync.Mutex` 保护；`Top`/`Snapshot`/`Result.Changed` 返回的都是新建切片与值类型副本，调用方对返回值的任何修改不会影响账本内部状态。
- **revision / generation**：每个 Add/Set 操作按输入顺序分配连续 revision（Delete 不分配）；非空成功批次 generation 只增加一次，空批次不改变任何计数。
- **复杂度**：批次结构校验 O(k·L)（k 为 op 数，L 为名称长度）；候选事务执行 O(k + n)（n 为当前账户数，用于拷贝 map）；`Top` 为 O(n log n)，`Snapshot` 为 O(n log n)（按名称排序）。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
