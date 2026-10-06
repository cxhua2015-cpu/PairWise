# readyqueue285

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.


## 设计说明

### 索引

- 主索引为 `map[string]Item`，按 ID 提供 O(1) 的存在性判断（`ErrExists`/`ErrNotFound`）。
- 就绪选择采用候选扫描 + 排序：`Pop` 收集 `ReadyAt <= now` 的候选后按规范顺序（Priority 降序、ReadyAt 升序、ID 升序）排序。`Snapshot` 使用同一比较器输出规范顺序，保证结果确定性。

### 候选事务

- `Apply` 先在 `validation.go` 中做无副作用的完整结构预检（与 `ValidateBatch` 共享同一套语义），不读取任何队列状态。
- 随后在互斥锁内顺序执行 Enqueue/Cancel，Enqueue 当场分配单调递增的 revision；容量上限只在批次末尾检查一次。
- 任一失败通过逆序 undo 日志回滚所有条目变更，并回收已分配的 revision；时间（`now`）与 generation 仅在成功时推进，非空成功批次 generation 恰好 +1，空批次不变。

### 所有权

- Item 按值进出队列，调用方与内部状态永不共享内存；`Snapshot`/`Pop` 返回的切片均为新分配。
- `Clone` 在锁内复制全部条目与逻辑时钟（now、generation、nextRevision），克隆体持有独立的 map 与互斥锁，之后两个队列互不影响。

### 复杂度

- `Apply`：O(k)，k 为批次内 op 数（末尾容量检查 O(1)）。
- `Pop`：O(n + r log r)，n 为队列大小，r 为就绪候选数。
- `Snapshot`：O(n log n)；`Stats`：O(1)；`Clone`：O(n)。
- 所有公开方法由单一互斥锁串行化，因此 `Stats`、`Snapshot`、`Clone` 都是线性一致的。
