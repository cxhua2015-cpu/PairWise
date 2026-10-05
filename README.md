# resourcelease149

Read `SPEC.md` and implement the package.

本任务要求状态引擎、policy.go 与 coordinator.go 三个生产文件协同实现，详见 SPEC.md。

## 架构说明

### 状态引擎（heartbeat.go）
- **索引**：单把 `sync.Mutex` 保护全部内部状态；条目存放在 `map[string]Entry`（按 key O(1) 定位），`Snapshot`/`Expire` 输出前按 key 排序，保证确定性。
- **候选事务**：`Apply` 先在锁外做整批结构校验（kind、key 字符集与字节上限、非负时间），再在锁内检查单调时间；随后把存活条目（`ExpiresAt > Now`）复制进候选 map，顺序执行 Put/Touch/Delete 并递增分配 revision，最后做容量检查。任一步失败直接返回，候选 map、淘汰结果、`now` 与 `nextRev` 全部随候选一起丢弃，实现整体回滚。非空成功批次 generation 恰好 +1，空批次只推进时间。
- **所有权**：`Snapshot` 与 `Expire` 返回的切片均为新建拷贝，调用方修改不会影响内部状态。
- **复杂度**：`Apply` 为 O(n + b)，n 为存活条目数、b 为批内操作数；`Snapshot`/`Expire` 为 O(n log n)（排序）；`Expire` 使用与 `Apply` 相同的闭区间边界 `ExpiresAt <= now`。

### 策略层（policy.go）
- 配置（actor 白名单 + 单批操作数上限）存放在不可变结构体中，通过 `atomic.Pointer` 原子替换；`ReplaceActors` 构建新配置后一次性 `Store`，读者无锁、无锁竞争，且永不会观察到半更新状态。
- `Authorize` 只读策略配置，不触碰核心状态，复杂度 O(1)。

### 协调层（coordinator.go）
- 单把 `sync.Mutex` 串行化准入：先 `Authorize`，拒绝时直接记录审计并返回 `ErrDenied`（不读取/修改核心状态）；通过后委托状态引擎，成功、拒绝与引擎失败都追加一条带连续 `Sequence` 的 `Decision`。
- `Decisions` 返回内部日志的完整拷贝，所有权隔离。

三层各自独立同步，均可并发调用。
