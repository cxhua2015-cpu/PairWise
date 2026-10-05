# delayedqueue

并发安全、使用显式时间的内存延迟优先队列（Go 1.22+，仅标准库）。公开契约见 `SPEC.md`。

## 设计说明

- **索引**：任务存放在 `map[string]*Job` 中，按 ID O(1) 查找。未维护堆，就绪集合在 Peek/Take 时按需筛选并排序，因为 limit ≤ 1000 且任务数为控制平面规模。
- **候选事务**：`Apply` 先在候选状态（现有任务的深拷贝 map）上按输入顺序执行全部操作，最后才做任务数与 Payload 总字节容量检查；任一失败直接丢弃候选，jobs、时间、generation、revision 全部天然回滚，不消耗任何 revision。
- **时间**：队列持有单调的 `now`（初始 0）。所有带时间的方法要求 `now >= 当前值`，否则 `ErrTime`；校验顺序为「结构校验 → 时间检查 → 顺序执行」。空批次可推进时间但不增加 generation。
- **排序**：就绪判定为 `ReadyAt <= now`；选择顺序为 Priority 降序、ReadyAt 升序、ID 升序（稳定、确定性）。`Snapshot` 按 ID 升序返回。
- **容量**：`MaxJobs` 与 `MaxTotalPayloadBytes` 只在批次末尾检查最终状态，允许临时超量（例如同批先 Cancel 再 Enqueue）。
- **所有权**：Payload 在入队时拷贝存入，在 Result/Peek/Take/Snapshot 输出时再次拷贝，调用方与队列互不共享切片。
- **并发**：全部方法由一把 `sync.Mutex` 保护，批次整体原子提交。

## 实际复杂度

设 n 为任务数、k 为批次数、m 为就绪任务数：

- `Apply`：结构校验 O(k)，候选拷贝 O(n)，执行 O(k)，容量汇总 O(n)，合计 O(n + k)。
- `Peek` / `Take`：筛选 O(n)，排序 O(m log m)。
- `Snapshot`：O(n log n)（按 ID 排序）。
- 空间：O(n + 总 Payload 字节)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
