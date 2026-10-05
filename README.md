# resourcecatalog106

并发安全的内存型资源目录（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

- **索引**：记录存放在 `map[string]Record` 哈希索引中，按名称 O(1) 定位；另维护
  `totalValue`（Value 总字节）、`generation`、`nextRevision` 三个计数器，避免批次末
  为容量检查而全表求和。`Snapshot`/`Changed` 在取出后按名称排序（O(n log n)）。
- **候选事务**：`Apply` 分两阶段。第一阶段对全部 Op 做完整结构校验（kind、名称字符集
  与长度、Value 长度），不读取任何状态；第二阶段把当前记录复制到候选 map，按输入顺序
  执行 Put/Delete——Put 分配连续 revision，Delete 不分配且要求目标存在。记录数与
  Value 总字节容量只在批次末对候选结果检查。任一阶段失败直接丢弃候选，状态、
  generation、revision 全部不变（天然回滚）；成功时整体换入候选，非空批次
  generation 恰好加一。
- **所有权**：Put 的 Value 在提交前深拷贝；`Get`/`Snapshot`/`Result.Changed` 返回的
  Value 与切片均为新分配的副本，调用方后续修改不影响内部状态，内部状态变化也不影响
  已返回的结果。
- **并发**：单把 `sync.RWMutex` 保护全部状态；`Apply` 持写锁，`Get`/`Snapshot` 持读锁，
  所有公开方法可安全并发调用（`-race` 验证通过）。
- **复杂度**：结构校验 O(Σ op 长度)；候选应用 O(n + m)，n 为现有记录数（复制候选
  map）、m 为批内 op 数；容量检查 O(1)；`Get` O(1)；`Snapshot` O(n log n)。
