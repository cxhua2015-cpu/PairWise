# balanceledger382

并发安全的内存型余额账本（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

### 索引
- 主索引为 `map[string]Account`，按账户名 O(1) 定位。
- 不维护有序索引：`Top` 与 `Snapshot` 在调用时对当前账户快照按需排序
  （分别为值降序/名称升序、名称升序），以换取写入路径 O(1) 与实现的简单性。

### 候选事务（candidate transaction）
- `Apply` 分三个阶段：先对全部 Op 做纯结构校验（不读状态），再在账户表的
  候选副本上按输入顺序执行 Add/Set/Delete 并分配连续 revision，最后在批次末
  检查账户容量。
- 任一步失败直接丢弃候选副本，内部状态、generation、revision 完全不变，
  从而实现整体回滚；成功时一次性提交副本，generation 只增加一次。

### 所有权与并发
- 所有公开方法由单把 `sync.Mutex` 保护，可安全并发调用；`Apply` 的
  校验—执行—提交整体串行化，保证原子性。
- 账本独占内部 map；`Result.Changed`、`Top`、`Snapshot` 返回的切片均为新建
  副本，调用方修改不会影响内部状态，也不会观察到中途状态。

### 复杂度（n = 账户数，m = 批次内 Op 数）
- `Apply`：时间 O(n + m)（克隆候选副本 + 顺序执行），空间 O(n)。
- `Top(k)`：时间 O(n log n)，空间 O(n)。
- `Snapshot`：时间 O(n log n)，空间 O(n)。
