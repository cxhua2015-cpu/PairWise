# expirytable394

并发安全的内存型“到期状态表”，使用显式非负单调时间，仅依赖 Go 标准库（Go 1.22+）。语义详见 `SPEC.md`。

## 索引

- 主索引为 `map[string]Entry`（键 → `{Key, ExpiresAt, Revision}`），按键 O(1) 定位。
- 未维护堆等按时间排序的辅助索引；淘汰采用全量扫描，换取实现的简单与无锁序反转风险。
- `Snapshot` 与 `Expire` 返回的条目按键名排序，保证输出确定性。

## 候选事务

`Apply` 分两阶段：

1. **校验**：先对整个批次做结构校验（kind 合法、键非空且仅含 `[a-z0-9-_]`、不超过 `MaxKeyBytes`、时间非负），再检查时间单调性（`Now < 当前时间` 返回 `ErrTime`）。
2. **候选执行**：在条目表的克隆上先淘汰 `ExpiresAt <= Now` 的条目，再顺序执行 Put/Touch/Delete；Put/Touch 各分配一个递增 revision。最终条目数超过 `MaxEntries` 或任一操作失败（`ErrNotFound` 等）时直接丢弃克隆——淘汰、时间与 revision 一并回滚，外部状态不变。

空批次为无操作（generation 不变）；非空成功批次 generation 恰好加一。

## 所有权

- 所有公开方法通过单一 `sync.Mutex` 串行化，支持并发调用。
- `Snapshot` 与 `Expire` 返回的切片均为新分配的副本，调用方可自由修改，不影响内部状态。
- 传入的 `Batch`/`Op` 仅按值读取，实现不保留其引用。

## 复杂度

设 n 为当前条目数，b 为批次操作数：

- `Apply`：时间 O(n + b)，候选克隆空间 O(n)。
- `Expire`：时间 O(n + k log k)，k 为到期条目数（排序输出）。
- `Snapshot`：时间 O(n log n)（排序输出），空间 O(n)。
- `New`：O(1)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
