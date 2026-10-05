# membershiplease

并发安全的内存型成员租约表（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 索引

- 主索引为 `map[string]Entry`，键即成员名，Put/Touch/Delete/过期扫描均基于该映射。
- 另维护单调标量：`now`（当前时间）、`generation`（代数）、`nextRevision`（下一个修订号，从 1 开始）。
- `Snapshot` 与 `Expire` 返回的条目按键排序，保证规范顺序；未使用堆等额外索引，过期采用线性扫描。

## 候选事务

`Apply` 分三个阶段：

1. **结构校验**：在读取任何状态前校验全部操作（kind 合法、键为非空 ASCII 小写/数字/连字符/下划线且不超 `MaxKeyBytes`、`ExpiresAt >= 0`），失败返回 `ErrInvalidInput`。
2. **时间检查**：`Now` 必须非负且不早于当前时间，否则 `ErrTime`。
3. **候选执行**：复制当前条目为候选状态，先删除 `ExpiresAt <= Now` 的条目，再顺序执行 Put/Touch/Delete；Put/Touch 从 `nextRevision` 分配修订号，Touch/Delete 缺失键返回 `ErrNotFound`。最终条目数超过 `MaxEntries` 返回 `ErrCapacity`。

任何错误都直接丢弃候选状态，淘汰、时间与 revision 一并回滚，已提交状态不受影响。非空成功批次 `generation` 恰好加一；空批次不改变任何状态。`Expire` 使用相同的闭区间边界（`ExpiresAt <= now`）并推进时间。

## 所有权

- 所有公开方法通过单一互斥锁串行化，支持并发调用。
- `Snapshot` 与 `Expire` 返回的切片均为新分配的副本，调用方修改不会影响内部状态；`Entry`/`Result`/`Snapshot` 均为值类型。

## 复杂度

- `Apply`：O(n + m)，n 为现有条目数（候选复制与过期扫描），m 为批内操作数；每次操作 O(1)。
- `Expire`：O(n + k log k)，k 为过期条目数（排序）。
- `Snapshot`：O(n log n)（排序输出）。
- 空间：O(n)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
