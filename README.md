# resourcecatalog196

并发安全的内存型资源目录（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

- **索引**：记录存储在 `map[string]Record` 哈希索引中，按名称 O(1) 定位；`Snapshot` 与 `Result.Changed` 在返回前按名称排序，内部不维护有序结构。
- **候选事务**：`Apply` 先对全部操作做完整结构校验（不读取状态），然后在克隆出的候选 map 上按输入顺序执行 Put/Delete；Put 从单调递增的 `nextRevision` 分配连续 revision，Delete 不分配。记录数与 Value 总字节容量只在批次末检查。任一环节失败即丢弃候选，状态、generation、revision 天然回滚；全部通过才一次性提交，非空成功批次 generation 只加一。
- **所有权**：Put 的 Value 在提交时深拷贝；`Get`/`Snapshot`/`Result.Changed` 返回的 Value 均为深拷贝副本，调用方对返回切片的修改不影响内部状态，反之亦然。
- **并发**：单把 `sync.RWMutex` 保护全部状态；`Apply` 持写锁，`Get`/`Snapshot` 持读锁，所有公开方法可并发调用（`-race` 验证通过）。
- **复杂度**：`Apply` 为 O(B + N)，B 为批内操作数、N 为当前记录数（克隆与容量求和）；`Get` 为 O(1)；`Snapshot` 为 O(N log N)（排序）。
