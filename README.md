# windowlimit

并发安全、显式时间驱动的内存滑动窗口限流器（Go 1.22+，仅标准库）。完整语义见 `SPEC.md`。

## 用法

```go
l, _ := windowlimit.New(windowlimit.Options{Window: 10, Limit: 5, MaxKeys: 8, MaxKeyBytes: 16, MaxEventsPerKey: 8})
res, err := l.Check(windowlimit.Batch{Now: 1, Requests: []windowlimit.Request{{Key: "api", Units: 3}}})
snap := l.Snapshot()
```

## 设计说明

**索引**：状态为 `map[string][]Event`，键到其事件切片。每个键的事件按提交顺序追加，由于时间单调不减且 revision 严格递增，切片天然按 `(At, Revision)` 有序，淘汰时只需从头扫描前缀，无需排序。

**事务**：`Check` 先在持锁状态下把全部键事件深拷贝到隔离候选状态，在候选上依次执行淘汰、按输入顺序判定、容量校验；全部成功才一次性提交（替换 map、推进 `now`、递增 `generation` 与 `nextRevision`）。批次结构错误（`ErrInvalidInput`，优先于时间错误）、时间倒退（`ErrTime`）、最终容量超限（`ErrCapacity`）都在提交前返回，状态完全不变，实现整体回滚。仅允许的请求追加事件并消耗一个 revision；被拒绝的请求不写事件、不耗 revision。空批次合法，可推进时间并触发淘汰；有淘汰或有允许请求时 `generation` 恰好加一，否则不变。

**窗口边界**：窗口为 `(Now-Window, Now]` 的左开区间。淘汰条件是 `At <= Now-Window`，即恰好落在左边界上的事件被移除；`Now-Window+1` 处的事件仍计入用量。

**容量**：`MaxKeys` 限制最终非空键数，`MaxEventsPerKey` 限制每键存活事件数，二者在整批请求执行完后统一校验，超限即整体回滚。`Limit` 是每键窗口内带权用量上限，`Units` 必须为正且不超过 `Limit`。

**复杂度**：设批次大小为 B、存活事件总数为 E、键数为 K。单次 `Check` 为 O(E + B)（候选拷贝与淘汰扫描全部事件，每个请求 O(1) 摊还判定，每键用量惰性求和）；`Snapshot` 为 O(E + K log K)（键名排序加拷贝）。所有公开方法由单一互斥锁保护，并发安全；`Snapshot` 返回的切片为独立拷贝，调用方修改不影响限流器。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
