# metacatalog206

并发安全的内存型元数据目录（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

**索引**：`Store` 内部使用 `map[string]Record` 作为主索引，按名称 O(1) 定位记录；另维护 `totalBytes`（Value 总字节）、`generation`、`revision` 三个计数器。`Snapshot`/`Changed` 在返回前按名称排序，不维护额外的有序结构。

**候选事务**：`Apply` 分两阶段。第一阶段不加锁做完整结构校验（kind、名称字符集与长度、Value 长度），失败即返回 `ErrInvalidInput`，不读取任何状态。第二阶段在写锁内把当前索引浅拷贝为候选 map，按输入顺序在候选上执行 Put/Delete：Put 分配连续 revision，Delete 不分配；Delete 缺失键返回 `ErrNotFound`。记录数与 Value 总字节容量只在批次末检查，超限返回 `ErrCapacity`。任一失败直接丢弃候选，状态、generation、revision 全部不变（天然回滚）；成功则整体替换索引，非空批次 generation 只增一次。

**所有权**：Put 的 Value 在提交前深拷贝；`Get`/`Snapshot`/`Result.Changed` 返回的 Value 均为新分配的副本，调用方对返回切片的修改不影响内部状态，反之亦然。

**并发**：单把 `sync.RWMutex` 保护全部状态。`Apply` 持写锁，`Get`/`Snapshot` 持读锁，所有公开方法可并发调用（`-race` 验证通过）。

**复杂度**：结构校验 O(批次大小)；候选拷贝 O(n)，n 为当前记录数；批次执行 O(批次大小)；容量检查 O(1)；`Get` O(1)；`Snapshot` 与 `Changed` 排序 O(k log k)。空间 O(n)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
