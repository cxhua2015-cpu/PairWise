# resourcelease089

并发安全的内存型资源租约表（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计

### 索引
- 主索引为 `map[string]Entry`，按键存储 `ExpiresAt` 与 `Revision`，Put/Touch/Delete/查询均为 O(1) 均摊。
- 无堆或有序索引：过期淘汰采用全量扫描（O(n)），`Snapshot`/`Expire` 返回前按键排序以保证确定性输出。

### 候选事务
- `Apply` 先对整个批次做结构校验（kind、键字符集与长度、非负时间），再做单调时间检查。
- 通过后在与互斥锁保护的候选副本上先淘汰 `ExpiresAt <= Now` 的条目（闭区间），再顺序执行 Put/Touch/Delete；Put/Touch 从 `nextRevision` 分配递增 revision。
- 最终容量检查失败或任何错误（`ErrNotFound`/`ErrCapacity`/`ErrTime`）时直接丢弃候选：条目、淘汰结果、`now`、`generation`、`nextRevision` 全部回滚。只有全部成功才一次性提交，非空成功批次 `generation` 恰好加一，空批次完全不变。
- `Expire` 使用相同闭区间边界（`ExpiresAt <= now`），推进 `now` 并返回被淘汰条目的拷贝。

### 所有权与并发
- 所有公开方法由单个 `sync.Mutex` 保护，可安全并发调用；锁内不调用外部代码。
- 返回的 `[]Entry`/`Snapshot` 均为新分配的拷贝，调用方修改不会影响内部状态。

### 复杂度
- `Apply`：O(n + m)，n 为当前条目数（候选复制与淘汰扫描），m 为批次操作数。
- `Expire`：O(n + k log k)，k 为被淘汰条目数（排序）。
- `Snapshot`：O(n log n)（拷贝并排序）。
- 空间：O(n)。
