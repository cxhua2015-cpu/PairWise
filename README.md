# resourceledger192

并发安全的内存型资源计量账本（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

- **索引**：账本主体为 `map[string]Account` 哈希索引，按名称 O(1) 定位账户；
  `Top` 与 `Snapshot` 在读取时物化切片并排序，不维护额外的有序结构，
  以换取写入路径的极简与无锁序反转风险。
- **候选事务**：`Apply` 先对整个批次做纯结构校验（kind、名称字符集与字节上限），
  不触碰状态；随后在 `maps.Clone` 得到的候选副本上按输入顺序执行 Add/Set/Delete，
  Add/Set 分配连续 revision。算术前先检测 int64 溢出（`ErrValue`），再执行绝对值
  上限；最终账户容量仅在批次末检查（`ErrCapacity`）。任一步失败直接丢弃候选副本，
  实现整体回滚；全部通过才一次性替换内部 map 并将 generation 加一（空批次不变）。
- **所有权**：所有公开方法由单一 `sync.Mutex` 保护，可并发调用。`Result.Changed`、
  `Top`、`Snapshot` 返回的切片均为新建副本，调用方修改不影响内部状态；
  候选副本在提交前为事务私有，提交后归账本独占。
- **复杂度**：结构校验 O(批次大小)；候选执行 O(批次大小 + 当前账户数)（克隆）；
  `Top`/`Snapshot` 为 O(n log n)，n 为账户数；单账户读写均摊 O(1)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
