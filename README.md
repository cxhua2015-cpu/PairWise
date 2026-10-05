# rankboard

并发安全的内存加权排行榜，仅依赖 Go 标准库（Go 1.22+）。

## 事务语义

- `Apply` 先完整校验整个批次（ID 合法性、Kind、Delta/Score 字段约束），再读取任何状态。
- 操作按输入顺序作用于隔离的候选状态副本：Add 对缺失条目先以 0 创建再累加；Set 插入或替换；Delete 要求条目存在。
- 条目容量（`MaxItems`）只在批次末尾检查一次，因此批内"先删后增"可以成功。
- 任一操作失败（`ErrInvalidInput` / `ErrNotFound` / `ErrScore` / `ErrCapacity`）即整体回滚：条目、generation、revision 均不变。
- 成功的非空批次使 generation 恰好加一；空批次不改变任何状态，返回零值 `Result`。
- Add/Set 各自分配一个连续递增的 revision；Delete 不分配。`Result.Changed` 为批内被 Add/Set 触及且批次结束时仍存活的条目，按 ID 排序、无重复。

## 排序

- `Top(limit)`（limit 须在 1..1000）：按 Score 降序、ID 升序稳定排序，最多返回 limit 条。
- `Snapshot()`：返回全部条目，按 ID 升序；`NextRevision` 为下一个将分配的 revision。
- 所有返回的切片均为拷贝，调用方修改不影响内部状态。

## 算术

- Add 在做加法之前先检测 int64 上溢/下溢，再检查结果的绝对值是否超过 `MaxAbsScore`；违例返回 `ErrScore`。
- Set 在校验阶段即要求 Score 落在 `[-MaxAbsScore, MaxAbsScore]`，否则返回 `ErrInvalidInput`。

## 并发与复杂度

- 所有公开方法通过单一互斥锁串行化，可安全并发调用。
- 设批次含 k 个操作、当前 n 个条目：`Apply` 为 O(n + k)（克隆候选状态 + 执行），`Top` 为 O(n log n)，`Snapshot` 为 O(n log n)。

## 验证

```
go build ./...
go vet ./...
go test -race ./...
```
