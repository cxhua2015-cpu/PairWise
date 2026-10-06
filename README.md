# expirytable294

并发安全的内存型“到期状态表”，使用显式非负单调时间，仅依赖标准库（Go 1.22+）。

## 架构

实现按职责拆分为四个联动文件，共享同一套结构语义：

- `heartbeat.go` — 核心事务引擎：`New`/`Apply`/`Expire`/`Snapshot`，持有互斥锁与内部状态。
- `validation.go` — 无副作用批次预检：`ValidateBatch` 只做结构校验（时间非负、kind 合法、键字符集与长度上限、Put/Touch 的 `ExpiresAt > Now`），不读取也不修改表状态；`Apply` 在检查时间之前复用同一校验。
- `stats.go` — 线性一致统计：`Stats` 在同一把锁内读取，返回与并发事务一致的摘要。
- `clone.go` — 所有权隔离的深拷贝：`Clone` 复制全部条目与逻辑时钟（generation、nextRevision、now），克隆体与原表完全独立。

## 索引与所有权

- 主索引为 `map[string]Entry`，按键 O(1) 定位；`Snapshot`/`Expire` 返回的切片均按键排序且为新建副本，与内部状态隔离。
- 所有公开方法通过单个 `sync.Mutex` 串行化，返回值不共享内部内存，调用方无需额外同步。

## 候选事务与回滚

`Apply` 先在候选副本上执行：淘汰 `ExpiresAt <= Now` 的条目，再顺序执行 Put/Touch/Delete（Put/Touch 分配递增 revision）。最终容量超限或任何错误（`ErrNotFound`、`ErrCapacity`、`ErrTime`）都会整体回滚——淘汰、时间与 revision 均不生效。非空成功批次 generation 只增加一次，空批次不变。`Expire` 使用相同闭区间边界 `ExpiresAt <= now`。

## 复杂度

- `ValidateBatch`：O(批次大小)，无状态访问。
- `Apply`：O(n + 批次大小)，n 为当前条目数（候选复制与淘汰扫描）。
- `Expire`：O(n)；`Snapshot`/`Clone`：O(n)；`Stats`：O(1)。
