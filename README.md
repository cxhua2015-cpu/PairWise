# readyqueue265

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.


## 设计说明

### 索引
- 主索引为 `map[string]Item`，按 ID 提供 O(1) 的存在性检查、Enqueue 插入与 Cancel 删除。
- Pop 的就绪候选通过在单次扫描中过滤 `ReadyAt <= now` 得到，再按规范顺序（Priority 降序、ReadyAt 升序、ID 升序）排序；未就绪项不参与排序，避免堆结构在“优先级优先于就绪时间”语义下的误弹出问题。

### 候选事务
- `Apply` 先做与 `ValidateBatch` 完全共享的无副作用结构预检（`validation.go` 中的 `validateBatch`），再在互斥锁内顺序执行 Enqueue/Cancel。
- 每个操作记录一条 undo 日志（Enqueue 记删除、Cancel 记旧值）；任一操作失败或末尾容量检查（`len(items) > MaxItems`）失败时，逆序回放 undo，时间、状态与 revision 一并回滚。
- 仅在非空批次成功提交后推进 `now`、`nextRevision` 并使 `generation` 恰好加一；空批次不改变 generation。

### 所有权
- 所有公开方法在单个互斥锁下执行，`Stats`/`Snapshot`/`Clone` 均为线性一致快照。
- 返回值与内部状态隔离：`Item` 为值类型，`Snapshot`/`Pop` 返回新建切片，`Clone` 重建独立 map 并复制逻辑时钟（`now`、`generation`、`nextRevision`），克隆体与原队列互不影响。

### 复杂度
- `New`/`ValidateBatch`：O(1) / O(k)，k 为批次操作数。
- `Apply`：O(k) 均摊（map 操作 + undo 日志），容量检查 O(1)。
- `Pop`：O(n + r log r)，n 为当前元素数，r 为就绪候选数。
- `Snapshot`/`Clone`：O(n log n) / O(n)；`Stats`：O(1)。
