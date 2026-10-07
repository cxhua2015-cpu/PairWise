# expirytable384

并发安全的内存型“到期状态表”，仅依赖 Go 标准库（Go 1.22+）。语义见 `SPEC.md`。

## 索引

- 主索引为 `map[string]Entry`，键到条目 O(1) 定位。
- `Snapshot` 与 `Expire` 返回的切片按键字典序排序（规范顺序），保证输出确定性。
- 未维护额外的堆/时间轮：到期淘汰在候选事务内以一次全表扫描完成，条目数受 `Options.MaxEntries` 上限约束，扫描成本有界。

## 候选事务

`Apply` 分两阶段执行：

1. **校验**：先对整个批次做结构校验（kind、键字符集与长度、非负时间），再做单调时间检查（`Now < 当前时间` 返回 `ErrTime`）。空批次仅校验，不改变任何状态。
2. **候选执行**：复制当前条目表为候选状态，先在候选上删除 `ExpiresAt <= Now`（闭区间）的条目，再按顺序执行 Put/Touch/Delete；Put/Touch 各分配一个递增 revision。最后做容量检查。

任何一步失败（`ErrNotFound` / `ErrCapacity` 等）直接丢弃候选，淘汰、时间、revision、generation 全部回滚；只有非空成功批次才提交候选并将 generation 加一。

## 所有权

- 表内部状态由单个 `sync.Mutex` 保护，所有公开方法（`Apply` / `Expire` / `Snapshot`）可并发调用。
- `Snapshot` 与 `Expire` 返回的切片均为新建副本，调用方修改不影响内部状态；条目为值类型，无共享指针。

## 复杂度

设 n 为当前条目数，m 为批次内操作数：

- `Apply`：O(n + m)（候选复制 + 顺序执行），额外空间 O(n)。
- `Expire`：O(n + k log k)，k 为到期条目数（排序）。
- `Snapshot`：O(n log n)（复制并排序）。
- `New`：O(1)。
