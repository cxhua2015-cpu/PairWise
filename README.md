# resourcelease094

并发安全的内存型资源租约表（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

**索引**：`Table` 内部以 `map[string]Entry` 为主索引，键即租约名称；不维护额外的过期堆——过期条目在 `Apply`/`Expire` 时以一次全表扫描按闭区间 `ExpiresAt <= Now` 淘汰。`Snapshot` 与 `Expire` 返回的条目按 Key 排序，保证输出确定性。

**候选事务**：`Apply` 分两阶段。第一阶段在不持锁的情况下对整个批次做纯结构校验（kind 合法、键非空且仅含 `[a-z0-9-_]`、键长与 `ExpiresAt`/`Now` 非负）。第二阶段持锁后先检查时间单调性（`Now < now` 返回 `ErrTime`），然后在候选副本上先淘汰 `ExpiresAt <= Now` 的条目，再顺序执行 Put/Touch/Delete，其中 Put/Touch 各分配一个递增 revision。只有当全部操作成功且最终容量不超过 `MaxEntries` 时才一次性提交；任何错误（`ErrNotFound`、`ErrCapacity` 等）都会连同淘汰、时间与 revision 一起回滚，原状态不变。非空成功批次 generation 恰好加一，空批次不改变 generation（但仍推进时间并执行淘汰）。

**所有权**：所有公开方法通过单一互斥锁串行化，可并发调用。`Snapshot` 与 `Expire` 返回的切片均为新分配的副本，调用方修改返回值不会影响内部状态；`Entry`/`Snapshot` 为值语义，不存在共享指针。

**复杂度**：设 `n` 为当前条目数、`m` 为批次操作数。`Apply` 为 O(n + m)（候选复制 + 顺序执行），`Expire` 为 O(n log n)（扫描 + 排序），`Snapshot` 为 O(n log n)（拷贝 + 排序），`New` 为 O(1)。所有操作空间复杂度 O(n)。

## 验证

```
go test ./...
go test -race ./...
go run ./cmd/demo
```
