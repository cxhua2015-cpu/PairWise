# leasegraph

并发安全的内存 DAG 租约调度器（Go 1.22+，仅标准库）。完整合同见 `SPEC.md`，公开 API 与 sentinel 错误定义在 `leasegraph/leasegraph.go`，实现位于 `leasegraph/scheduler.go`。

## 设计说明

### 并发模型
所有公开方法共用一把 `sync.Mutex`，在锁内完成"校验 → 状态转换 → 返回拷贝"的全过程，因此每个方法都是原子的，且任意交错调用下状态一致。返回值中的切片全部是新分配的副本，锁释放后调用者对返回值的任何修改都不会影响调度器。

### 图索引
- 任务存放在 `map[string]*task`，按 ID O(1) 定位。
- 反向邻接表 `dependents map[string][]string` 记录每个任务的直接依赖者，用于成功后的解锁传播与失败后的级联取消，无需全图扫描。
- 任务的正向依赖列表按加入时的顺序原样保存（`Snapshot` 中 `Dependencies` 保持该顺序）。

### 环检测
已提交图始终无环，因此候选图的环只可能完全落在批内子图上。`AddBatch` 对批内依赖边运行 Kahn 拓扑消除：若批内任务不能全部入度归零，则存在环（包括自环），返回 `ErrCycle`。批内前向引用天然支持，因为校验与建图都在提交前基于完整批次进行。

### 就绪顺序与 Claim
就绪顺序为 Priority 降序、ID 字节序升序（`readyLess`）。`Claim` 在线性扫描中选出最优就绪任务，将其置为 `running`、`Attempt+1`，并分配调度器生命周期内严格递增且非零的 token，`Deadline = now + LeaseDuration`。`now+LeaseDuration > MaxTime` 时返回 `ErrInvalidTime`，不消耗 token；无就绪任务返回 `ErrNoReady`，同样不消耗 token。调度器不隐式处理到期租约，必须显式调用 `Sweep`。

### 租约与重试
`Complete` 按顺序校验：时间 → ID 语法 → 任务存在 → running 状态 → token 相等 → `now < Deadline`（到期未 Sweep 时报 `ErrStaleLease`）→ Result 形状/大小 → 字节容量。任何一步失败都不改变状态、容量或 token 序列。失败时若 `Attempt < MaxAttempts` 任务回到 ready，否则变为 `failed`。`Sweep` 按 Deadline 升序、ID 升序处理所有 `Deadline <= now` 的 running 任务，应用同样的重试/终止规则但不增加 `Attempt`；被本次扫描中较早级联取消的任务会被跳过，不计入 `Expired`。

### 失败传播
任务终止失败时，沿 `dependents` 做 BFS，将所有尚未处于 `succeeded/failed/canceled` 的传递依赖者置为 `canceled`（含 ready/running 任务）。`Transition.Failed`、`Transition.Canceled` 按 ID 升序且去重；`Ready` 使用就绪顺序。

### 容量计算
`UsedBytes` 只统计每个任务当前保存的 `Payload` 与成功 `Result` 的字节数，随提交/成功增量维护。`AddBatch` 最后检查 `MaxTasks` 与 `MaxBytes`；`Complete` 在保存 Result 前检查总字节，超限返回 `ErrCapacity` 并完整回滚（任务保持 running）。

### 批量原子性
`AddBatch` 严格按 SPEC 顺序校验：逐任务字段（ID → Priority → Payload 大小 → 依赖 ID → 同任务重复依赖）→ 任务 ID 重复 → 未知依赖 → 环 → 容量。所有校验通过前不触碰任何共享状态，任一失败都不留下任务或占用容量。

### 所有权隔离
输入的 ID/依赖字符串按值保存，`Payload`/`Result` 深拷贝；`Lease`、`Snapshot`、`Transition`、`SweepResult` 中的所有可变切片均为新副本，彼此及与内部状态完全隔离。

### 复杂度
设 N 为任务数、E 为依赖边数、B 为批次大小：
- `AddBatch`：时间 O(N + E + B²)（批内 Kahn），空间 O(B + E_batch)。
- `Claim`：时间 O(N)（线性选最优就绪），空间 O(|Payload|)。
- `Complete`：时间 O(d·deg + C)，d 为直接依赖者数、C 为级联取消规模；空间 O(C)。
- `Sweep`：时间 O(N + E_exp + C)，E_exp 为到期任务相关边；空间 O(E_exp + C)。
- `Snapshot`：时间 O(N log N)（按 ID 排序 + 就绪排序），空间 O(N + 总字节数)。
- 常驻空间：O(N + E + 总字节数)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
