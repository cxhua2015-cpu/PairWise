# metacatalog216

并发安全的内存型元数据目录（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

**索引**：`Store` 使用 `map[string]entry` 作为主索引，键为记录名，`entry` 保存深拷贝后的 Value 与 Revision；另维护 `totalValue`（Value 总字节）、`generation` 与 `nextRevision` 计数器。记录数即 `len(map)`，容量检查为 O(1)。

**候选事务**：`Apply` 分三个阶段。第一阶段对所有 Op 做完整结构校验（kind、名称字符集与长度、Value 长度），不读取任何状态；第二阶段在候选副本（`map` 的浅拷贝 + 写时复制 Value）上按输入顺序执行 Put/Delete，Put 分配连续 revision，Delete 不分配，Delete 缺失键即返回 `ErrNotFound`；第三阶段才检查最终记录数与 Value 总字节容量。任一阶段失败直接返回，原始状态、generation、revision 完全不受影响——回滚通过“不提交候选副本”天然实现。成功时整体替换 map，generation 恰好加一。

**所有权**：Put 的 Value 在提交前深拷贝；`Get`/`Snapshot` 返回的 Record（含 Value 切片）与 `Result.Changed` 均为新分配的副本，调用方对返回值的任何修改都不会影响内部状态，反之亦然。`Snapshot.Records` 与 `Result.Changed` 均按名称排序；`Changed` 对同批次内重复触碰的名称去重，仅保留批末仍存在的最终记录。

**并发**：单把 `sync.RWMutex` 保护全部状态。`Apply` 持写锁，`Get`/`Snapshot` 持读锁可并行执行。

**复杂度**：`Apply` 为 O(B + N)，B 为批内 Op 数、N 为当前记录数（候选副本克隆），加上 Changed 排序 O(B log B)；`Get` 为 O(1)（另加 Value 拷贝）；`Snapshot` 为 O(N log N)。空间 O(N)。
