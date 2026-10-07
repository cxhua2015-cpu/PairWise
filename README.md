# metacatalog406

并发安全的内存型元数据目录（Go 1.22+，仅标准库）。原子批次按输入顺序执行
Put/Delete：Put 分配连续 revision，Delete 不分配；完整结构校验先于状态读取；
记录数与 Value 总字节容量只在批次末检查；失败回滚全部状态、generation 与
revision；Get/Snapshot 深拷贝 Value，Snapshot 按名称排序。

## 多文件架构

- `servicecatalog.go` — 核心事务引擎（`New`/`Apply`/`Get`/`Snapshot`）。
- `validation.go` — 无副作用的批次预检（`ValidateBatch`），与 `Apply` 共享同一套
  结构语义：kind 合法、名称字符集与长度上限、Put 值长度上限、Delete 不得携带值。
- `stats.go` — 线性一致的状态统计（`Stats`），在读锁内汇总，与并发事务保持一致。
- `clone.go` — 保留逻辑时钟（generation/nextRevision）且所有权完全隔离的深拷贝。

## 索引

`Store` 以 `map[string]entry` 作为主索引，键为记录名，值为 `{value, revision}`。
单把 `sync.RWMutex` 保护全部状态：写事务（`Apply`）持写锁，`Get`/`Snapshot`/
`Stats`/`Clone` 持读锁，因此所有公开方法均可并发调用且观察到的状态是线性一致的。

## 候选事务

`Apply` 先在锁外做完整结构预检，再在写锁内把当前索引复制为候选 map，按输入顺序
在候选上重放所有 Op（Put 从 `nextRevision` 起连续分配 revision，Delete 校验存在性），
最后才对候选整体检查 `MaxRecords` 与 `MaxTotalValueBytes`。任何一步失败直接丢弃候选，
原状态、generation 与 revision 天然不变，无需显式回滚日志；成功时一次性换入候选，
非空批次 generation 恰好加一，空批次不产生任何变化。

## 所有权

所有进出边界的 `[]byte` 都经过拷贝：Put 写入时复制调用方的 Value；`Get`、
`Snapshot`、`Result.Changed` 与 `Clone` 返回的值均为独立副本，返回切片与内部状态
完全隔离，调用方后续修改不会影响目录，反之亦然。

## 复杂度

设 n 为当前记录数、k 为批次内 Op 数：

- `Apply`：O(n + k) 时间与 O(n) 额外空间（候选复制 + 重放 + 结果按名排序 O(k log k)）。
- `Get`：O(1) 均摊。`Snapshot`/`Clone`：O(n log n)（排序）/ O(n)。
- `Stats`：O(n)。`ValidateBatch`：O(k)，不读取任何状态。
