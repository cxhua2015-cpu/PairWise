# devicelease

Read `SPEC.md` and implement the package.

## 设计说明

### 索引
租约条目存储在 `map[string]Entry` 哈希索引中，按键（设备名）直接定位，Put/Touch/Delete 均为 O(1) 均摊查找。快照与过期结果按字典序排序后返回，保证输出确定性。

### 候选事务
`Apply` 先在锁外完成整批结构校验（kind、键字符集与长度、非负 ExpiresAt），再在写锁内检查单调时间。随后在候选副本（当前条目的克隆）上先淘汰 `ExpiresAt <= Now` 的条目，再顺序执行 Put/Touch/Delete；Put/Touch 从候选 revision 计数器分配新版本。最终容量超限或任一 op 失败（`ErrNotFound`/`ErrCapacity`）时直接丢弃候选，淘汰、时间、revision 与 generation 全部回滚；成功才一次性提交。非空成功批次 generation 恰好加一，空批次不变。

### 所有权
所有公开方法通过 `sync.RWMutex` 并发安全：`Apply`/`Expire` 持写锁，`Snapshot` 持读锁。`Snapshot` 与 `Expire` 返回的切片均为新分配的副本，调用方修改不影响表内状态；表也不保留调用方传入的切片。

### 复杂度
设批次大小为 B、表内条目数为 N：
- `Apply`：O(N + B)（克隆候选 + 顺序执行），额外 O(N) 空间；
- `Expire`：O(N + E log E)，E 为过期条目数（排序）；
- `Snapshot`：O(N log N)（拷贝并排序）；
- 单 op 查找/更新：O(1) 均摊。
