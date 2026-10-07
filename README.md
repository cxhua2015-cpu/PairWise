# metacatalog341

并发安全的内存型元数据目录（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

**索引**：`Store` 以 `map[string]Record` 作为主索引，按名称 O(1) 定位记录；`generation` 与 `revision` 为单调计数器。所有共享状态由一把 `sync.Mutex` 保护，`Apply`/`Get`/`Snapshot` 均可并发调用。

**候选事务**：`Apply` 先做整批结构校验（kind、名称字符集与长度、Value 长度），不读取任何状态；随后在记录的副本（候选事务）上按输入顺序执行 Put/Delete——Put 分配连续 revision，Delete 不分配、缺失键返回 `ErrNotFound`。记录数与 Value 总字节容量只在批次末检查。任一步失败直接丢弃候选副本，状态、generation、revision 天然回滚；成功则整体换入，非空批次 generation 只增加一次。

**所有权**：Put 的 Value 在写入时拷贝；`Get`/`Snapshot`/`Result.Changed` 返回的 Value 均为深拷贝，返回切片与内部状态完全隔离，调用方修改互不影响。`Snapshot` 按名称排序。

**复杂度**：结构校验 O(批次大小)；候选事务复制与执行 O(记录数 + 批次大小)；容量检查 O(记录数)；`Get` O(1)；`Snapshot` 与 `Changed` 排序 O(n log n)。空间 O(记录数 + Value 总字节)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
