# rangelock

并发安全的内存区间读写租约锁管理器（Go 1.22+，仅标准库）。公开契约见 `SPEC.md`。

## 设计说明

- **索引**：`Manager` 持有一把 `sync.Mutex` 和 `map[id]*lockRecord`。锁记录内联 resource、区间、模式、token、到期时间与 metadata 副本，按 ID O(1) 定位；资源与区间维度不做额外索引。
- **冲突检测**：半开区间 `[Start,End)`，重叠判定为 `a.Start < b.End && b.Start < a.End`，相邻不冲突。读-读可共存，任一方为写即冲突，owner 不豁免。批量检测对候选状态做 O(n²) 扫描。
- **事务（AcquireBatch）**：先做完整结构校验（名称长度、`Start<End`、模式、正 TTL、`now+TTL` 不溢出、单条 metadata 上限、批内 ID 去重），任何结构错误都不触碰状态。然后在隔离的候选 map 上先清理 `now >= ExpiresAt` 的到期锁，再按输入顺序分配单调递增的非零 fencing token 并检查冲突，最后统一校验锁数、owner 数与 metadata 总字节容量。任何失败整体回滚，不消耗 token、不推进 generation；成功才一次性提交并使 generation 恰好 +1。
- **租约**：`Renew` 仅在 token 匹配且 `now < ExpiresAt` 时成功（严格边界），`Release` 无时间参数，可释放尚未清扫的到期锁。失败操作是精确无操作。`Sweep` 按 ID 升序删除到期锁，`limit==0` 表示不限，仅在实际删除时推进 generation。
- **容量**：锁总数、去重 owner 数、存储 metadata 总字节均在批次末尾对候选整体校验，超限返回 `ErrCapacity`。
- **所有权**：metadata 在写入、提交与每次返回（Acquire/Query/Snapshot）时均深拷贝，返回值与输入及内部状态互不别名。
- **复杂度**：AcquireBatch 为 O(B·N)（B 为批量大小，N 为现存锁数）；Renew/Release 为 O(1)；Query/Snapshot/Sweep 为 O(N log N)（排序）。所有公开方法可并发调用。

## 验证

```
go test ./...
go test -race ./...
go run ./cmd/demo
```
