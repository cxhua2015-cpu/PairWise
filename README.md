# metacatalog371

并发安全的内存型元数据目录（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

**索引**：`Store` 内部使用 `map[string]entry` 作为主索引，键为记录名，`entry` 持有
深拷贝后的 Value 与 Revision。单把 `sync.Mutex` 保护全部状态（索引、generation、
nextRevision），所有公开方法（`Apply`/`Get`/`Snapshot`）在锁内完成，因此并发调用安全。
名称有序的需求（`Snapshot.Records`、`Result.Changed`）在读取时排序，不为索引维护额外
有序结构。

**候选事务**：`Apply` 分三个阶段。第一阶段对全部 Op 做完整结构校验（kind、名称字符集
与长度、Value 长度），不读取任何状态；第二阶段在索引的克隆（候选事务）上按输入顺序
执行 Put/Delete——Put 分配连续 revision，Delete 不分配，Delete 缺失记录返回
`ErrNotFound`；第三阶段仅对候选最终状态检查记录数与 Value 总字节容量（`ErrCapacity`）。
任一阶段失败直接丢弃候选，已提交状态、generation 与 revision 完全不变；成功时整体
替换索引，generation 只加一次（空批次不加）。

**所有权**：所有进入 Store 的 Value（`Apply` 的 Put）都会被复制；所有离开 Store 的
Value（`Result.Changed`、`Get`、`Snapshot`）同样是新分配的副本。调用方修改输入或返回
的切片不会影响目录内部状态，返回的切片之间也互不影响。

**复杂度**：设批次含 B 个 Op、当前记录数 N、名称总长 M。
`Apply` 为 O(N + B + C log C)（克隆索引 O(N)，执行 O(B)，对 C 个变更名排序）；
`Get` 为 O(1) 均摊（外加返回值复制）；`Snapshot` 为 O(N log N)（排序）加全量深拷贝
O(M)。结构校验为 O(B) 且先于任何状态读取。
