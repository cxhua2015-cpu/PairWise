# expirytable354

并发安全的内存型“到期状态表”，实现见 `expirytable354/heartbeat.go`，语义以 `SPEC.md` 与契约测试为准。

## 索引

- 主索引为 `map[string]Entry`，键即条目键，Put/Touch/Delete/Expire 均为 O(1) 均摊定位。
- `Snapshot` 与 `Expire` 的返回切片按键排序，保证输出确定性。

## 候选事务

- `Apply` 先对整个批次做结构校验（kind、键字符集与字节上限、非负时间），再检查单调时间。
- 校验通过后在与真实状态隔离的候选 map 上先淘汰 `ExpiresAt <= Now` 的条目，再顺序执行 Put/Touch/Delete；Put/Touch 从候选 revision 计数器分配版本。
- 任一步失败（ErrNotFound）或最终容量超限（ErrCapacity）时直接丢弃候选，淘汰、时间与 revision 一并回滚；成功时一次性提交并将 generation 加一。空批次不改变任何状态。

## 所有权

- 表内部状态（entries、now、generation、nextRevision）由单个 `sync.Mutex` 保护，所有公开方法可并发调用。
- `Snapshot` 与 `Expire` 返回的切片均为新分配的副本，调用方修改不会影响内部状态。

## 复杂度

- `Apply`：O(n + b)，n 为当前条目数（候选复制与淘汰扫描），b 为批内操作数。
- `Expire`：O(n + k log k)，k 为到期条目数（排序）。
- `Snapshot`：O(n log n)（复制并排序）。
- 空间：O(n)。
