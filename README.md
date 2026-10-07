# readyqueue430

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.


## 设计说明

### 索引

队列以 `map[string]Item` 为主索引，Enqueue/Cancel 的存在性判断为 O(1)。规范弹出顺序（Priority 降序、ReadyAt 升序、ID 升序）在 `Pop`/`Snapshot` 时按需排序物化，避免每次写入都维护有序结构。单把互斥锁为所有公开方法提供线性化点。

### 候选事务

`Apply` 先调用与 `ValidateBatch` 共享的结构校验，再在锁内按序执行 Enqueue/Cancel：Enqueue 分配自增 revision，Cancel 删除条目，容量上限只在批次末尾检查。任何失败（`ErrExists`/`ErrNotFound`/`ErrCapacity`）通过 undo 日志回滚条目、revision 计数器与逻辑时间，generation 仅在非空成功批次上 +1。`Preview` 在锁内克隆出一致快照作为候选队列，并在其上运行真实的 `Apply`，因此返回的 `Result`、`Snapshot`、`Stats` 与同状态实际提交完全一致，错误及优先级也一致，而原对象的状态、generation、revision 与逻辑时钟均不变。

### 所有权

`Clone` 在锁内复制全部逻辑时钟并新建 map 与互斥锁，克隆体与原对象完全隔离。`Snapshot`、`Pop`、`Preview` 返回的切片均为新分配的副本，调用方修改不会影响队列内部状态。

### 复杂度

- `Enqueue`/`Cancel`（单次 op）：O(1) 均摊；含 k 个 op 的批次为 O(k)，末尾容量检查 O(1)。
- `Pop`：O(n log n)，n 为就绪候选数；删除为 O(k)。
- `Snapshot`/`Clone`/`Preview`：O(n log n)（排序）/ O(n) / O(n + k)。
- `Stats`：O(1)。空间 O(n)。
