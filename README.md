# expirytable284

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.


## 设计与语义

### 索引

- 主索引为 `map[string]Entry`，按键精确查找，Put/Touch/Delete 均为 O(1) 均摊。
- `Snapshot`/`Expire` 返回的条目按键排序，保证可复现的输出顺序；返回切片为新建拷贝，与内部状态完全隔离。

### 候选事务（Apply）

1. 先调用与 `ValidateBatch` 共享的无副作用结构校验（不读状态）：非负 `Now`、合法 kind、键字符集与字节上限、Put/Touch 要求 `ExpiresAt > Now`、Delete 不得携带 `ExpiresAt`。
2. 加锁后检查单调时间，`Now < 当前时间` 返回 `ErrTime`。
3. 在候选副本上先淘汰 `ExpiresAt <= Now`（闭区间）的条目，再顺序执行 Put/Touch/Delete；Put/Touch 各分配一个 revision，Touch/Delete 缺失键返回 `ErrNotFound`。
4. 最终条目数超过 `MaxEntries` 返回 `ErrCapacity`。任何错误都会连同淘汰、时间与 revision 一起回滚，只有全部成功才提交；非空成功批次 generation 恰好加一，空批次不变。

`Expire` 使用相同的闭区间边界（`ExpiresAt <= now`），推进逻辑时钟并返回被移除条目。

### 所有权与并发

- 所有公开方法由单把互斥锁保护，可并发调用；`Stats` 在锁内读取，是线性一致的状态摘要。
- `Clone` 在锁内深拷贝条目 map 与逻辑时钟（now/generation/nextRevision），克隆体与原表不共享任何内存，互不影响。
- 配置（`MaxEntries`/`MaxKeyBytes`）在 `New` 后不可变，因此 `ValidateBatch` 无需加锁即可安全读取。

### 复杂度

- Put/Touch/Delete 单操作：O(1) 均摊；`Apply` 为 O(n + e)，n 为批次操作数，e 为当前条目数（候选复制与淘汰扫描）。
- `Expire`/`Snapshot`/`Clone`：O(e)；排序输出额外 O(e log e)。
- 空间：O(e)。

### 文件分工

- `heartbeat.go`：类型、构造、事务引擎（Apply/Expire/Snapshot）。
- `validation.go`：共享结构预检 `ValidateBatch` 与键校验。
- `stats.go`：线性一致统计 `Stats`。
- `clone.go`：保留逻辑时钟、所有权完全隔离的深拷贝 `Clone`。
