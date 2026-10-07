# expirytable409

并发安全的内存型“到期状态表”，使用显式非负单调逻辑时钟，仅依赖标准库（Go 1.22+）。

## 架构与索引

- `heartbeat.go` — 核心事务引擎。`Table` 以 `map[string]Entry` 作为主索引（按键 O(1) 定位），
  由一把 `sync.Mutex` 保护全部内部状态，所有公开方法可并发调用。
- `validation.go` — 无副作用的批次结构预检。`ValidateBatch` 与 `Apply` 共享同一套
  `validateBatch`/`validateKey` 语义，只读取不可变的 `Options`，不触碰表状态。
- `stats.go` — `Stats` 在同一把锁内读取，得到线性一致的汇总（generation、nextRevision、now、条目数）。
- `clone.go` — `Clone` 在锁内逐条复制 map，保留逻辑时钟（now/generation/nextRevision），
  克隆体与原表所有权完全隔离，互不影响。

## 候选事务与回滚

`Apply` 先完整结构校验（未知 kind、非法键、负时间、`ExpiresAt <= Now` 的 Put/Touch 均返回
`ErrInvalidInput`），再检查单调时间（`ErrTime`）。随后在候选副本上先淘汰 `ExpiresAt <= Now`
的条目，再顺序执行 Put/Touch/Delete（Put/Touch 分配递增 revision，Touch/Delete 缺失键返回
`ErrNotFound`）。最终容量超限（`ErrCapacity`）或任何错误都会连同淘汰、时间和 revision 一起回滚，
原表状态不变。非空成功批次 generation 只增加一次，空批次不变。`Expire` 使用相同的闭区间边界。

## 所有权

`Snapshot`、`Expire` 返回的切片均为独立副本，调用方修改不会影响内部状态；`Clone` 的深拷贝
与原表无任何共享内存。

## 复杂度

- `Apply`：O(E + B)，E 为当前条目数（候选复制与淘汰），B 为批次内 op 数。
- `Expire`：O(E + K log K)，K 为到期条目数（结果按键排序，保证确定性输出）。
- `Snapshot` / `Clone`：O(E log E) / O(E)。
- `Stats` / `ValidateBatch`：O(1) / O(B)。
- 空间：O(E)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
