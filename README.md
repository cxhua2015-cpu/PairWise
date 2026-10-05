# resourceledger092

并发安全的内存型资源计量账本。原子批次按输入顺序执行 Add/Set/Delete，
Add/Set 分配连续 revision，失败整体回滚。详见 `SPEC.md`。

## 设计说明

- **索引**：账户存储为 `map[string]Account`，按名称 O(1) 定位；
  `Top` 与 `Snapshot` 在读取时物化切片并排序，不维护额外的有序索引，
  以避免写路径上的额外开销与并发复杂度。
- **候选事务**：`Apply` 先做完整结构校验（kind、名称字符集与字节上限），
  再在写锁内把当前 map 浅拷贝为候选状态，所有 Add/Set/Delete 只作用于候选；
  int64 溢出在算术前检测，绝对值上限逐操作执行，账户容量仅在批次末对
  候选检查。任何失败直接丢弃候选，已提交状态零改动；全部成功才整体换入，
  generation 恰好加一，revision 连续推进。
- **所有权**：`Top`、`Snapshot`、`Result.Changed` 返回的切片均为每次调用
  新建，账户为值类型，调用方修改返回值不影响账本内部状态。
- **并发**：单个 `sync.RWMutex` 保护全部状态；`Apply` 取写锁，
  `Top`/`Snapshot` 取读锁，可多读者并行。
- **复杂度**：批次结构校验 O(ops)；候选拷贝 O(n)（n 为账户数）；
  每个 op O(1)；容量检查 O(1)。`Top` 与 `Snapshot` 为 O(n log n) 排序。
