# readyqueue375

并发安全的内存型“就绪优先队列”，实现见 `readyqueue375/prioritybox.go`，语义以 `SPEC.md` 与契约测试为准。

## 设计说明

**索引与所有权**
- 队列内部只持有一份权威数据：`map[string]Item`，以 ID 为键，承担存在性判断（`ErrExists`/`ErrNotFound`）与 O(1) 定位。
- 规范顺序（Priority 降序、ReadyAt 升序、ID 升序）不冗余存储，而是在 `Pop`/`Snapshot` 时按需物化并排序，避免双索引不一致。
- 所有公开方法由同一把 `sync.Mutex` 保护；`Snapshot`/`Pop` 返回的切片都是新分配的副本，调用方修改不会影响内部状态（所有权随返回值转移）。

**候选事务（Apply）**
1. 先对整个批次做纯结构校验（kind、ID 字符集与字节上限、Cancel 不得携带 Priority/ReadyAt、负 ReadyAt、负 Now），不读取任何状态。
2. 校验通过后检查单调时间（`Now < now` → `ErrTime`）；空批次为无操作，generation 不变。
3. 将 `items` 克隆为候选 map，在其上顺序执行 Enqueue（分配递增 revision）/Cancel；任何一步失败直接丢弃候选，时间、状态、revision 天然回滚。
4. 仅在末尾做最终容量检查（`len > MaxItems` → `ErrCapacity`），因此批内“先超后删”是合法的。
5. 全部成功才一次性提交：替换 map、推进时间、更新 nextRevision，并将 generation 恰好加一。

**时间与 revision**
- 时间显式、非负且单调：`Apply`/`Pop` 的 `now` 小于当前时间返回 `ErrTime`，负值返回 `ErrInvalidInput`；成功后时间推进到 `now`。
- revision 从 1 开始由 Enqueue 单调分配，失败批次不消耗 revision；`Snapshot.NextRevision` 是下一个待分配值。

**复杂度**（n = 队列大小，k = 批次大小）
- `Apply`：结构校验 O(k·L)（L 为 ID 长度），候选克隆 O(n)，执行 O(k)，提交 O(1)。
- `Pop`：筛选就绪项 O(n)，排序 O(n log n)，删除 O(limit)。
- `Snapshot`：复制并排序 O(n log n)。
- 空间 O(n)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
