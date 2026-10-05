# sessiontable

并发安全的内存型“会话到期表”，仅依赖 Go 标准库（Go 1.22+）。语义详见 `SPEC.md`。

## 索引结构

- 主索引为 `map[string]Entry`，按键存储，Put/Touch/Delete/查找均为 O(1) 均摊。
- 不维护堆等额外有序结构；`Snapshot` 与 `Expire` 的结果在返回前按键名排序（规范顺序），排序成本为 O(n log n)。
- 每次 `Apply` 的候选淘汰需扫描全表，成本 O(n)。

## 候选事务

`Apply` 分三个阶段：

1. **结构校验**：在读取任何状态前校验整个批次（kind 合法、键非空且仅含 `[a-z0-9_-]`、键长不超过 `MaxKeyBytes`），失败返回 `ErrInvalidInput`。
2. **时间检查**：`Now` 必须非负且不早于当前表时间，否则返回 `ErrTime`。
3. **候选执行**：在条目 map 的副本上先删除 `ExpiresAt <= Now` 的条目（闭区间），再按顺序执行 Put/Touch/Delete；Put/Touch 各分配一个递增 revision。最终容量超限返回 `ErrCapacity`，任何错误都会丢弃整个候选状态——淘汰、时间与 revision 一并回滚，表保持原状。

只有全部成功时才一次性提交：替换条目 map、推进时间、更新 revision 计数，且非空批次 generation 恰好加一（空批次不改变任何状态）。

## 所有权与并发

- 所有公开方法通过单个互斥锁串行化，可安全并发调用。
- `Snapshot` 与 `Expire` 返回的切片均为新建副本，调用方可自由修改，不影响表内状态；表也从不保留调用方传入的切片。
- `Expire(now)` 使用与 `Apply` 相同的闭区间边界（`ExpiresAt <= now`）删除条目，推进表时间并返回被删条目（按键排序），不增加 generation。

## 复杂度

| 操作 | 时间 | 额外空间 |
| --- | --- | --- |
| `Apply`（m 个 op，n 个条目） | O(n + m) | O(n) 候选副本 |
| `Expire` | O(n + k log k)，k 为过期数 | O(k) |
| `Snapshot` | O(n log n) | O(n) |

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
