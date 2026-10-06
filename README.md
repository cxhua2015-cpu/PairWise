# expirytable274

并发安全的内存型“到期状态表 274”，Go 1.22+，仅依赖标准库。语义详见 `SPEC.md`。

## 架构

实现按职责拆分为四个联动文件，共享同一套结构语义与逻辑时钟：

- `heartbeat.go` — 核心事务引擎：`New` / `Apply` / `Expire` / `Snapshot`，以及类型与错误值。
- `validation.go` — 无副作用的批次结构预检 `ValidateBatch`，`Apply` 在读取任何状态之前调用同一函数。
- `stats.go` — 线性一致的状态摘要 `Stats`（在读锁下一次性采集）。
- `clone.go` — 保留逻辑时钟（now / generation / nextRevision）的深拷贝 `Clone`。

## 索引

条目存储为 `map[string]Entry`，按键 O(1) 定位；`Snapshot`/`Expire` 返回的切片按键排序，且为独立副本，与内部状态完全隔离。

## 候选事务

`Apply` 先在候选副本（`map` 的浅拷贝）上执行：淘汰 `ExpiresAt <= Now` 的条目（闭区间），再顺序执行 Put/Touch/Delete，Put/Touch 各分配一个 revision。任何错误（结构、时间回退、未找到、最终容量超限）都会丢弃候选，连同淘汰、时间与 revision 一起回滚；只有全部成功才一次性提交，非空成功批次 generation 恰好加一，空批次不改变任何状态。

## 所有权

所有公开方法通过一把 `sync.RWMutex` 保护，可并发调用；写路径独占，读路径（`Snapshot`/`Stats`/`Clone`）共享。`Clone` 逐条复制条目，克隆体与原表互不影响；返回值不暴露内部 map 或切片。

## 复杂度

- `ValidateBatch`：O(批次大小)，不读状态。
- `Apply`：O(n + 批次大小)，n 为当前条目数（候选拷贝与淘汰扫描）。
- `Expire`：O(n + k log k)，k 为到期条目数（排序）。
- `Snapshot` / `Clone`：O(n log n) / O(n)。
- `Stats`：O(1)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
