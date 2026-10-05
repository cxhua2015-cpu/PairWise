# resourcecatalog096

并发安全的内存型资源目录（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

- **索引**：记录存储在 `map[string]entry` 中，按名称 O(1) 定位；`entry` 持有 Value 副本与 revision。Snapshot/Changed 在返回前按名称排序，索引本身不维护有序结构。
- **候选事务**：`Apply` 先做完整结构校验（不读状态），再在单个互斥锁内把当前 map 浅拷贝为候选副本，按输入顺序在其上执行 Put/Delete；Put 从局部 `nextRevision` 计数器分配连续 revision，Delete 不分配。记录数与 Value 总字节容量只在批次末对候选副本检查。任一步失败直接返回，候选副本与局部计数器被丢弃，已提交状态、generation、revision 完全不变；成功时整体换入候选副本，非空批次 generation 恰好加一。
- **所有权**：Put 时拷贝调用方传入的 Value；Get/Snapshot/Result 返回的 Value 均为新分配的深拷贝，返回切片与内部状态完全隔离，调用方后续修改互不影响。
- **并发**：所有公开方法通过单个 `sync.Mutex` 串行化内部状态访问，可安全并发调用；校验在锁外完成，不涉及共享状态。
- **复杂度**：Put/Delete 结构校验 O(L)（L 为名称/值长度）；`Apply` 为 O(R + N + C log C)，其中 R 为当前记录数（候选拷贝）、N 为批次 op 数、C 为变更名数（排序）；`Get` O(1) 加一次值拷贝；`Snapshot` O(R log R)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
