# expirytable314

并发安全的内存型“到期状态表”，仅依赖 Go 标准库（Go 1.22+）。语义详见 `SPEC.md`。

## 设计说明

**索引**：条目存储在 `map[string]Entry` 中，按键 O(1) 定位。`Snapshot` 与 `Expire` 返回的切片在锁内按键排序，保证确定性输出。

**候选事务**：`Apply` 先在锁外完成整批结构校验（kind、键字符集与长度、非负时间），再在锁内检查单调时间。随后在候选状态（当前条目的副本）上先淘汰 `ExpiresAt <= Now` 的条目，再顺序执行 Put/Touch/Delete；Put/Touch 分配递增 revision。只有在所有操作成功且最终容量不超限的情况下才整体提交；任何错误（`ErrNotFound`、`ErrCapacity` 等）都会连同淘汰、时间和 revision 一起回滚，已观察状态不变。非空成功批次 generation 只增加一次，空批次不变。

**所有权**：`Expire` 与 `Snapshot` 返回的切片均为新分配的副本，调用方可自由修改，不影响表内状态；表也不会保留对返回切片的引用。

**并发**：所有公开方法通过单个 `sync.Mutex` 串行化，支持任意并发调用；`go test -race` 下无数据竞争。

**复杂度**：设批次含 `k` 个操作、表内 `n` 个条目——
- `Apply`：结构校验 O(k)，候选复制与淘汰 O(n)，执行 O(k)，总计 O(n + k)。
- `Expire`：O(n log n)（扫描 O(n) + 结果排序）。
- `Snapshot`：O(n log n)（复制 + 排序）。
- 空间：O(n)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
