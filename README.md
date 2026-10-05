# resourceledger122

并发安全的内存型资源计量账本（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

- **索引**：账本主体为 `map[string]Account`，按名称 O(1) 定位账户；`Top`/`Snapshot` 在读取时拷贝并排序，不维护额外有序结构，保证写路径轻量。
- **候选事务**：`Apply` 先对整个批次做纯结构校验（kind、名称字符集与字节上限），再在账户表的拷贝（候选事务）上按输入顺序执行 Add/Set/Delete；任一步失败直接丢弃候选，实现整体回滚。int64 溢出在加法前检测，绝对值上限在每次赋值后立即检查，账户容量上限仅在批次末尾检查一次。
- **所有权**：所有返回的切片（`Result.Changed`、`Top`、`Snapshot.Accounts`）均为新分配的拷贝，调用方修改不影响内部状态；内部 `map` 仅在持写锁提交时整体替换。
- **并发**：单把 `sync.RWMutex` 保护全部状态；`Apply` 持写锁，`Top`/`Snapshot` 持读锁，可多读者并行。
- **复杂度**：结构校验 O(B·L)（B 为批大小，L 为名称长度）；候选拷贝 O(A)（A 为账户数）；批次执行 O(B)；`Top`/`Snapshot` 为 O(A log A)；`Top(n)` 截断至 n 条。revision 随 Add/Set 连续递增，非空成功批次 generation 恰好加一。
