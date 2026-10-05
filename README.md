# resourcecatalog101

并发安全的内存型资源目录（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

- **索引**：单只 `map[string]Record` 作为主索引，按名称 O(1) 定位；另维护
  `totalValue`（Value 总字节）与 `nextRevision` 计数器，避免批次末全表扫描。
  `Snapshot`/`Result.Changed` 在返回前对名称排序，不维护额外的有序结构。
- **候选事务**：`Apply` 分两阶段。第一阶段对全部 Op 做纯结构校验（kind、名称
  字符集与长度、Value 长度），不读任何状态；第二阶段在写锁内把操作按输入顺序
  应用到按名缓存的 delta 覆盖层（候选事务）上：Put 分配连续 revision，Delete
  要求记录存在。记录数与 Value 总字节容量只在批次末基于 delta 计算终态后检查。
  任一步失败直接返回，内部 map、generation、revision 完全未被触碰，天然回滚；
  全部通过后才统一提交并使 generation 恰好 +1。
- **所有权**：Put 的 Value 在提交前深拷贝；`Get`/`Snapshot`/`Result.Changed`
  返回的 Value 均为独立副本，调用方对返回切片的任何修改不影响内部状态，
  内部状态也不会因调用方后续修改入参切片而变更。
- **并发**：`sync.RWMutex` 保护全部状态；`Apply` 持写锁，`Get`/`Snapshot`
  持读锁。结构校验在加锁前完成，缩短临界区。
- **复杂度**：结构校验 O(Σ 名称+值长度)；候选应用 O(k)，k 为批内 Op 数；
  容量检查 O(u)，u 为批内去重名称数；提交 O(u)。`Get` O(1)；`Snapshot`
  O(n log n)（排序）+ O(总字节)（深拷贝）。空间 O(记录数 + 总字节)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
