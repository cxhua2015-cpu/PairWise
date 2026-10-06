# expirytable289

并发安全的内存型“到期状态表”，仅依赖 Go 标准库（Go 1.22+）。表使用显式非负单调时间，所有公开方法均可并发调用。

## 索引与数据结构

- 主索引为 `map[string]Entry`，键到条目 O(1) 定位；条目内联 `ExpiresAt` 与 `Revision`，无二级索引。
- 单把 `sync.Mutex` 保护全部状态（条目、逻辑时钟 `now`/`generation`/`nextRevision`），保证线性一致。
- `Snapshot`/`Stats` 在同一临界区内构造，`Entries` 按键名排序（规范顺序），返回切片为新建副本，与内部状态完全隔离。

## 候选事务（Apply）

1. 先做一次无副作用的结构预检（与 `ValidateBatch` 共享 `validateBatch`）：`Now >= 0`、kind 合法、键为非空 ASCII `[a-z0-9-_]` 且不超过 `MaxKeyBytes`、`ExpiresAt >= 0`，Put/Touch 要求 `ExpiresAt > Now`。
2. 再检查时间单调性：`Now < now` 返回 `ErrTime`。
3. 在候选副本上先淘汰 `ExpiresAt <= Now` 的条目（闭区间），再顺序执行 Put/Touch/Delete；Put/Touch 各分配一个递增 revision，Touch/Delete 缺失键返回 `ErrNotFound`。
4. 最终条目数超过 `MaxEntries` 返回 `ErrCapacity`。任何错误整体回滚：淘汰、时间与 revision 均不生效（候选副本直接丢弃）。
5. 成功时提交候选副本并推进 `now`；非空批次 `generation` 恰好加一，空批次不变。

`Expire(now)` 使用相同闭区间边界删除并返回到期条目（按键排序），同样要求时间单调。

## 所有权

- `Clone` 在锁内深拷贝全部条目与逻辑时钟，克隆体与原表完全独立，互不影响。
- `Snapshot`、`Expire` 返回的切片均为新分配内存，调用方修改不会污染表。

## 复杂度

- `Apply`：O(n + m)，n 为当前条目数（候选复制与淘汰），m 为批次操作数。
- `Expire`：O(n + k log k)，k 为到期条目数（排序）。
- `Snapshot`：O(n log n)（排序）；`Stats`：O(1)；`Clone`：O(n)。
- 空间：O(n)。

## 文件分工

- `heartbeat.go`：类型、错误值、核心事务（Apply/Expire/Snapshot）。
- `validation.go`：无副作用批次预检，与 Apply 共享结构语义。
- `stats.go`：线性一致的状态统计。
- `clone.go`：保留逻辑时钟的独立深拷贝。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
