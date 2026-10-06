# expirytable244

并发安全的内存型“到期状态表”，仅依赖 Go 标准库（Go 1.22+）。详见 `SPEC.md`。

## 架构与多文件联动

实现刻意拆分为四个相互协作的组件，共享同一套语义：

- `heartbeat.go` — 核心事务引擎：`New` / `Apply` / `Expire` / `Snapshot`，以及表状态与锁。
- `validation.go` — 无副作用的批次预检：`ValidateBatch` 只做结构校验，不读取、不修改表状态；`Apply` 复用同一入口，保证两处结构语义完全一致。
- `stats.go` — 线性一致的状态统计：`Stats` 在读锁下一次性采样，反映并发事务序列中的某一确定时刻。
- `clone.go` — 所有权安全的深拷贝：`Clone` 复制全部逻辑时钟（now、generation、nextRevision）与全部条目，不共享任何切片或 map。

## 索引

表内部维护两个结构：插入有序的键列表 `order` 与哈希索引 `items map[string]Entry`。查找、Put/Touch/Delete 为 O(1) 均摊（Delete 的有序列表移除为 O(n)），快照与过期返回按插入顺序稳定输出。

## 候选事务

`Apply` 的流程：

1. 调用 `ValidateBatch` 做完整结构校验（键字符集与字节上限、kind 合法、Put/Touch 的 `ExpiresAt > Now`、`Now >= 0`），失败返回 `ErrInvalidInput`；
2. 检查单调时间，`Now < 当前时间` 返回 `ErrTime`；
3. 在候选副本上先删除 `ExpiresAt <= Now` 的条目（闭区间），再顺序执行 Put/Touch/Delete，Put/Touch 分配递增 revision；
4. 校验最终容量，超限返回 `ErrCapacity`。

任何一步出错都直接丢弃候选副本，因此淘汰、时间与 revision 一并回滚，原状态不受影响。非空成功批次 generation 只增加一次，空批次不变（但仍推进时间并执行淘汰）。`Expire` 使用相同的闭区间边界并推进单调时钟。

## 所有权

所有公开方法返回的切片（`Snapshot.Entries`、`Expire` 结果）都是新分配的副本，与内部状态隔离；`Clone` 产出的表与原表完全独立，互不影响。

## 并发与复杂度

- 单把 `sync.RWMutex`：写路径（`Apply`/`Expire`）持写锁，读路径（`Snapshot`/`Stats`/`Clone`）持读锁；`ValidateBatch` 只读配置，无需加锁。
- `Apply`：O(batch + n)，n 为当前条目数（候选拷贝与淘汰扫描）。
- `Expire`：O(n)；`Snapshot`/`Clone`：O(n)；`Stats`：O(1)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
