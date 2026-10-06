# metacatalog211

并发安全的内存型元数据目录（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

- **索引**：`Store` 内部使用 `map[string]Record` 作为主索引，按名称 O(1) 定位记录；另维护 `totalValue` 计数避免每次重算 Value 总字节。Snapshot 与 `Result.Changed` 在返回前按名称排序。
- **候选事务**：`Apply` 先做整批结构校验（kind、名称字符集与长度、Value 长度），不触碰状态；然后在索引的副本（候选事务）上按输入顺序执行 Put/Delete，Put 从单调递增的 `nextRevision` 分配连续 revision，Delete 不分配。批次末尾才检查记录数与 Value 总字节容量；任何失败直接丢弃候选，状态、generation、revision 全部不变（天然回滚）。非空成功批次 generation 只加一，空批次不变。
- **所有权**：Put 时深拷贝 Value 存入；`Get`/`Snapshot`/`Result.Changed` 返回的 Value 均为新切片，调用方修改返回值或入参均不影响内部状态。
- **并发**：单把 `sync.RWMutex` 保护全部状态；`Apply` 持写锁，`Get`/`Snapshot` 持读锁，可并发执行。
- **复杂度**：结构校验 O(批次大小)；Apply 额外 O(n) 复制索引（n 为当前记录数）加 O(k log k) 排序 Changed（k 为涉及名称数）；Get O(1)（外加返回值拷贝）；Snapshot O(n log n)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
