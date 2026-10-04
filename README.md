# fairqueue

并发安全的内存加权公平任务队列（Go 1.22+，仅标准库）。完整契约见 `SPEC.md`。

## 用法

```go
q, err := fairqueue.New(fairqueue.Options{MaxTasks: 8, MaxPayloadBytes: 64, MaxNameBytes: 16},
    []fairqueue.QueueWeight{{Queue: "bulk", Weight: 1}, {Queue: "urgent", Weight: 2}})
gen, err := q.ApplyBatch([]fairqueue.Change{{Kind: fairqueue.Put, ID: "u1", Queue: "urgent", Payload: []byte("one")}})
res, err := q.Dequeue(3)
snap := q.Snapshot()
```

运行示例：`go run ./cmd/demo`。

## 设计

- **索引**：一个全局 `map[ID]*Task` 保证任务 ID 全局唯一并支持 O(1) 任意 ID 删除定位；每个命名队列一条 `[]*Task` FIFO 切片，按 Sequence 严格先进先出。
- **加权轮转**：构造时将队列名升序排序，每个名字连续重复 Weight 次，生成不可变调度轮（如 `a=2,b=1` → `[a,a,b]`）。调度从游标开始逐槽检查，每检查一槽游标推进一格（模轮长），命中非空队列即取其队首任务，空队列跳过；整轮无命中则停止。`Peek` 在克隆状态上模拟，`Dequeue` 提交结果游标。
- **事务**：`ApplyBatch` 先对全部变更做结构校验（不查状态），再在隔离候选状态上按输入顺序执行存在性语义（Put 重复 → `ErrExists`，Delete 缺失 → `ErrNotFound`，同 ID 先删后加合法并分配新 Sequence），每个成功 Put 分配连续递增的 Sequence；仅对最终候选检查任务数与 Payload 字节容量。任何失败整体回滚且不消耗 Sequence；成功非空批次 generation 恰好 +1。
- **容量**：`MaxTasks` 与总 Payload 字节只在批次最终状态上检查，因此"先删后加"的替换不受临时超限影响。
- **所有权**：所有进入的 Payload 在提交时深拷贝，所有返回的 Task/Payload/Wheel/Items 均为独立副本，调用方与内部状态互不影响。
- **并发**：全部公开方法由一把 `sync.Mutex` 保护，可安全并发调用。

## 复杂度

设 n = 任务总数，b = 批次变更数，W = 权重总和（轮长，≤1024），P = 涉及 Payload 总字节，k = 选中任务数。

- `New`：时间 O(W + q log q)，空间 O(W)，q 为队列数。
- `ApplyBatch`：候选克隆 O(n + P)，执行 O(b · 队首删除均摊 O(1)，切片删除 O(队长))，容量检查 O(1)；空间 O(n + P)。
- `Peek` / `Dequeue`：克隆 O(n + P)；调度每次选择最多扫描一整轮，O(k · W)。
- `Snapshot`：收集 O(n + P)，排序 O(n log n)。
- 总空间：O(n + P + W)。
