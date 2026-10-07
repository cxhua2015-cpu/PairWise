# metacatalog376

并发安全的内存型元数据目录（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

**索引**：`Store` 以 `map[string]Record` 作为主索引，按名称 O(1) 定位记录；另维护 `totalVal`（Value 总字节）、`gen`（generation）与 `nextRev`（下一个 revision）三个计数器，避免遍历求值。`Snapshot` 与 `Result.Changed` 在返回前按名称排序。

**候选事务**：`Apply` 先对整个批次做完整结构校验（kind、名称字符集与长度、Value 长度、Delete 不得携带 Value），不读取任何状态；随后在写锁内把当前记录复制到候选 map，按输入顺序执行 Put/Delete——Put 分配连续 revision，Delete 不分配且要求记录存在（否则 `ErrNotFound`）。记录数与 Value 总字节容量只在批次末检查，超限返回 `ErrCapacity`。任一步失败直接丢弃候选 map，状态、generation、revision 全部自然回滚；只有全部成功才一次性提交并令 generation 增一（空批次不增）。

**所有权**：Put 的 Value 在入库前深拷贝；`Get`/`Snapshot`/`Result.Changed` 返回的 Value 均为独立副本，调用方对返回切片的修改不影响内部状态，反之亦然。

**并发**：单个 `sync.RWMutex` 保护全部状态——`Apply` 持写锁，`Get`/`Snapshot` 持读锁，所有公开方法可安全并发调用。

**复杂度**：结构校验 O(批次总字节)；`Apply` 为 O(R + B·V + C log C)，其中 R 为现有记录数（候选复制）、B 为操作数、V 为平均 Value 长度、C 为触及的不同名称数（排序）；`Get` O(1)（外加一次 Value 拷贝）；`Snapshot` O(R·V + R log R)。
