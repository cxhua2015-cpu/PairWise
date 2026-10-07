# metacatalog366

并发安全的内存型元数据目录，仅依赖 Go 标准库（Go 1.22+）。语义详见 `SPEC.md`。

## 设计说明

**索引**：`Store` 使用单个 `map[string]Record` 作为主索引，按名称 O(1) 定位记录；另维护 `totalBytes`、`generation`、`revision` 三个计数器。所有共享状态由一把 `sync.RWMutex` 保护：`Apply` 取写锁，`Get`/`Snapshot` 取读锁，因此所有公开方法可并发调用。

**候选事务**：`Apply` 分三个阶段——
1. 完整结构校验（kind、名称字符集与长度、Value 长度），在任何状态读取之前完成；
2. 在候选视图（`candidate` 覆盖层 + 已提交 map）上按输入顺序执行 Put/Delete，Put 分配连续 revision，Delete 不分配；Delete 缺失记录返回 `ErrNotFound`；
3. 仅在批次末检查最终记录数与 Value 总字节容量，超限返回 `ErrCapacity`。

任何失败都直接丢弃候选视图，已提交状态、generation 与 revision 天然回滚，无需反向补偿。非空成功批次 generation 只增一次，空批次不变。`Result.Changed` 按名称去重（保留最后一次 Put）并按名称排序。

**所有权**：Put 时深拷贝 Value；`Get`/`Snapshot` 返回深拷贝的 Value 与独立切片，调用方对返回值的修改不会影响内部状态，反之亦然。`Snapshot.Records` 按名称排序。

**复杂度**：结构校验 O(批次总字节)；候选执行 O(k)，k 为批次数；容量检查 O(d)，d 为候选涉及的不同名称数；`Get` O(|value|)（拷贝）；`Snapshot` O(n log n)（n 为记录数，排序 + 全量深拷贝）。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
