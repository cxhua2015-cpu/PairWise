# expirytable289

并发安全的内存型“到期状态表”，仅依赖 Go 标准库（Go 1.22+）。表使用显式非负单调时间，所有公开方法均可并发调用。

## 架构与多文件联动

- `heartbeat.go` — 核心类型与事务引擎：`New` / `Apply` / `Expire` / `Snapshot`。
- `validation.go` — 无副作用批次预检：`ValidateBatch` 与 `Apply` 共享同一套结构校验（`validateBatchLocked`），先结构校验再检查时间，不读写任何表状态。
- `stats.go` — `Stats` 在同一互斥锁下读取，返回线性一致的状态摘要。
- `clone.go` — `Clone` 深拷贝全部字段（含 `now`、`generation`、`nextRevision` 逻辑时钟），切片与映射全部新建，与原表完全隔离所有权。

## 索引

条目以插入序保存在 `order []string` 中，`index map[string]Entry` 提供 O(1) 键查找。`Snapshot` 按插入序返回条目副本，返回值与内部状态隔离。

## 候选事务

`Apply` 先在候选状态（`order`/`index` 的副本）上执行：删除 `ExpiresAt <= Now` 的条目（闭区间），再顺序执行 Put/Touch/Delete，Put/Touch 分配递增 revision。最终容量超限或任何错误（`ErrNotFound`、`ErrCapacity` 等）都会整体回滚——淘汰、时间与 revision 均不落盘；只有全部成功才一次性提交，非空成功批次 `generation` 恰好加一，空批次不变（时间仍前进）。`Expire` 使用相同的闭区间边界并推进时间。

## 所有权

`Snapshot`、`Expire` 返回的切片均为新建副本；`Clone` 不共享任何底层数组或映射，克隆体的后续变更不会影响原表，反之亦然。

## 复杂度

设 n 为条目数、m 为批次操作数：

- `Apply`：O(n + m)（候选复制加顺序执行）；`ValidateBatch`：O(m)。
- `Expire`：O(n)；`Snapshot` / `Clone`：O(n)；`Stats`：O(1)。
- 键查找 O(1)，删除 O(n)（维护插入序）。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
