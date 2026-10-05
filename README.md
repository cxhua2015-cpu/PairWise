# delayedqueue

并发安全、使用显式时间的内存延迟优先队列（Go 1.22+，仅标准库）。公开契约见 `SPEC.md`。

## 设计说明

- **索引**：单一 `map[string]Job` 按 ID 索引，Enqueue/Reschedule/Cancel 均为 O(1) 定位。未维护堆，就绪选择在调用时全量扫描并排序，因此 Peek/Take 为 O(n log n)，Snapshot 为 O(n log n)（按 ID 排序），Apply 为 O(n)（候选复制与最终容量统计）。对控制平面规模（数千任务）足够简单且正确。
- **候选事务**：`Apply` 先在当前状态的浅拷贝（候选 map）上按输入顺序执行全部操作，失败时直接丢弃候选，作业、时间、generation、revision 分配全部天然回滚；成功才整体提交。最终任务数与 Payload 总字节容量在所有操作执行完后统一检查，允许同批临时超量。
- **时间**：显式非负 `int64` 时间，从 0 开始；所有计时方法要求 `now >= Snapshot.Now`，否则 `ErrTime`。空批次可推进时间但不增加 generation；非空批次提交时 generation 恰好加一。
- **排序**：Peek/Take 的就绪选择按 Priority 降序、ReadyAt 升序、ID 升序，稳定且确定性；Snapshot 按 ID 升序。
- **容量**：`MaxJobs` 与 `MaxTotalPayloadBytes` 仅在批次末尾校验；超限返回 `ErrCapacity` 并整体回滚，不消耗 revision。
- **所有权**：所有 Payload 在入队时深拷贝，所有返回（Result.Changed、Peek、Take、Snapshot）均为深拷贝，调用方与队列互不影响。
- **并发**：单把 `sync.Mutex` 保护全部状态，所有方法并发安全；`-race` 下通过。

## 校验与错误顺序

`Apply` 先对整批做纯结构校验（不读状态），再做时间单调性检查，最后在候选状态上顺序执行并做状态相关检查（`ErrConflict`/`ErrNotFound`/`ErrCapacity`）。Enqueue 与 Reschedule 分配连续 revision，Cancel 不分配。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
