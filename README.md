# readyqueue210

并发安全的内存型“就绪优先队列”，仅依赖 Go 标准库（Go 1.22+）。语义详见 `SPEC.md`。

## 设计说明

### 索引
- 主索引为 `map[string]Item`，按 ID O(1) 定位，用于 Enqueue 的重复检测与 Cancel。
- 就绪堆/排序不持久维护：`Pop` 与 `Snapshot` 时把条目收集到切片，按规范顺序
  （Priority 降序 → ReadyAt 升序 → ID 升序）现场排序。队列容量受 `MaxItems` 约束，
  单次排序代价有界，换来实现与回滚路径的简单可靠。

### 候选事务（Apply）
1. 先校验 `Now`（非负、单调）与全部 Op 的结构（kind 合法、ID 字符集与长度、ReadyAt 非负），
   此阶段不触碰任何状态。
2. 顺序执行 Enqueue/Cancel，Enqueue 就地分配递增 revision；每一步记录 undo 日志
   （新增记“删除”，删除记“恢复原值”）。
3. 任一步失败（`ErrExists`/`ErrNotFound`）或**末尾**容量检查（`ErrCapacity`）失败时，
   逆序回放 undo 日志并恢复 `nextRevision`，时间、generation 与条目状态全部回滚。
4. 全部成功才提交：非空批次 generation 恰好 +1，时间前进到 `Now`；空批次只推进时间，
   generation 不变。

### 所有权与并发
- 所有公开方法由同一把 `sync.Mutex` 保护，可安全并发调用；批次内部效果对外原子可见。
- `Pop`/`Snapshot` 返回的切片与 `Item` 均为新分配的副本，调用方修改不影响队列内部状态。
- 时间为显式非负单调值：`Apply.Now` 与 `Pop` 的 `now` 小于当前时间返回 `ErrTime`，
  负值返回 `ErrInvalidInput`；失败路径不推进时间。

### 复杂度
设 n 为当前条目数，b 为批次内 Op 数：
- `Apply`：结构校验 O(b·L)（L 为 ID 长度），执行 O(b)，末尾容量检查 O(1)；回滚 O(b)。
- `Pop`：筛选 O(n) + 排序 O(n log n)，删除 O(min(limit, n))。
- `Snapshot`：O(n log n)（含复制与排序）。
- `New`：O(1)。空间 O(n)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
