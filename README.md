# resourcelease119

并发安全的内存型资源租约表（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

### 索引
- 主索引为 `map[string]Entry`，按键（非空 ASCII 小写字母/数字/`-`/`_`，长度受 `MaxKeyBytes` 限制）O(1) 定位。
- 到期不做堆索引：`Apply`/`Expire` 时按 `ExpiresAt <= Now` 闭区间全表扫描淘汰，实现简单且与单调时间语义一致。
- `Snapshot` 与 `Expire` 返回的条目按键排序，保证输出确定性。

### 候选事务
- `Apply` 先在锁外对整批做结构校验（kind、键、非负 `ExpiresAt`），再在锁内检查时间单调性（`Now >= now`，否则 `ErrTime`）。
- 随后在候选副本（条目 map 的浅拷贝 + 局部 `nextRevision`）上先淘汰过期条目，再顺序执行 Put/Touch/Delete；Put/Touch 各分配一个 revision。
- 最终容量超限（`ErrCapacity`）或任何错误（`ErrNotFound` 等）直接丢弃候选：淘汰、时间与 revision 一并回滚，原状态零副作用。
- 全部成功才一次性提交；非空批次 generation 恰好 +1，空批次不变。

### 所有权
- 所有公开方法由单一 `sync.Mutex` 保护，可并发调用。
- 表不保留调用方传入的切片；`Snapshot`/`Expire` 返回的切片均为新建副本，调用方可自由修改，不影响内部状态。

### 复杂度
- `Apply`：O(n + m)，n 为现存条目数（拷贝 + 淘汰扫描），m 为批内操作数。
- `Expire`：O(n + k log k)，k 为到期条目数（排序）。
- `Snapshot`：O(n log n)（排序）；空间 O(n)。
