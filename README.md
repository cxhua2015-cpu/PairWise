# expirytable269

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.


## 设计说明

### 索引

表内条目以 `map[string]Entry` 为主索引，键即用户 key，查找、插入、删除均为 O(1) 均摊。`Snapshot` 与 `Expire` 返回的切片按 key 排序，保证输出确定性，且与内部状态完全隔离。整表由一把 `sync.RWMutex` 保护：写路径（`Apply`/`Expire`）持写锁，读路径（`Snapshot`/`Stats`/`Clone`）持读锁，所有公开方法可并发调用。

### 候选事务

`Apply` 分三个阶段：先调用与 `ValidateBatch` 共享的无副作用结构预检（不读取任何状态），再在写锁内检查时间单调性，最后在候选副本上执行——先淘汰 `ExpiresAt <= Now` 的条目，再顺序执行 Put/Touch/Delete 并分配 revision。任何错误（`ErrNotFound`、`ErrCapacity` 等）发生时直接丢弃候选，淘汰、时间与 revision 一并回滚；只有全部成功才提交候选、推进逻辑时钟，且非空批次 generation 只增加一次。`Expire` 使用相同的闭区间边界。

### 所有权

`Clone` 在读锁下复制全部条目与逻辑时钟（now、generation、nextRevision），返回的表与原表不共享任何内存，可独立演进。`Snapshot`/`Expire` 返回的切片均为新建副本，调用方修改不会影响表内状态。

### 复杂度

- `Apply`：O(n + m)，n 为现存条目数（候选复制与淘汰），m 为批次内 op 数。
- `Expire`：O(n + k log k)，k 为到期条目数（排序输出）。
- `Snapshot`/`Clone`：O(n log n) / O(n)。
- `Stats`：O(1)。
- `ValidateBatch`：O(m)，无副作用、不触碰表状态。
