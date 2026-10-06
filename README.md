# expirytable204

并发安全的内存型“到期状态表 204”，仅依赖 Go 标准库（Go 1.22+）。语义详见 `SPEC.md`。

## 索引结构

- **主索引**：`map[string]Entry`，键到当前条目的 O(1) 映射，Entry 为不可变值类型。
- **到期索引**：基于 `(ExpiresAt, Key)` 排序的惰性最小堆。堆节点携带 `Revision`，
  与主索引中条目不一致的节点视为陈旧并在弹出时跳过，因此 Put/Touch/Delete 无需
  维护堆中位置，只追加新节点。

## 候选事务（Apply）

1. 对整个批次做完整结构校验（kind、键字符集与字节上限），任何非法输入返回
   `ErrInvalidInput`，此时不读取任何状态。
2. 校验时间：`Now` 必须非负且不早于当前表时间，否则 `ErrTime`。
3. 在候选副本（克隆的 map 与堆）上先淘汰 `ExpiresAt <= Now` 的条目，再顺序执行
   Put/Touch/Delete；Put/Touch 各分配一个递增 revision。
4. 最终条目数超过 `MaxEntries` 或任何一步出错（`ErrNotFound` 等）时，候选副本
   直接丢弃——淘汰、时间与 revision 一并回滚，表状态保持不变。
5. 全部成功后一次性提交：非空批次 generation 恰好加一，空批次不改变任何状态。

`Expire(now)` 使用相同的闭区间边界（`ExpiresAt <= now`）在真实状态上淘汰，并推进表时间。

## 所有权与并发

- 所有公开方法通过单个 `sync.Mutex` 串行化，可安全并发调用。
- `Snapshot` 与 `Expire` 返回的切片均为新分配的副本，调用方修改不会影响内部状态。
- `Entry`/`Result`/`Snapshot` 均为纯值类型，表不保留调用方传入切片的引用。

## 复杂度

- Put/Touch/Delete：均摊 O(log H)，H 为堆中节点数（含待清理的陈旧节点）。
- Apply 批次：O(E + H + k·log H)，E 为当前条目数（候选克隆），k 为批次内操作数。
- Expire：O(m·log H)，m 为实际淘汰加陈旧节点数。
- Snapshot：O(E·log E)（按键排序输出）。
- 空间：O(E + H)。
