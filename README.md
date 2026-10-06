# expirytable259

并发安全的内存型到期状态表（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.

- `heartbeat.go` — 核心类型、错误值与事务引擎（`New`/`Apply`/`Expire`/`Snapshot`）。
- `validation.go` — 无副作用批次预检（`ValidateBatch`），与 `Apply` 共享同一套结构语义。
- `stats.go` — 线性一致的状态统计（`Stats`）。
- `clone.go` — 保留逻辑时钟、所有权完全隔离的深拷贝（`Clone`）。

## 索引

主索引为 `map[string]Entry`，按键 O(1) 定位；`Snapshot`/`Expire` 返回的切片按键排序以保证确定性输出。表内不维护额外的堆或时间轮——到期淘汰在候选事务与 `Expire` 中通过一次全量扫描完成，条目数受 `MaxEntries` 上限约束，扫描成本有界。

## 候选事务

`Apply` 先执行完整结构校验（不读状态），再在写锁内检查单调时间，随后把存活条目（`ExpiresAt > Now`）复制到候选 map：候选中先淘汰 `ExpiresAt <= Now` 的条目，再顺序执行 Put/Touch/Delete，Put/Touch 各分配一个 revision。最终容量超限或任何错误（`ErrNotFound`/`ErrCapacity` 等）直接丢弃候选，淘汰、时间与 revision 一并回滚；全部成功才一次性提交，非空批次 generation 恰好加一。

## 所有权

所有公开方法通过单把 `sync.RWMutex` 并发安全：写路径（`Apply`/`Expire`）持写锁，读路径（`Snapshot`/`Stats`/`Clone`）持读锁。`Snapshot`、`Expire` 返回的切片均为新建拷贝，`Clone` 深拷贝整个条目 map——调用方与表之间、原表与克隆体之间不共享任何可变内存。

## 复杂度

- `Apply`：O(n + m)，n 为存活条目数（候选复制 + 淘汰扫描），m 为批次操作数。
- `Expire`：O(n + k log k)，k 为到期条目数（结果排序）。
- `Snapshot`/`Clone`：O(n log n) / O(n)。
- `Stats`：O(1)。
- 空间：O(n)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
