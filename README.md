# expirytable219

并发安全的内存型“到期状态表”，仅依赖 Go 标准库（Go 1.22+）。语义详见 `SPEC.md`。

## 设计说明

**索引**：条目存储于 `map[string]Entry` 哈希索引，按 key 定位 O(1)。表不维护按过期时间排序的堆；淘汰（`Apply` 前置驱逐与 `Expire`）通过一次全表扫描完成，因为表容量受 `MaxEntries` 上限约束，扫描成本有界。`Snapshot` 与 `Expire` 返回的条目按 key 排序，保证确定性输出。

**候选事务**：`Apply` 分两阶段。第一阶段在不加锁的情况下对整个批次做纯结构校验（kind 合法、key 字符集与字节上限、`ExpiresAt >= 0`），失败返回 `ErrInvalidInput`，不触碰任何状态。第二阶段持锁检查时间单调性（`Now >= now` 且非负，否则 `ErrTime`），然后在候选副本上先驱逐 `ExpiresAt <= Now` 的条目，再按序执行 Put/Touch/Delete：Put 为 upsert，Touch/Delete 要求键存在（否则 `ErrNotFound`），Put/Touch 各分配一个递增 revision。最终容量超限返回 `ErrCapacity`。任何失败都直接丢弃候选副本——淘汰、时间与 revision 随之一并回滚，真实状态零变更。全部成功才提交：非空批次 generation 恰好 +1，空批次不改变 generation。

**所有权**：`Snapshot` 与 `Expire` 返回的切片均为新分配的深拷贝，调用方修改返回值不影响表内状态；表也不会保留对返回切片的引用。所有公开方法（`Apply`/`Expire`/`Snapshot`）由单一互斥锁保护，可安全并发调用。

**复杂度**：设批次含 k 个操作、表内 n 个条目（n ≤ MaxEntries）。`Apply` 为 O(n + k)（候选复制/驱逐扫描 + 顺序应用），`Expire` 为 O(n + m log m)（m 为到期条目数，排序），`Snapshot` 为 O(n log n)（拷贝并排序）。空间 O(n)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
