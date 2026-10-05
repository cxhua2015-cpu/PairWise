# sessiontable

并发安全的内存型会话到期表（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

**索引**：主索引为 `map[string]Entry`，按键 O(1) 定位。到期扫描（Apply 候选淘汰与 `Expire`）为全表 O(n) 遍历；会话表规模受 `MaxEntries` 上限约束，且实现零第三方依赖，故未引入堆/时间轮。`Snapshot` 与 `Expire` 返回的条目按键排序，保证输出确定性。

**候选事务**：`Apply` 先在锁外对整批 Op 做纯结构校验（kind 合法、键非空且仅含 `[a-z0-9-_]`、长度 ≤ `MaxKeyBytes`），再在锁内检查时间单调性（`Now >= 0` 且不小于当前时间）。随后在候选副本上先淘汰 `ExpiresAt <= Now`（闭区间）的条目，再顺序执行 Put/Touch/Delete，Put/Touch 从候选 revision 计数器分配版本号。最终容量超限或任何错误发生时直接丢弃候选，淘汰、时间与 revision 一并回滚；仅全部成功才提交，且非空批次 generation 恰好 +1。

**所有权**：`Snapshot` 与 `Expire` 返回的切片均为新分配的拷贝，调用方可自由修改，不影响表内状态；表也不会保留对返回数据的引用。

**并发**：所有公开方法通过单一 `sync.Mutex` 串行化，支持任意并发调用；批次语义为线性化的全有或全无。

**复杂度**：`Apply` 为 O(n + m)（n 为表大小，m 为批大小）；`Expire` 与 `Snapshot` 为 O(n log n)（含排序）；`New` 为 O(1)。空间 O(n)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
