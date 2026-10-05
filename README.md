# resourcelease199

并发安全的内存型资源租约表，语义见 `SPEC.md`。仅依赖标准库，Go 1.22+。

## 索引

`Table` 内部以 `map[string]Entry` 为主索引，键即租约名称。未维护额外的堆或有序结构：
过期判定采用闭区间 `ExpiresAt <= Now` 的全量扫描，因为时间由调用方显式推进，
每次 `Apply`/`Expire` 本就只发生一次淘汰。`Snapshot` 与 `Expire` 返回的条目按 Key 排序，
保证输出确定性。

## 候选事务

`Apply` 分两阶段执行：

1. **校验**：先对整个批次做结构校验（kind 合法、键非空且仅含 `[a-z0-9-_]`、
   不超过 `MaxKeyBytes`），再检查时间单调性（`Now` 非负且不后退）。
2. **候选状态**：克隆当前条目表，先在候选上淘汰 `ExpiresAt <= Now` 的条目，
   再按顺序执行 Put/Touch/Delete；Put/Touch 从单调计数器分配 revision。
   最终条目数超过 `MaxEntries` 或任一操作失败（如 Touch/Delete 不存在的键）
   时直接丢弃候选——淘汰、时间与 revision 一并回滚，已发布状态不受影响。
   仅在全部成功时提交候选并推进 `now`；非空成功批次 `generation` 恰好加一，空批次不变。

## 所有权

所有公开方法通过单个互斥锁串行化，可并发调用。`Snapshot` 与 `Expire` 返回的切片
均为新分配的副本，调用方可自由修改，不会别名内部状态；`Entry` 为纯值类型，
不存在共享指针。`Options` 在 `New` 时按值拷贝，之后不可变。

## 复杂度

设 `n` 为当前条目数、`m` 为批次操作数：

- `Apply`：克隆与淘汰 `O(n)`，执行操作 `O(m)`，合计 `O(n + m)`。
- `Expire`：扫描 `O(n)`，结果排序 `O(k log k)`（`k` 为淘汰条数）。
- `Snapshot`：`O(n log n)`（拷贝 + 排序）。
- 空间：`O(n)`，每次成功 `Apply` 额外 `O(n)` 的临时候选副本。
