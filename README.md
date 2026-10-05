# resourcelease119

并发安全的内存型“资源租约表 119”（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 索引

- 主索引为 `map[string]Entry`（键 → 租约条目），Put/Touch/Delete/Expire 均为 O(1) 均摊定位。
- 不维护额外的过期堆/有序索引：过期淘汰在 `Apply` 候选构建与 `Expire` 时按闭区间 `ExpiresAt <= Now` 全量扫描完成。

## 候选事务

`Apply` 分阶段执行，任何失败都整体回滚：

1. 对整个批次做完整结构校验（kind、键字符集与字节上限、`ExpiresAt >= 0`），不读取状态。
2. 时间检查：`Now` 必须非负且不早于当前表时间，否则 `ErrTime`。
3. 在候选副本上先淘汰 `ExpiresAt <= Now` 的条目，再顺序执行 Put/Touch/Delete；Put/Touch 分配递增 revision。
4. 最终容量超限（`ErrCapacity`）或任何错误都会连同淘汰、`Now`、revision、generation 一起回滚。
5. 仅当非空批次成功时提交候选状态，且 generation 只增加一次；空批次为无操作。

## 所有权

- 表内部状态（`entries` map）完全私有，`Snapshot` 与 `Expire` 返回的切片均为新建副本，调用方修改不影响表。
- 所有公开方法通过单一 `sync.Mutex` 串行化，支持任意并发调用。

## 复杂度

- `Apply`：O(E + O)，E 为当前条目数（构建候选并淘汰），O 为批内操作数。
- `Expire`：O(E + K log K)，K 为本次过期条目数（结果按键排序）。
- `Snapshot`：O(E log E)（返回按键排序的确定性快照）。
- 空间：O(E)。
