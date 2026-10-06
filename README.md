# expirytable249

并发安全的内存型“到期状态表 249”，仅依赖 Go 标准库（Go 1.22+）。表使用显式非负单调逻辑时间，所有公开方法均可并发调用。

## 架构与多文件联动

- `heartbeat.go` — 核心事务引擎：`New` / `Apply` / `Expire` / `Snapshot`。
- `validation.go` — 无副作用批次预检：`ValidateBatch` 与 `Apply` 共享同一套结构语义（非负时间、已知 kind、键字符集与字节上限、Put/Touch 的 `ExpiresAt > Now`），预检不读取、不修改任何状态。
- `stats.go` — 线性一致统计：`Stats` 在与事务相同的读锁下取快照，始终反映历史中的单一时间点。
- `clone.go` — 深拷贝：`Clone` 复制全部条目与逻辑时钟（generation、nextRevision、now），与原表完全隔离。

## 索引

条目存储为 `map[string]Entry`，以键为唯一索引；Put/Touch/Delete 均为 O(1) 均摊定位。`Snapshot`/`Expire` 返回的切片按键排序以保证确定性输出。

## 候选事务

`Apply` 先做完整结构校验（同 `ValidateBatch`），再检查单调时间（`Now < now` 返回 `ErrTime`）。随后在候选副本上先淘汰 `ExpiresAt <= Now` 的条目，再顺序执行 Put/Touch/Delete；Put/Touch 各分配一个递增 revision。任何错误（`ErrNotFound`、`ErrCapacity` 等）发生时直接丢弃候选副本，淘汰、时间与 revision 随之整体回滚；只有全部成功才提交。非空成功批次 generation 恰好加一，空批次不变。`Expire` 使用相同闭区间边界（`ExpiresAt <= now`）并推进逻辑时钟。

## 所有权

`Snapshot`、`Expire` 返回的切片与 `Clone` 返回的表均为全新分配的内存，调用方修改不会影响表内部状态；克隆体与原表互不影响。

## 复杂度

- `Apply`：O(n + m)，n 为当前条目数（候选复制与淘汰扫描），m 为批内操作数。
- `Expire`：O(n + k log k)，k 为到期条目数（排序输出）。
- `Snapshot` / `Clone`：O(n log n) / O(n)。
- `Stats` / `ValidateBatch`：O(1) / O(m)。
- 空间：O(n)，单次 `Apply` 额外 O(n) 候选副本。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
