# idempotency

并发安全的内存幂等请求注册表（Go 1.22+，仅标准库）。完整契约见 `SPEC.md`。

## 状态机

每个 key 对应一条记录，处于两种状态之一：

- **pending**：leader 持有租约（`LeaseUntil`）。`Renew` 延长租约；`Complete` 原子转为 completed；`Abort` 删除记录；租约到期后可被 `Begin` 接管。
- **completed**：结果保留至 `ReplayUntil`，期间相同 fingerprint 的 `Begin` 返回深拷贝的 replay；到期后记录视为不存在。

`Begin` 的观察结果：不存在/已过期 → 成为 leader；未过期 pending 同 fingerprint → `Pending`；未过期 completed 同 fingerprint → `Replay`；未过期记录不同 fingerprint → `ErrConflict`。

## Fencing token

每次成功创建/接管记录分配一个非零、单调递增的 token。`Renew`/`Complete`/`Abort` 只接受当前 pending token；错误的、陈旧的或已完成记录的 token 一律返回 `ErrStaleToken`。接管后旧 token 立即失效。容量不足时不会消耗 token。

## 过期

所有过期判断都基于调用方显式传入的 `now`，注册表本身不读时钟。边界为闭区间到期：`now >= LeaseUntil` 的 pending 可被接管/清扫，`now >= ReplayUntil` 的 completed 视为不存在/可清扫。`Sweep(now, limit)` 按 key 升序删除，`limit == 0` 表示不限。`now+lease`、`now+replayTTL` 溢出返回 `ErrInvalidTime` 且不改变状态。

## 容量

容量只统计当前记录数（`MaxEntries`）与已保存结果字节（`MaxResultBytes`）。`Begin` 按替换后的最终状态检查（替换 completed 记录会先释放其结果字节）；`Complete` 要求结果不超过 `MaxResultBytes`。失败返回 `ErrCapacity`/`ErrResultTooLarge`，状态、计数与 generation 均不变。

## 所有权与 generation

所有公开方法由单一互斥锁保护，可并发使用。所有失败操作不改变任何状态；每次成功的可观察状态变更（创建/接管/替换、续租、完成、放弃、至少删除一条的 Sweep）恰好将 generation 推进一次。`Snapshot` 与 `Begin` 的 replay 返回深拷贝，返回值与内部状态及彼此完全隔离。

## 复杂度

- `Begin`/`Renew`/`Complete`/`Abort`：平均 O(1)（map 操作 + 结果拷贝 O(len(result))）。
- `Sweep`：O(n log n)（收集过期 key 并排序）。
- `Snapshot`：O(n log n)（排序）+ O(结果字节)（深拷贝）。
- 空间：O(记录数 + 结果字节)。
