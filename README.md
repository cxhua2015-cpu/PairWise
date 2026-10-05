# certificatelease

并发安全的内存型证书租约表（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 索引

活跃条目仅以 key 为索引存放在一个 `map[string]Entry` 中，由 `sync.RWMutex`
保护。刻意不维护过期时间的二级索引，因此淘汰扫描是对候选 map 的线性遍历。

## 候选事务

`Apply` 先对整个批次做完整结构校验（不触碰状态），再检查时间单调性，
然后在候选副本上执行：先删除 `ExpiresAt <= Now` 的条目（闭区间），再按序执行
Put/Touch/Delete，Put/Touch 分配递增 revision。最终容量检查失败或任何错误都会
整体回滚——淘汰、时间与 revision 一并丢弃，因为候选只有全部成功才替换正式状态。
`Expire` 使用相同的闭区间边界。非空成功批次 generation 只增加一次，空批次不变。

## 所有权

`Snapshot` 与 `Expire` 返回的切片均为独立拷贝，调用方修改不影响表内状态。

## 复杂度

- `Apply`：O(E + N)，E 为当前条目数（拷贝 + 淘汰扫描），N 为批内 op 数。
- `Expire`：O(E + R log R)，R 为被移除条目数（排序）。
- `Snapshot`：O(E log E)（拷贝并按键排序）。
