# resourcecatalog116

并发安全的内存型“资源目录”，实现见 `resourcecatalog116/servicecatalog.go`，规范见 `SPEC.md`。

## 设计说明

- **索引**：`Store` 以 `map[string]record` 为主索引，键为资源名，值为 `{value, revision}`；另维护 `totalValue`（Value 总字节数）、`generation` 与 `nextRevision` 计数器。所有状态由一把 `sync.Mutex` 保护，公开方法（`Apply`/`Get`/`Snapshot`）均可在任意 goroutine 上并发调用。
- **候选事务**：`Apply` 先在持锁状态下对全部操作做完整结构校验（kind、名称字符集与长度、Value 长度），不做任何状态读取；随后在现有记录的拷贝（候选事务）上按输入顺序执行 Put/Delete——Put 分配连续 revision，Delete 不分配。记录数与 Value 总字节容量只在批次末检查；任何失败直接丢弃候选，已提交状态、generation 与 revision 完全不变。成功后候选整体替换主索引，非空批次 generation 恰好加一。
- **所有权**：Put 的 Value 在入库时深拷贝，调用方之后修改入参不影响目录；`Get`/`Snapshot`/`Result.Changed` 返回的 Value 均为深拷贝，调用方修改返回值不会污染内部状态。`Snapshot` 的记录按名称排序。
- **复杂度**：`Apply` 为 O(n + m)，其中 n 为现有记录数（候选拷贝）、m 为批内操作数，外加结果排序 O(k log k)（k 为批内涉及的不同名称数）；`Get` 为 O(1)（含返回值拷贝 O(v)）；`Snapshot` 为 O(n log n)（排序）加 O(总字节数) 的深拷贝。空间为 O(记录数 + Value 总字节数)。

## 验证

```
go test ./...
go test -race ./...
go run ./cmd/demo
```
