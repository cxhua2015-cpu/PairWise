# expirytable414

并发安全的内存型“到期状态表 414”。表由显式、非负、单调递增的逻辑时钟驱动，仅依赖 Go 标准库（Go 1.22+）。

## 多文件架构

- `heartbeat.go` — 核心事务引擎：`New` / `Apply` / `Expire` / `Snapshot`，以及类型与错误值。
- `validation.go` — 无副作用的批次结构预检 `ValidateBatch`，与 `Apply` 共享同一套结构语义。
- `stats.go` — 线性一致的统计 `Stats`。
- `clone.go` — 保留逻辑时钟、所有权完全隔离的深拷贝 `Clone`。

## 索引

内部状态为 `map[string]Entry`，键到条目 O(1) 定位。`Snapshot`/`Expire` 返回的切片按键排序，保证输出确定性，且每次调用都新建切片，与内部状态完全隔离。

## 候选事务

`Apply` 的执行顺序：

1. **结构校验**（与 `ValidateBatch` 相同，不读状态）：`Now >= 0`、kind 合法、键非空且仅含 ASCII 小写字母/数字/连字符/下划线并不超过 `MaxKeyBytes`、`ExpiresAt >= 0`，且 Put/Touch 要求 `ExpiresAt > Now`。
2. **时间检查**：`Now < 当前时钟` 返回 `ErrTime`。
3. **候选状态**：复制存活条目，先删除 `ExpiresAt <= Now` 的条目（闭区间），再顺序执行 Put/Touch/Delete；Put/Touch 各分配一个递增 revision，Touch/Delete 缺失键返回 `ErrNotFound`。
4. **最终容量检查**：候选条目数超过 `MaxEntries` 返回 `ErrCapacity`。

任何错误都会连同淘汰、时间与 revision 一起回滚——只有全部成功才提交候选状态、推进时钟。非空成功批次 generation 只增加一次，空批次不变。`Expire(now)` 使用相同的闭区间边界（`ExpiresAt <= now`）并推进时钟。

## 所有权

- 所有公开方法持有同一把互斥锁，可并发调用；`Stats`/`Snapshot`/`Clone` 在锁内构造，满足线性一致。
- `Snapshot`/`Expire` 返回的切片为新建副本，调用方修改不影响表。
- `Clone` 深拷贝条目 map 与全部逻辑时钟（now、generation、nextRevision），克隆体与原表互不影响。

## 复杂度

- `ValidateBatch`：O(批次数 × 键长)，不读状态。
- `Apply`：O(n + 批次数)，n 为当前条目数（候选复制）；失败零副作用。
- `Expire`：O(n + k log k)，k 为到期条目数（排序输出）。
- `Snapshot`：O(n log n)；`Stats`：O(1)；`Clone`：O(n)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
