# leasegraph contract

实现必须使用 Go 1.22+ 标准库，并保留 `leasegraph/leasegraph.go` 中的公开 API、常量和错误值。

## Options 与通用边界

- `MaxTasks`：1..10000。
- `MaxBytes`：1..64 MiB。容量只统计每个任务当前保存的 `Payload` 与成功 `Result` 的字节数。
- `LeaseDuration`：1..1e12，单位为调用者定义的逻辑时间单位。
- `MaxAttempts`：1..100。
- 合法时间为 0..`MaxTime`（1e15）。`now+LeaseDuration` 超过 `MaxTime` 时，`Claim` 返回 `ErrInvalidTime`，且不得改变状态或 token 序列。
- ID 长度为 1..64 字节，只允许 ASCII 字母、数字、点、下划线和连字符。
- Priority 范围为 -1000..1000；单个 Payload/Result 最多 1 MiB。

## AddBatch

- 空批次成功且无副作用。
- 校验顺序：按输入顺序校验每个任务的 ID、Priority、Payload 大小、依赖 ID 与同一任务中的重复依赖；随后检查任务 ID 与已有/批内任务重复；再按输入及依赖顺序检查未知依赖；再检查整个候选图是否有环；最后检查 `MaxTasks` 与 `MaxBytes`。
- 批内依赖可前向引用。依赖可以指向已存在任务或本批任意任务。
- 任一步失败都不得留下任务或占用容量。
- 新任务在全部依赖已经成功时为 `ready`，否则为 `blocked`。任务及依赖关系提交后不可修改。
- 输入 ID/依赖字符串按值保存；Payload 必须深拷贝。

## 就绪顺序与 Claim

- 就绪顺序为 Priority 降序，再按 ID 字节序升序。
- `Claim(now)` 先校验时间及截止时间加法；无就绪任务返回 `ErrNoReady`，不得消耗 token。
- 成功领取把任务变为 `running`，`Attempt` 加一，生成调度器生命周期内严格递增且非零的 token，并令 `Deadline=now+LeaseDuration`。
- 返回的 Payload 必须与内部状态隔离。
- 调度器不会隐式处理到期租约；必须调用 `Sweep`。

## Complete

- 校验顺序：时间、ID 语法、任务存在、状态为 running、token 相等、`now < Deadline`、成功/失败结果形状与大小、成功后的总字节容量。
- `success=true` 时 Result 可为 nil；成功保存 Result，任务变为 `succeeded`，并返回因此首次变为 ready 的任务。
- `success=false` 时 Result 必须为 nil。若 `Attempt < MaxAttempts`，任务重新变为 ready；否则任务变为 `failed`，所有尚未处于 succeeded/failed/canceled 的传递依赖任务变为 `canceled`。
- `Transition.Ready` 使用就绪顺序；`Failed` 和 `Canceled` 均按 ID 升序。所有切片均不得重复。
- 任何错误都不得改变任务、容量或 token 序列。到期但未 Sweep 的任务仍为 running，但 Complete 返回 `ErrStaleLease`。

## Sweep

- `Sweep(now)` 先校验时间，然后一次性处理所有 `Deadline <= now` 的 running 任务，处理顺序为 Deadline 升序、再按 ID 升序。
- 每个到期任务按 Complete 失败的重试/终止规则转换，但不会增加 Attempt；终止失败会传递取消依赖者。
- `Expired` 按实际处理顺序；其余字段遵循 Complete 的排序与去重规则。
- 无到期任务时成功返回空结果。Sweep 原子执行。

## Snapshot 与所有权

- Snapshot 中 Tasks 按 ID 升序；Ready 使用领取顺序。
- TaskView 的 Dependencies 保留加入时的顺序。非 running 任务的 LeaseToken 与 Deadline 必须为 0。
- 所有输入 Payload/Result 以及 Lease、Snapshot、Transition、SweepResult 的可变切片都必须与内部状态及彼此隔离。
- 所有公开方法均可被多个 goroutine 并发调用，且必须通过 race detector。

## 错误包装

操作级错误可包装公开 sentinel，但 `errors.Is` 必须成立。一次失败只需返回按校验顺序遇到的第一个错误。
