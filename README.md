# resourcecatalog186

并发安全的内存型资源目录（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计要点

- **索引**：`Store` 内部使用 `map[string]Record` 作为主索引，按名称 O(1) 定位记录；另维护 `total`（Value 总字节数）、`gen`（generation）、`rev`（已分配的最大 revision）三个计数器，避免全表扫描。
- **候选事务**：`Apply` 分三个阶段——(1) 完整结构校验（kind、名称字符集与长度、Value 长度），不读任何状态；(2) 在记录的克隆副本（候选事务）上按输入顺序执行 Put/Delete，Put 分配连续 revision，Delete 不分配，Delete 缺失记录即返回 `ErrNotFound`；(3) 仅在批次末检查最终记录数与 Value 总字节容量。任一阶段失败直接丢弃候选状态，generation 与 revision 天然回滚；全部通过才一次性提交。
- **所有权**：Put 时深拷贝调用方的 Value；`Get`/`Snapshot` 返回深拷贝，返回切片与内部状态完全隔离。`Snapshot` 记录按名称排序。非空成功批次 generation 恰好加一，空批次不改变任何状态。
- **并发**：所有公开方法由一把 `sync.Mutex` 保护；结构校验在锁外进行（只依赖不可变的 `Options`），状态读写均在临界区内完成。

## 复杂度

- `Apply`：O(k·v + c log c)，k 为批内 op 数，v 为 Value 平均长度（深拷贝），c 为变更名数（排序 `Changed`）；候选克隆为 O(n)，n 为当前记录数。
- `Get`：O(v)（返回值深拷贝）。
- `Snapshot`：O(n·v + n log n)。
- 空间：O(n·v)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
