# expirytable299

并发安全的内存型“到期状态表”，仅依赖 Go 标准库（Go 1.22+）。表使用显式非负单调时间，适用于分布式控制面的到期状态跟踪。

## 架构与索引

- **核心事务引擎**（`expirytable299/heartbeat.go`）：以 `map[string]Entry` 为主索引，键到条目 O(1) 定位；`Snapshot`/`Expire` 输出按 Key 排序，保证确定性。单把 `sync.RWMutex` 保护全部状态，写路径独占、读路径（`Snapshot`/`Stats`/`Clone`/`ValidateBatch`）共享。
- **无副作用预检**（`expirytable299/validation.go`）：`ValidateBatch` 只做结构校验（Now 非负、kind 合法、键字符集与字节上限、Put/Touch 的 `ExpiresAt > Now`），不读不改表状态；`Apply` 复用同一函数，保证预检与事务结构语义完全一致。
- **线性一致统计**（`expirytable299/stats.go`）：`Stats` 在读锁内一次性采样 generation、nextRevision、now 与条目数，与并发事务保持线性一致。
- **所有权安全克隆**（`expirytable299/clone.go`）：`Clone` 复制逻辑时钟（now/generation/nextRevision）并逐条深拷贝条目，克隆体与原表零共享，互不影响。

## 候选事务与回滚

`Apply` 的顺序：结构校验 → 单调时间检查（`Now < now` 返回 `ErrTime`）→ 在候选副本上先淘汰 `ExpiresAt <= Now` 的条目 → 顺序执行 Put/Touch/Delete（Put/Touch 分配递增 revision，Touch/Delete 缺失返回 `ErrNotFound`）→ 最终容量检查。任何错误（含 `ErrCapacity`）直接丢弃候选，淘汰、时间与 revision 一并回滚。非空成功批次 generation 只增一次，空批次不变（但仍推进时间）。`Expire` 使用相同闭区间边界 `ExpiresAt <= now`。

## 所有权

所有返回的切片（`Snapshot.Entries`、`Expire` 结果）均为新分配的拷贝，与内部状态隔离；`Clone` 深拷贝全部条目，调用方无法通过返回值修改表。

## 复杂度

- `ValidateBatch`：O(批次大小)，无状态访问。
- `Apply`：O(n + 批次大小)，n 为当前条目数（候选复制与淘汰扫描）。
- `Expire`：O(n + k log k)，k 为到期条目数（排序输出）。
- `Snapshot`/`Clone`：O(n)；`Stats`：O(1)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
