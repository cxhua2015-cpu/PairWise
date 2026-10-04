# rangelock

并发安全的内存区间读写租约锁管理器（Go 1.22+，仅标准库）。公开契约见 `SPEC.md`。

## 设计说明

- **索引**：单把 `sync.Mutex` 保护全部状态；锁按 ID 存于 `map[string]*lockEntry`，另维护单调递增的 `generation` 与非零 fencing token 计数器 `nextToken`。无第三方依赖。
- **冲突检测**：同一 resource 上按半开区间 `[Start,End)` 判定重叠（`a.Start < b.End && b.Start < a.End`），相邻区间不冲突；任一方为 `Write` 即冲突，重叠 `Read` 共存，owner 身份不豁免。批量获取时新请求与候选态中全部锁逐一比较。
- **事务**：`AcquireBatch` 先做完整结构校验（含批内重复 ID），再在隔离候选 map 上清理 `now >= ExpiresAt` 的到期锁、按输入顺序分配 token 并检查冲突，最后统一校验锁数、owner 数与 metadata 字节容量。任何失败直接丢弃候选态——不剪到期锁、不消耗 token、不推进 generation；成功时整体替换锁表并只推进一次 generation。
- **租约**：`Renew` 要求结构合法、token 匹配且 `now < ExpiresAt`（严格边界），成功替换截止时间；`Release` 无时间参数，token 匹配即可释放（包括尚未清扫的到期锁）；失败操作均为精确无操作。`Sweep` 按 ID 升序删除到期锁，`limit==0` 表示不限，仅在实际删除时推进 generation。
- **容量**：`MaxLocks`、`MaxOwners`（去重 owner 数）、`MaxMetadataBytes`（存储的 metadata 总字节）在整批应用之后统一检查，超限返回 `ErrCapacity` 并整体回滚。
- **所有权**：metadata 在提交时深拷贝存入，在 `AcquireBatch`/`Query`/`Snapshot` 返回时再次深拷贝，输入、内部状态与多次返回值之间互不共享底层数组。
- **复杂度**：单批次冲突检测为 O(b·n)（b 为批量大小，n 为现存锁数，最坏 O(n²)）；`Query`/`Snapshot`/`Sweep` 为 O(n log n) 排序；`Renew`/`Release` 为 O(1)。所有方法可在任意 goroutine 并发调用。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
