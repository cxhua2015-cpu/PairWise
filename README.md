# resourceledger167

并发安全的内存型资源计量账本（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计

- **索引**：账户存于 `map[string]Account`（按名称 O(1) 查找）。`Top` 与 `Snapshot`
  在读取时物化并排序，不维护额外有序索引，保证写路径简单、无索引不一致风险。
- **候选事务**：`Apply` 先做完整结构校验（kind、名称字符集与字节上限），再在锁内
  把账户表浅拷贝为候选映射，按输入顺序执行 Add/Set/Delete。任一步失败（溢出、
  绝对值上限、未找到、批次末容量超限）直接丢弃候选，实现整体回滚；成功才一次性
  提交并令 generation 恰好 +1。Add/Set 从 `nextRevision` 分配连续 revision。
- **所有权**：`Ledger` 独占内部映射；`Result.Changed`、`Top`、`Snapshot` 返回的切片
  均为新分配的拷贝，调用方修改不影响内部状态。
- **并发**：单个 `sync.Mutex` 保护全部状态，所有公开方法可并发调用。

## 复杂度

- `Apply`：O(k·n) 拷贝 + O(k) 执行（k 为批内 op 数，n 为账户数）。
- `Top`：O(n log n)；`Snapshot`：O(n log n)（按名称排序）。
- 空间：O(n)，候选事务期间临时 O(n + k)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
