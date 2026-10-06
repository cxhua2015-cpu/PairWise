# expirytable279

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.

## Design notes

- **索引**：条目存储在 `map[string]Entry` 哈希索引中，按键 O(1) 定位；`Snapshot`/`Expire` 返回的切片按字典序排序以保证确定性，且与内部状态完全隔离。
- **候选事务**：`Apply` 先用 `ValidateBatch` 做无副作用的整批结构校验（不读取状态），再检查单调时间；随后在候选副本上先删除 `ExpiresAt <= Now` 的条目（闭区间），再顺序执行 Put/Touch/Delete 并分配 revision。最终容量超限或任何错误都会丢弃候选，淘汰、时间与 revision 一并回滚；只有全部成功才提交，非空批次 generation 恰好加一。
- **所有权**：`Table` 内所有可变状态由单个 `sync.Mutex` 保护，所有公开方法并发安全且线性一致。`Clone` 在深拷贝逻辑时钟（now/generation/nextRevision）的同时复制全部条目，克隆体与原表互不影响；`Snapshot`/`Stats`/`Expire` 返回的数据均为独立副本。
- **复杂度**：Put/Touch/Delete 单操作 O(1)；`Apply` 为 O(n + m)（n 为现存条目数、 m 为批内操作数，候选复制与淘汰扫描）；`Expire` 与 `Snapshot` 为 O(n log n)（含排序）；`Stats` 为 O(1)；`Clone` 为 O(n)。

## 文件分工

- `expirytable279/heartbeat.go`：核心类型、构造函数、事务引擎（Apply/Expire/Snapshot）。
- `expirytable279/validation.go`：批次结构预检与键合法性，与 Apply 共享同一套语义。
- `expirytable279/stats.go`：线性一致的状态统计。
- `expirytable279/clone.go`：保留逻辑时钟、所有权完全隔离的深拷贝。
