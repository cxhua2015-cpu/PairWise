# readyqueue215

并发安全的内存型“就绪优先队列”，仅依赖 Go 标准库（Go 1.22+）。语义详见 `SPEC.md`。

## 设计说明

**索引**
- 主索引为 `map[string]Item`，按 ID O(1) 定位，用于 Enqueue 去重（`ErrExists`）与 Cancel 查找（`ErrNotFound`）。
- 规范顺序（Priority 降序、ReadyAt 升序、ID 升序）不维护额外堆结构，而是在 `Pop`/`Snapshot` 时对候选切片即时排序，换取写入路径 O(1) 与实现的简单可靠。

**候选事务（candidate transaction）**
- `Apply` 先对批次做完整结构校验（kind、ID 字符集与字节上限、ReadyAt 非负），不触碰任何状态。
- 随后在 `items` 的克隆与 `nextRevision` 的副本上顺序执行 Enqueue/Cancel；任何一步失败（`ErrExists`/`ErrNotFound`）或末尾容量检查失败（`ErrCapacity`）都直接丢弃候选，时间、状态、revision 天然回滚。
- 全部成功才一次性提交：替换 map、推进 `now` 与 `nextRevision`，非空批次 `generation` 恰好加一，空批次不变。

**所有权与并发**
- 所有公开方法（`Apply`/`Pop`/`Snapshot`）由同一把 `sync.Mutex` 保护，可任意并发调用。
- `Pop` 在选择后原子地从 map 中删除所选项；`Snapshot` 与 `Pop` 返回的切片均为新分配的副本，调用方修改不会影响内部状态。
- 时间为显式非负单调值：负值返回 `ErrInvalidInput`，回退返回 `ErrTime`，失败操作不推进时间。

**复杂度**（n 为队列大小，b 为批次大小）
- `New`：O(1)。
- `Apply`：O(n + b)——克隆 map 加顺序执行；b 个 Enqueue 各 O(1) 分配 revision，容量仅在末尾检查一次。
- `Pop`：O(n log n) 排序就绪候选，删除所选项 O(k)。
- `Snapshot`：O(n log n)，返回规范顺序的独立副本。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
