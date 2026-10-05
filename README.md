# policycatalog

并发安全的内存型策略目录，仅依赖 Go 标准库（Go 1.22+）。语义详见 `SPEC.md`。

## 设计说明

**索引**
- 记录存储在 `map[string]Record` 中，按名称 O(1) 定位；名称即主键。
- 另维护 `totalValue`（Value 总字节）与单调递增的 `generation` / `revision` 计数器，避免每次容量检查时遍历全表。
- `Snapshot` 与 `Result.Changed` 在返回前按名称排序，排序只作用于临时切片，不影响索引。

**候选事务（candidate transaction）**
- `Apply` 分三个阶段：先对整个批次做完整结构校验（kind、名称字符集与长度、Value 长度），不读取任何状态；再在记录的克隆map上按输入顺序执行 Put/Delete（Put 分配连续 revision，Delete 不分配）；最后才在批次末检查记录数与 Value 总字节容量。
- 任一步失败（`ErrInvalidInput` / `ErrNotFound` / `ErrCapacity`）直接丢弃候选状态，已存储的记录、`generation`、`revision` 全部保持不变，实现原子回滚。
- 空批次成功且不增加 `generation`；非空成功批次 `generation` 恰好加一。

**所有权**
- Put 时拷贝调用方传入的 `Value`；`Get` / `Snapshot` / `Result.Changed` 返回的 `Value` 均为深拷贝，返回切片与内部状态完全隔离，调用方可自由修改。
- 并发安全由一把 `sync.Mutex` 保证：结构校验在锁外完成，状态读取与提交在临界区内原子执行。

**复杂度**（n = 批次内 op 数，m = 当前记录数，k = 批次涉及的不同名称数）
- `Apply`：校验 O(n·名称长度)；候选克隆 O(m)；执行 O(n)；`Changed` 排序 O(k log k)。
- `Get`：O(1) 查找 + O(|Value|) 拷贝。
- `Snapshot`：O(m) 拷贝 + O(m log m) 排序。
- 空间：O(m + n)（候选克隆与结果缓冲）。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
