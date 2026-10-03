# leasegraph

并发安全的内存 DAG 租约调度器（Go 1.22+，仅标准库）。完整行为合同见 `SPEC.md`，
公开 API 骨架在 `leasegraph/leasegraph.go`，实现位于 `leasegraph/scheduler.go`。

## 图索引

调度器以 `map[string]*task` 存放全部任务。每个任务同时保存：

- `deps`：依赖 ID 列表，按 `AddBatch` 输入顺序原样保存（快照中保持该顺序）；
- `dependents`：反向边（哪些任务依赖我），在批次提交时建立，用于就绪解锁与失败级联。

任务与依赖关系提交后不可修改，因此正反向索引只在 `AddBatch` 时增长。

## 环检测

已有图恒为无环（只有无环图才能提交成功），因此 `AddBatch` 只需在**批内新任务子图**上
做三色 DFS（白/灰/黑）：沿指向批内任务的依赖边遍历，遇到灰色节点即判定 `ErrCycle`。
单任务自依赖、批内前向引用环都能被捕获。检测在重复 ID、未知依赖校验之后、容量校验之前。

## 就绪顺序

就绪集合维护为按 `(Priority 降序, ID 字节序升序)` 排序的切片，插入用二分查找定位。
`Claim` 直接取队首；`Snapshot.Ready` 是该切片的拷贝；`Transition.Ready` 与
`SweepResult.Ready` 中的新就绪任务也按同一顺序输出。

## 租约与重试

- `Claim(now)` 校验 `0 <= now <= MaxTime` 且 `now+LeaseDuration <= MaxTime`，否则
  `ErrInvalidTime` 且不消耗 token；无就绪任务返回 `ErrNoReady`，同样不消耗 token。
- 成功领取：任务进入 `running`，`Attempt+1`，token 取自调度器生命周期内严格递增、
  非零的计数器，`Deadline = now + LeaseDuration`。
- `Complete` 按序校验：时间、ID 语法、存在性、`running` 状态、token 相等、
  `now < Deadline`、结果形状与大小、容量；任何错误都不改变状态、容量或 token 序列。
- 失败时若 `Attempt < MaxAttempts` 则回到就绪队列等待重试，否则终止失败。
- 调度器不隐式处理到期租约：`Sweep(now)` 一次性处理所有 `Deadline <= now` 的
  running 任务（按 Deadline 升序、再按 ID 升序），按相同重试/终止规则转换，
  但不增加 `Attempt`。整个 Sweep 在一次临界区内原子完成。

## 失败传播

任务终止失败（重试耗尽，无论来自 `Complete` 还是 `Sweep`）时，沿 `dependents`
反向边 BFS，把所有尚未处于 `succeeded/failed/canceled` 的传递依赖者置为 `canceled`
（就绪者同时移出就绪队列）。由于 `succeeded` 是终态且只有全部依赖成功才能就绪，
被级联取消的实际只会是 `blocked` 任务；`Failed`、`Canceled` 输出均按 ID 升序去重。

## 容量计算

`UsedBytes` 只统计每个任务当前保存的 `Payload` 与成功 `Result` 的字节数：
`AddBatch` 累加各任务 Payload，`Complete(success=true)` 累加 Result。
`AddBatch` 与 `Complete` 在提交前分别检查 `MaxTasks`/`MaxBytes` 上限，
超限返回 `ErrCapacity` 且不留任何副作用。

## 批量原子性

`AddBatch` 严格按合同顺序校验：逐任务字段（ID、Priority、Payload 大小、依赖 ID 语法、
任务内重复依赖）→ 任务 ID 与已有/批内重复 → 未知依赖 → 环 → `MaxTasks`/`MaxBytes`。
全部校验在只读候选图上完成，通过后才一次性提交；任一步失败都不会留下任务或占用容量。
`Complete`/`Sweep` 同样在单个互斥锁临界区内完成校验与状态迁移，天然原子。

## 所有权

所有进入调度器的可变数据（`Dependencies`、`Payload`、`Result`）在边界处深拷贝；
所有返回的可变数据（`Lease.Payload`、`TaskView` 各切片、`Transition`、`SweepResult`、
`Snapshot.Ready`）都是新分配的副本，与内部状态及彼此之间完全隔离。
非 running 任务在快照中 `LeaseToken` 与 `Deadline` 恒为 0。

## 并发

全部公开方法由一把 `sync.Mutex` 保护，可任意并发调用；`go test -race ./...` 通过。

## 复杂度

设 N 为任务数、D 为单任务依赖数、R 为就绪队列长度、E 为一次操作触发的边数。

- `AddBatch`（批大小 B）：校验 O(B·D + 批内边数)，提交 O(B·D + B·R)；
  环检测为批内子图 O(B + 批内边数)。
- `Claim`：O(1)（取就绪队首）。
- `Complete`：O(E + K log K)，K 为新就绪/级联任务数；就绪插入二分定位 O(log R)，
  切片移动 O(R)。
- `Sweep`：O(N log N) 排序到期任务（实际仅对到期子集排序）加级联 O(E)。
- `Snapshot`：O(N log N)（按 ID 排序）加全量深拷贝 O(总字节数)。
- 空间：O(N·D + 总字节数)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
