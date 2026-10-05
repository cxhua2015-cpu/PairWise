# leasepool

并发安全、显式时间驱动的内存租约池（Go 1.22+，仅标准库）。公开契约见 `SPEC.md`。

## 设计

### 索引
- `resources map[string]struct{}`：资源名集合，O(1) 存在性判断。
- `leases map[string]Lease`：以资源名为键的租约表；一个资源至多一条租约。
- 两个 map 即全部状态；排序（Snapshot/Expire/Changed）在读取时按需进行，不维护额外有序结构。

### 候选事务（Apply）
1. **结构校验**：先校验整个批次（`Now >= 0`、Kind 合法、字段组合、名称字符集与长度），不读任何状态。
2. **时间单调性**：校验通过后才检查 `Batch.Now >= 当前时间`，否则 `ErrTime`。
3. **隔离执行**：将 resources/leases 复制为候选状态，按输入顺序逐条执行；每个成功操作分配一个连续 revision（从 1 开始），Acquire/Renew 把 revision 写入租约。
4. **容量检查**：最终资源数仅在批次末尾检查，超过 `MaxResources` 返回 `ErrCapacity`。
5. **提交或回滚**：任一步失败直接丢弃候选状态，时间、generation、revision 均不前进；成功时用候选状态整体替换，非空批次 generation 加一并推进时间，空批次仅推进时间。

### 过期规则
- 租约 `ExpiresAt <= Now` 即视为过期。过期租约仍占据资源：Remove 报 `ErrBusy`，Renew/Release 报 `ErrNotFound`。
- Acquire 把过期租约视为空闲并替换之；但替换只发生在候选状态中，批次失败则旧租约保留。
- `Expire(now)` 物理移除 `ExpiresAt <= now` 的租约，按资源名排序返回；仅当确实移除租约时 generation 加一，不分配 revision。

### revision / generation
- `revision`：单调递增的操作计数器，每个成功执行的 Op 消耗一个；`Snapshot.NextRevision = 已用最大值 + 1`。
- `generation`：状态代际，仅在非空批次成功提交或 Expire 真正移除租约时加一。
- 任何失败路径两者都保持不变。

### 并发与隔离
- 所有公开方法由一把 `sync.Mutex` 保护，可安全并发调用。
- 所有返回的切片（`Result.Changed`、`Expire`、`Snapshot.Resources/Leases`）均为新建副本，不别名内部存储。

### 复杂度
- 单条 Op：O(1) 均摊（map 操作）。批次为 O(Ops)。
- 候选复制：每批次 O(R + L) 时间与空间（R=资源数，L=租约数）。
- `Snapshot`/`Expire`/`Changed` 排序：O(n log n)，n 为相应集合大小。
- 空间总量：O(R + L)。
