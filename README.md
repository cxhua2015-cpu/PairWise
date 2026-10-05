# resourcelease164

并发安全的内存型资源租约表（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计说明

### 索引
- 主索引为 `map[string]Entry`，键即租约键，Put/Touch/Delete/Expire 均为 O(1) 均摊定位。
- 不维护额外的过期堆；淘汰（Apply 前扫描与 `Expire`）采用全表扫描，O(n)。
- `Snapshot` 与 `Expire` 返回的条目按键排序，保证输出确定性。

### 候选事务
- `Apply` 分两阶段：先对整个批次做完整结构校验（时间非负、kind 合法、键字符集与长度上限），再检查时间单调性（`Now < now` 返回 `ErrTime`）。
- 校验通过后在候选副本上执行：先删除 `ExpiresAt <= Now` 的条目（闭区间），再顺序执行 Put/Touch/Delete；Put/Touch 各分配一个递增 revision。
- 任一操作失败（`ErrNotFound`）或最终容量超限（`ErrCapacity`）时整体回滚：淘汰、时间与 revision 分配均不生效，内部状态保持 Apply 前的样子。
- 成功提交时原子替换条目映射；非空批次 generation 恰好加一，空批次不变。

### 所有权
- 所有公开方法（`Apply`/`Expire`/`Snapshot`）由同一把互斥锁保护，可并发调用。
- 返回的 `Snapshot.Entries` 与 `Expire` 结果均为新分配的切片与条目副本，调用方修改不影响内部状态。

### 复杂度
- `Apply`：O(n + m)，n 为当前条目数（候选复制与淘汰扫描），m 为批次数。
- `Expire`：O(n + k log k)，k 为过期条目数（排序）。
- `Snapshot`：O(n log n)（排序）。
- 空间：O(n)。
