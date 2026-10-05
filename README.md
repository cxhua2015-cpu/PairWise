# resourcecatalog181

并发安全的内存型资源目录（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

- **索引**：`Store` 内部使用 `map[string]Record` 作为主索引，按名称 O(1) 定位；另维护 `generation`、`nextRevision` 与 `totalValue`（Value 总字节）三个计数器，避免每次容量检查时遍历全表。
- **候选事务**：`Apply` 分三个阶段——(1) 对整个批次做完整结构校验（kind、名称字符集与长度、Value 长度、Delete 不得携带 Value），不读任何状态；(2) 在索引的克隆副本上按输入顺序执行 Put/Delete，Put 分配连续 revision，Delete 不分配；(3) 仅在批次末检查最终记录数与 Value 总字节上限。任一阶段失败直接丢弃候选副本，`generation`、`nextRevision` 与索引全部保持不变，实现天然回滚；成功时一次性换入候选副本，非空批次 `generation` 只加一。
- **所有权**：Put 时深拷贝调用方传入的 Value；`Get`/`Snapshot`/`Result.Changed` 返回的 Record 均携带独立副本，返回切片与内部状态完全隔离，调用`Snapshot` 记录按名称排序。
- **并发**：所有公开方法通过单把 `sync.Mutex` 串行化，批次原子生效，支持任意并发调用。
- **复杂度**：结构校验 O(批次大小)；候选执行 O(批次大小 + 克隆索引 O(n))；容量检查 O(1)；`Get` O(1)；`Snapshot` O(n log n)（排序）。`Apply` 的空间开销为 O(n + 批次大小)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
