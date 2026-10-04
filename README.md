# casstore

并发安全的内存条件事务键值存储（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 索引与存储

- 主索引为 `map[string]Entry`，另维护 `valueBytes`（存活值总字节）、`nextRevision`、`generation` 三个计数器。
- 单把 `sync.RWMutex`：`Transact` 取写锁，`Get`/`List`/`Snapshot` 取读锁。
- 有序视图（`List`/`Snapshot`）在读取时收集匹配 key 并 `sort.Strings`，不维护额外的有序结构。

## 校验

- `New` 要求 `MaxKeys`/`MaxValueBytes`/`MaxNameBytes` 均为正，否则 `ErrInvalidOptions`。
- key：非空、≤ `MaxNameBytes`、仅含 `[A-Za-z0-9._/-]`；value：非 nil、≤ `MaxValueBytes`（空值合法）。
- `Transact` 先按顺序校验全部 compare，再按顺序校验全部 write，全部通过后才读取状态；任何结构错误返回 `ErrInvalidInput` 且不触碰状态。

## 比较与事务

- 比较在执行写锁内针对事务开始时的快照求值：Exists/NotExists 测存在性，Revision 精确匹配条目 revision，Value 按字节比较。
- 任一比较不成立：返回 `Succeeded:false`、当前 generation、无错误、无状态变化。
- 全部成立后，写入在隔离的候选 map（当前状态的拷贝）上按输入顺序执行：Put 创建/替换并分配当前 `nextRevision` 后递增；Delete 缺失键返回 `ErrNotFound`。
- 同批重复写、删除后重建均合法，后者获得新 revision。
- 所有写入完成后才检查最终容量（键数 ≤ `MaxKeys` 且存活值总字节 ≤ `MaxValueBytes`），超限返回 `ErrCapacity`。
- 任何失败整体回滚：候选状态被丢弃，不消耗 revision、不改变 generation。

## Revision 与 Generation

- `nextRevision` 从 1 开始，每次 Put 分配一个连续递增的 revision；`TxnResult.Revision` 为本事务最后一次 Put 分配的 revision（无 Put 时为 0）。
- 含至少一个写入的成功事务使 generation 恰好加 1；只读事务（无写入）不改变 generation。

## 容量

- 单值上限 `MaxValueBytes` 属结构校验（`ErrInvalidInput`）；最终键数与总存活字节属容量检查（`ErrCapacity`），仅在全部写入执行后判定。

## 所有权

- 输入 value 在 Put 时拷贝；`Get`/`List`/`Snapshot` 返回深拷贝，调用方与存储互不共享底层数组。

## 分页与快照

- `List(prefix, after, limit)`：prefix/after 可为空，非空须为合法 key；limit ∈ [1,1000]。返回 key 以 prefix 开头且字典序大于 after 的条目，按 key 升序，稳定分页。
- `Snapshot` 返回 generation、nextRevision、键数、值字节数及按 key 排序的深拷贝条目。

## 复杂度

- `Transact`：校验 O(C+W)，比较 O(C)，写入 O(W)，候选拷贝 O(N)（N 为当前键数）。
- `Get`：O(1)；`List`/`Snapshot`：O(N log N)（排序）+ O(N) 拷贝。
