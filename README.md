# resourcelease189

并发安全的内存型“资源租约表”，Go 1.22+，仅依赖标准库。语义见 `SPEC.md`。

## 索引

- 主索引为 `map[string]Entry`，按 key 精确查找，Put/Touch/Delete 均为 O(1) 均摊。
- `Snapshot` 与 `Expire` 的结果按 key 排序输出，保证确定性（排序 O(n log n)）。
- 未维护过期堆：候选淘汰与 `Expire` 均为全表扫描 O(n)，在租约表规模下换取实现的简单与无锁序依赖。

## 候选事务

`Apply` 分三个阶段：

1. **结构校验**：先校验整个批次（kind 合法、key 为非空 ASCII `[a-z0-9-_]` 且不超过 `MaxKeyBytes`、`Now`/`ExpiresAt` 非负），任何一项失败返回 `ErrInvalidInput`，不读取状态。
2. **时间检查**：`Now < 当前时间` 返回 `ErrTime`。
3. **候选执行**：克隆当前条目，先在候选上淘汰 `ExpiresAt <= Now`（闭区间）的条目，再顺序执行 Put/Touch/Delete；Put/Touch 分配递增 revision，Touch/Delete 目标缺失返回 `ErrNotFound`。最终条目数超过 `MaxEntries` 返回 `ErrCapacity`。

任何错误都直接丢弃候选：淘汰、时间与 revision 一并回滚，状态保持提交前快照。全部成功才一次性提交，`now` 前进、`generation` 恰好加一（空批次不改变任何状态）。`Expire` 使用相同的闭区间边界与单调时间规则。

## 所有权

- 表独占内部 `entries` map；`Snapshot` 与 `Expire` 返回的切片均为新分配的拷贝，调用方可自由修改，不影响内部状态。
- 所有公开方法通过单个 `sync.Mutex` 串行化，支持任意并发调用；`go test -race` 干净。

## 复杂度

| 操作 | 时间 | 额外空间 |
| --- | --- | --- |
| `Apply`（m 个 op，n 个条目） | O(n + m) | O(n) 候选克隆 |
| `Expire` | O(n log n)（含排序） | O(k)，k 为淘汰数 |
| `Snapshot` | O(n log n)（含排序） | O(n) |
