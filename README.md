# metacatalog426

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.


## 设计说明

### 索引
`Store` 以 `map[string]Record` 作为主索引，按名称 O(1) 定位记录；`Snapshot`/`Result.Changed` 在输出时按名称排序，不维护额外有序结构。单把 `sync.RWMutex` 保护全部状态：`Apply` 取写锁，`Get`/`Snapshot`/`Stats`/`Clone`/`Preview` 取读锁，因此所有公开方法均可并发调用且各自线性一致。

### 候选事务与回滚
`Apply` 先做完整结构校验（`ValidateBatch`，不读状态），再在写锁内按输入顺序执行 Put/Delete：Put 分配连续递增 revision，Delete 不分配。执行中记录每个被触及名称的旧值，失败（`ErrNotFound`/`ErrCapacity`）时整体恢复记录表与 `nextRevision`，generation 不变；记录数与 Value 总字节容量只在批次末检查。非空成功批次 generation 恰好加一。

### 所有权
所有进出的 `Value` 切片均深拷贝：Put 时拷贝输入，`Get`/`Snapshot`/`Result.Changed` 返回独立副本，调用方后续修改不会影响内部状态，反之亦然。

### Clone 与 Preview
`Clone` 在读锁下复制记录表与逻辑时钟（generation、nextRevision），得到完全独立的 `*Store`。`Preview` 在一次读锁内取得一致快照（内部 clone），随后在候选副本上复用与 `Apply` 完全相同的 `applyLocked` 事务语义，返回候选 `Result`、`Snapshot`、`Stats`；错误及优先级与同状态 `Apply` 一致，失败时全部返回零值，原对象状态与逻辑时钟不变。

### 复杂度
- `Apply`/`Preview`：O(n + m log m)，n 为批次 op 数，m 为变更名称数（排序）；容量检查 O(记录数)。
- `Get`：O(1) 加返回值拷贝 O(|Value|)。
- `Snapshot`/`Stats`/`Clone`：O(R + V)，R 为记录数，V 为 Value 总字节数。
