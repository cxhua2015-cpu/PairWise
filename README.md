# resourcelease099

并发安全的内存型“资源租约表 099”，Go 1.22+，仅依赖标准库。语义详见 `SPEC.md`。

## 索引结构

- 主索引为 `map[string]Entry`，键即租约键，读写均为 O(1) 均摊。
- 不维护额外的堆或有序索引；过期判定在 `Apply`/`Expire` 时线性扫描完成。
- `Snapshot` 与 `Expire` 的返回切片按键名字典序排序，保证输出确定性。

## 候选事务

`Apply` 采用候选状态（copy-on-write）事务模型：

1. 先对全部操作做结构校验（kind、键字符集与字节上限、非负 ExpiresAt），再检查时间单调性（`Now < now` 返回 `ErrTime`）。
2. 克隆当前 map 得到候选状态，先在候选上淘汰 `ExpiresAt <= Now` 的条目（闭区间），再顺序执行 Put/Touch/Delete；Put/Touch 各分配一个递增 revision。
3. 最终容量超限（`ErrCapacity`）或任何错误（`ErrNotFound` 等）发生时直接丢弃候选，淘汰、时间与 revision 一并回滚，已提交状态不受影响。
4. 仅在全部成功时提交候选，非空批次 generation 恰好加一；空批次不改变任何状态。

## 所有权与并发

- 所有公开方法通过单一 `sync.Mutex` 串行化，可安全并发调用。
- 内部 `Entry` 为值类型；`Snapshot` 与 `Expire` 返回新建的切片，调用方对返回值的修改不会泄漏进表内状态。
- 表内时间显式、非负且单调：`Apply` 与 `Expire` 共用同一时钟，回退即 `ErrTime`。

## 复杂度

- `Apply`：O(n + m)，n 为当前条目数（克隆 + 淘汰扫描），m 为批内操作数。
- `Expire`：O(n + k log k)，k 为被淘汰条目数（排序）。
- `Snapshot`：O(n log n)（排序）。
- 空间：O(n)。
