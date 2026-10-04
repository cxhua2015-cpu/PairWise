# breaker

并发安全、使用显式时间的内存断路器注册表（Go 1.22+，仅标准库）。完整契约见 `SPEC.md`。

## 索引结构

`Registry` 内部持有：

- `services map[string]*service`：按名称 O(1) 查找服务；
- `names []string`：构造时排序的服务名切片，用于 `Snapshot`、`Sweep` 以及批次开始时的自动转换，保证输出稳定按名称排序；
- `now` / `generation`：注册表级显式时间与代数计数器。

所有公开方法通过一把 `sync.Mutex` 串行化，无锁内分配之外的共享状态，天然并发安全。

## 状态机

每个服务在 `Closed`、`Open`、`HalfOpen` 三态间转换：

- **Closed**：`Success` 重置连续失败计数；`Failure` 递增，达到 `FailureThreshold` 转为 `Open`，`OpenUntil = now + OpenFor`（int64 极值处饱和到 `math.MaxInt64`，不溢出回绕），两个计数器清零。
- **Open**：拒绝事件（`ErrOpen`）。当 `now >= OpenUntil` 时，在任意定时方法（`Record`/`Allow`/`Sweep`）中自动转为 `HalfOpen` 并清零计数器。
- **HalfOpen**：`Failure` 立即重新打开；`Success` 递增恢复计数，达到 `RecoveryThreshold` 转为 `Closed`。

## 事务（Record）

`Record` 分四个阶段，任一失败整体回滚：

1. **结构校验**：按输入顺序校验所有事件的服务名（不读任何状态），失败返回 `ErrInvalidInput`；
2. **时间检查**：`Now < Snapshot.Now` 返回 `ErrTime`；
3. **候选状态推进**：在隔离的候选状态上先把所有到期 `Open` 服务自动转为 `HalfOpen`；
4. **顺序处理事件**：按输入顺序应用成功/失败；未知服务 `ErrNotFound`、仍 `Open` 的服务 `ErrOpen`。

实现上对触及的服务做备份，出错时逆序恢复，因此自动转换、时间、计数器和 `generation` 都不会泄漏。提交时：`now` 前进；非空成功批次 `generation` 恰好 +1；空批次仅当发生了自动转换才 +1。`Result.Changed` 为发生状态（模式）变化的服务名，去重并按名称排序。

## 时间与溢出

- 时间为显式非负 `int64`，注册表从 0 开始，所有定时方法要求 `now >= Snapshot.Now`，否则 `ErrTime`。
- `OpenUntil = now + OpenFor` 在 `now + OpenFor > math.MaxInt64` 时饱和为 `math.MaxInt64`，避免 int64 溢出回绕成负数导致断路器立即"到期"。
- `Allow`/`Sweep` 执行相同的全局到期推进，仅当至少一个服务状态变化时 `generation` +1；`Allow` 返回目标服务是否允许请求（`Closed` 或 `HalfOpen`）及其状态。
- `Snapshot` 返回按名称排序的服务副本，调用方修改不影响注册表。

## 复杂度

设 `S` 为服务数、`E` 为批次事件数：

- `New`：`O(S log S)`（排序名称）；
- `Record`：`O(E + S)`（自动转换需扫描全部服务）+ `O(C log C)`，`C` 为变化服务数；
- `Allow` / `Sweep`：`O(S)`；
- `Snapshot`：`O(S)`；
- 空间：`O(S)`。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
