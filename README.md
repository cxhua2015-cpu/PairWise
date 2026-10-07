# metacatalog306

并发安全的内存型元数据目录（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计说明

- **索引**：`Store` 内部以 `map[string]entry` 作为主索引，键为记录名，值为 `{value, revision}`；另维护 `totalValue` 运行合计，避免每次容量检查都全表扫描。单把 `sync.RWMutex` 保护全部状态：`Apply` 持写锁，`Get`/`Snapshot` 持读锁，因此所有公开方法可并发调用。
- **候选事务**：`Apply` 先对整个批次做完整结构校验（kind、名称字符集与长度、Value 长度），失败直接返回 `ErrInvalidInput`，不读取任何状态。随后在记录的副本（候选视图）上按输入顺序执行 Put/Delete：Put 分配连续 revision，Delete 不分配；Delete 缺失键即返回 `ErrNotFound`。记录数与 Value 总字节上限只在批次末检查，超限返回 `ErrCapacity`。任何失败都直接丢弃候选视图，已提交的 `records`、`generation`、`revision` 保持不变，实现整体回滚；只有全部成功才一次性提交，非空成功批次 `generation` 恰好加一。
- **所有权**：Put 时拷贝调用方传入的 Value；`Get`/`Snapshot`/`Result.Changed` 返回的 Value 均为深拷贝，返回切片亦为新建，调用方对返回数据的修改不会影响内部状态，反之亦然。
- **复杂度**：结构校验 O(批次总字节)；候选应用 O(已有记录数 + 批次大小)（复制索引为浅拷贝，仅 Value 在写入时深拷贝）；批次末容量检查 O(1)（记录数与 `totalValue` 均为维护好的计数）；`Get` O(1) 均摊；`Snapshot` 与 `Changed` 排序为 O(n log n)，n 为记录数。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
