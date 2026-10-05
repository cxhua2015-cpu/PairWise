# resourcelease089

并发安全的内存型“资源租约表”，仅依赖 Go 标准库（Go 1.22+）。语义详见 `SPEC.md`。

## 索引

- 主索引为 `map[string]Entry`（键 → 租约条目），Put/Touch/Delete/Expire 均为 O(1) 均摊定位。
- `Snapshot` 与 `Expire` 的返回切片按字典序排序，保证输出确定性；排序为 O(n log n)。
- 除互斥锁与 map 外无额外索引结构，条目本身携带 `ExpiresAt` 与单调递增的 `Revision`。

## 候选事务

`Apply` 分两阶段：

1. **结构校验**：在读取任何状态前完整校验整个批次（时间非负、kind 合法、Delete 不得携带 `ExpiresAt`、键为非空 ASCII 小写/数字/连字符/下划线且不超过 `MaxKeyBytes`），失败返回 `ErrInvalidInput`。
2. **候选执行**：校验通过后加锁检查时间单调性（`Now < 当前时间` 返回 `ErrTime`），随后在候选 map 上先淘汰 `ExpiresAt <= Now` 的条目，再顺序执行 Put/Touch/Delete，Put/Touch 分配递增 revision。最终容量超限（`ErrCapacity`）或任何错误（如 `ErrNotFound`）发生时，候选状态被直接丢弃——淘汰、时间与 revision 一并回滚，表保持原状。

只有全部成功才提交候选状态、推进时间；非空成功批次 `generation` 恰好加一，空批次不变。`Expire` 使用相同闭区间边界（`ExpiresAt <= now`）并同样推进单调时间。

## 所有权

- 表内部状态不对外暴露引用：`Snapshot` 与 `Expire` 返回的切片均为新分配的拷贝，调用方修改返回值不影响表。
- `New` 拷贝 `Options`，之后修改传入值不影响表。

## 并发与复杂度

- 所有公开方法（`Apply`/`Expire`/`Snapshot`）通过单一 `sync.Mutex` 串行化，可安全并发调用；已通过 `go test -race` 验证。
- 时间复杂度：`Apply` 为 O(n + m)（n 为现存条目数，用于候选淘汰与克隆；m 为批次操作数）；`Expire` 为 O(n + k log k)（k 为过期条目数）；`Snapshot` 为 O(n log n)；空间 O(n)。
