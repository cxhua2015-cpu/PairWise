# readyqueue330

并发安全的内存型“就绪优先队列”（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计说明

**索引**
- 主存储为 `map[string]*entry`，按 ID 提供 O(1) 查找，用于 `Enqueue` 的重复检测与 `Cancel` 定位。
- 辅助索引为按 `ReadyAt` 升序（平手按 Priority 降序、ID 升序）的二叉最小堆（`container/heap`），`Pop` 只需从堆顶取出 `ReadyAt <= now` 的候选，无需全表扫描。

**候选事务（Apply）**
- `Apply` 先做整批结构校验（时间非负且单调、kind 合法、ID 字符集与字节上限、`ReadyAt >= 0`），任何失败在读取状态前返回 `ErrInvalidInput`/`ErrTime`。
- 校验通过后在克隆的 map 与堆上顺序执行 Enqueue/Cancel（Enqueue 在克隆上分配递增 revision）；任一操作失败（`ErrExists`/`ErrNotFound`）或最终容量超限（`ErrCapacity`）时直接丢弃克隆，时间、状态与 revision 全部回滚。
- 仅在全部成功时提交：替换内部状态、推进时间、generation 恰好加一（空批次不产生任何变化）。

**所有权与并发**
- 所有公开方法由同一把 `sync.Mutex` 保护，可安全并发调用。
- 队列不共享调用方传入的数据；`Pop` 与 `Snapshot` 返回的切片均为新建拷贝，调用方修改不影响内部状态。
- 时间为显式非负单调值：`Apply.Now` 与 `Pop` 的 `now` 不得小于队列当前时间，否则返回 `ErrTime`。

**复杂度**（n = 队列大小，b = 批次大小，k = Pop 数量，r = 就绪候选数）
- `Apply`：校验 O(b·L)（L 为 ID 长度），执行 O(b log n)，克隆 O(n)。
- `Pop`：O(r log n)，其中收集与回填候选各为堆操作。
- `Snapshot`：O(n log n)（按规范顺序排序输出）。
- `New`：O(1)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
