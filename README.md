# expirytable299

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.


## 设计说明

### 索引

- 主索引为 `map[string]Entry`，键即条目键，Put/Touch/Delete 均为 O(1) 均摊。
- `Snapshot`/`Expire` 返回的切片按键排序，保证确定性输出，且为独立副本，与内部状态完全隔离。

### 候选事务

- `Apply` 先调用 `ValidateBatch` 做无副作用的完整结构校验（非负时间、合法 kind、键字符集与长度、Put/Touch 的 `ExpiresAt > Now`），再检查时间单调性（`Now < now` 返回 `ErrTime`）。
- 通过后复制候选 map，先在候选上按闭区间 `ExpiresAt <= Now` 淘汰，再顺序执行 Put/Touch/Delete；Put/Touch 各分配一个递增 revision。
- 任何错误（`ErrNotFound`、`ErrCapacity` 等）直接丢弃候选，淘汰、时间与 revision 一并回滚；成功才提交，且非空批次 generation 只增一次，空批次不变。

### 所有权

- 所有公开方法由同一把互斥锁保护，可并发调用；`Stats` 在锁内读取，满足线性一致。
- `Clone` 在锁内深拷贝全部条目与逻辑时钟（now、generation、nextRevision），克隆体与原表完全独立。
- 返回值（`Snapshot.Entries`、`Expire` 切片）均为新建切片，调用方修改不影响表。

### 复杂度

- Put/Touch/Delete 单 op：O(1) 均摊；`Apply`：O(n + e)，n 为 op 数、e 为存活条目数（候选复制与淘汰扫描）。
- `Expire`/`Snapshot`/`Clone`：O(e log e)（排序）或 O(e)；`Stats`/`ValidateBatch`：O(1)/O(n)。
