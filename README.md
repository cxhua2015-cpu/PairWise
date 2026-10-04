# breaker

并发安全、使用显式时间的内存断路器注册表（Go 1.22+，仅标准库）。完整契约见 `SPEC.md`。

## 索引结构

`Registry` 内部使用 `map[string]*service` 按服务名索引，每个条目保存不可变的 `Policy` 与可变的 `ServiceState`。所有共享状态由一把 `sync.Mutex` 保护；`Snapshot` 返回深拷贝并按名称排序，调用方修改返回值不会影响注册表。

## 状态机

每个服务在三个状态间转换：

- **Closed**：`Success` 将连续失败计数清零；`Failure` 递增计数，达到 `FailureThreshold` 后转为 Open，`OpenUntil = now + OpenFor`（饱和加法），两个计数器清零。
- **Open**：拒绝事件（`ErrOpen`）。当 `now >= OpenUntil` 时，在任何定时方法中自动转为 HalfOpen 并清零计数器。
- **HalfOpen**：`Failure` 立即重新 Open；`Success` 递增恢复计数，达到 `RecoveryThreshold` 后转为 Closed。

## 事务语义（Record）

`Record` 是原子的：

1. 先对批次中所有事件的服务名做纯结构校验（不读状态），非法返回 `ErrInvalidInput`。
2. 再检查全局时间单调性，`now` 倒退返回 `ErrTime`。
3. 克隆全部服务状态，在候选状态上先把所有到期 Open 服务推进为 HalfOpen，再按输入顺序处理成功/失败事件。
4. 未知服务（`ErrNotFound`）或仍处 Open 的事件（`ErrOpen`）触发整体回滚：自动转换、时间、计数器与 generation 全部还原。
5. 成功的非空批次使 generation 恰好加一；空批次仅在发生自动转换时加一。`Result.Changed` 列出本批次状态发生变化的服务，去重并按名称排序。

`Allow`/`Sweep` 执行同样的全局推进，仅在至少一个服务状态变化时使 generation 加一。

## 时间与溢出

注册表使用非负 `int64` 显式时间，起点为 0。每个定时方法要求 `now >= Snapshot.Now`，否则返回 `ErrTime`。`OpenUntil = now + OpenFor` 采用饱和加法，在 `math.MaxInt64` 处截断而非溢出回绕。

## 复杂度

设服务数为 N、批次事件数为 B：

- `Record`：校验 O(B)，克隆 O(N)，自动推进 O(N)，事件处理 O(B)，结果排序 O(N log N)。
- `Allow` / `Sweep`：O(N log N)（推进 + 变更列表排序）。
- `Snapshot`：O(N log N)（拷贝 + 排序）。
- 空间：O(N)。
