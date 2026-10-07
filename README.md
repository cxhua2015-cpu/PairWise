# balanceledger387

并发安全的内存型余额账本（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计说明

- **索引**：账本主体是 `map[string]Account`（按名称 O(1) 定位）。`Top`/`Snapshot`
  在读取时全量拷贝并排序，不维护额外有序索引，以保证写路径 O(1) 且实现简单。
- **候选事务**：`Apply` 先做整批结构校验（kind、名称字符集与字节上限），再在写锁内
  把当前账户表浅拷贝为候选 map，按输入顺序在其上执行 Add/Set/Delete。任何一步失败
  （溢出/绝对值上限 `ErrValue`、Delete 缺失 `ErrNotFound`、批次末容量 `ErrCapacity`）
  直接丢弃候选，实现整体回滚；全部成功才一次性替换内部表，非空成功批次 generation 加一。
- **溢出与上限**：Add 在算术前用 `math.MaxInt64/MinInt64` 边界比较检测 int64 溢出，
  再对结果执行 `MaxAbsValue` 绝对值上限；Set 直接校验目标值。容量仅在批次末检查，
  因此批内"先删后建"可以成功。
- **Revision**：Add/Set 各分配一个连续递增的 revision（Delete 不消耗），从 1 开始；
  `Snapshot.NextRevision` 为下一个待分配值。
- **所有权**：返回的 `Result.Changed`、`Top`、`Snapshot.Accounts` 均为拷贝，调用方修改
  不影响内部状态；输入 `Batch` 不被保留。
- **并发**：单把 `sync.RWMutex`；写（Apply）持写锁，读（Top/Snapshot）持读锁，
  所有公开方法可并发调用。
- **复杂度**：Apply 为 O(k·a)（k 个操作，候选拷贝 a 个账户；Changed 去重为均摊 O(k)），
  Top/Snapshot 为 O(a log a)，空间 O(a + k)。
