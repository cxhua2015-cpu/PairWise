# expirytable244

并发安全的内存型“到期状态表 244”（Go 1.22+，仅标准库）。表使用调用方提供的显式非负单调时钟；详细语义见 `SPEC.md`。

## 架构与多文件联动

- `heartbeat.go` — 核心类型与事务引擎：`New` / `Apply` / `Expire` / `Snapshot`。
- `validation.go` — 无副作用的批次结构预检：`validateBatchStruct` 是 `Apply` 与 `ValidateBatch` 共享的唯一结构语义来源（键字符集与长度、未知 kind、Put/Touch 要求 `ExpiresAt > Now`）。
- `stats.go` — `Stats` 在同一把互斥锁内读取，给出线性一致的 `Generation` / `NextRevision` / `Now` / `Entries` 摘要。
- `clone.go` — `Clone` 在锁内复制全部条目与逻辑时钟（`now`、`generation`、`nextRevision`），产出所有权完全独立的深拷贝。

## 索引

条目存放在 `map[string]Entry` 哈希索引中，按键 O(1) 定位；`Snapshot`/`Expire` 返回的切片按键排序以保证确定性，且均为拷贝，与内部状态隔离。

## 候选事务与回滚

`Apply` 先结构校验、再检查时钟（负数或回退返回 `ErrTime`），然后在候选副本上先淘汰 `ExpiresAt <= Now` 的条目，再顺序执行 Put/Touch/Delete（Put/Touch 分配递增 revision）。最终容量超限或任何错误直接丢弃候选，淘汰、时间与 revision 随之一并回滚；只有全部成功才一次性提交。非空成功批次 `generation` 恰好加一，空批次不改变任何状态。`Expire` 使用相同的闭区间边界（`ExpiresAt <= now`）。

## 所有权与并发

所有公开方法通过单把 `sync.Mutex` 串行化，可安全并发调用。`Snapshot`、`Expire`、`Clone` 返回的数据均为深拷贝，调用方修改不会影响表；`Clone` 的后续写入对原表不可见，反之亦然。

## 复杂度

- `Apply`：O(n + m)，n 为现有条目数（候选复制与淘汰扫描），m 为批次内 op 数。
- `Expire`：O(n + k log k)，k 为被淘汰条目数（排序）。
- `Snapshot` / `Clone`：O(n log n) / O(n)。
- `Stats` / `ValidateBatch`：O(1) / O(m)。
- 空间：O(n)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
