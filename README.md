# resourcelease159

并发安全的内存型资源租约表（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

**索引**
- 主索引为 `map[string]Entry`，键到租约条目 O(1) 定位。
- 未维护按过期时间的堆/有序索引：过期淘汰在 `Apply`/`Expire` 时线性扫描完成，条目数受 `MaxEntries` 上限约束，实现更简单且无额外写路径开销。

**候选事务**
- `Apply` 先在锁外对整个批次做纯结构校验（kind、键字符集与字节上限、`ExpiresAt >= 0`），再在锁内检查时间单调性（`Now >= 0 && Now >= 当前时间`）。
- 校验通过后，在候选副本（复制当前 map，先剔除 `ExpiresAt <= Now` 的条目）上顺序执行 Put/Touch/Delete，Put/Touch 从局部 `nextRevision` 起分配 revision。
- 任一步失败（`ErrNotFound`）或最终容量超限（`ErrCapacity`）直接返回，候选副本被丢弃：淘汰、时间、revision、generation 全部天然回滚。
- 仅当全部成功时才一次性提交：替换 map、推进 `now` 与 `nextRevision`，非空批次 `generation` 恰好加一，空批次不产生任何变化。

**所有权**
- 所有公开方法（`Apply`/`Expire`/`Snapshot`）由同一把 `sync.Mutex` 保护，可并发调用。
- `Snapshot` 与 `Expire` 返回的切片均为新建副本，调用方修改不会影响内部状态；`Entry` 为纯值类型，无共享指针。

**复杂度**
- `Apply`：O(n + m)，n 为当前条目数（候选复制与淘汰扫描），m 为批次内操作数；每次操作 O(1)。
- `Expire`：O(n + k log k)，k 为过期条目数（按字典序排序返回）。
- `Snapshot`：O(n log n)（排序保证确定性输出）。
- 空间：O(n)，n ≤ `MaxEntries`。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
