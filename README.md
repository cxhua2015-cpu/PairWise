# readyqueue210

并发安全的内存型“就绪优先队列”，仅依赖 Go 标准库（Go 1.22+）。语义见 `SPEC.md`。

## 索引

- **主索引**:`map[string]Item`，按 ID O(1) 定位，用于 Enqueue 的重复检测（`ErrExists`）与 Cancel 的存在性检查（`ErrNotFound`)。
- **有序视图**：不维护持久堆；在 `Pop`/`Snapshot` 时把候选收集到切片并按规范顺序（Priority 降序、ReadyAt 升序、ID 升序）排序。队列容量受 `Options.MaxItems` 约束，因此该策略简单且足够快。

## 候选事务

`Apply` 采用候选事务（copy-on-write）:

1. **结构校验**：先对整个批次做纯结构校验（`Now >= 0`、kind 合法、ID 字符集/字节上限、Enqueue 的 `ReadyAt >= 0`)，失败返回 `ErrInvalidInput`，此阶段不读取任何队列状态。
2. **时间检查**：持锁后校验单调时间，`b.Now < now` 返回 `ErrTime`。
3. **候选执行**：在克隆的 map 上顺序执行 Enqueue/Cancel;Enqueue 从候选的 `nextRevision` 起分配 revision。
4. **末尾容量检查**：仅在全部操作执行后检查 `len(items) > MaxItems`，超出返回 `ErrCapacity`。
5. **提交或回滚**：任何一步失败都直接丢弃候选，时间、状态、revision 保持不变；成功才整体提交，且非空批次 `generation` 恰好加一（空批次不变）。

## 所有权

- 所有公开方法（`Apply`/`Pop`/`Snapshot`）由一把 `sync.Mutex` 保护，可任意并发调用。
- `Pop` 与 `Snapshot` 返回的切片均为新建副本，调用方修改返回值不影响队列内部状态。
- 时间为显式非负单调时钟：`Apply` 与 `Pop` 成功时推进 `now`，失败时回滚（不变）。

## 复杂度

设 `M = MaxItems`（队列容量上限）,`k` 为批次的 op 数，`r` 为就绪候选数，`n` 为 Pop 请求数量：

| 操作 | 时间 | 额外空间 |
| --- | --- | --- |
| `New` | O(1) | O(1) |
| `Apply` | O(M + k)（克隆 + 顺序执行） | O(M + k) |
| `Pop` | O(M + r log r)（筛选 + 排序 + 至多 n 次删除） | O(r) |
| `Snapshot` | O(M log M) | O(M) |

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
