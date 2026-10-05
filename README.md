# resourcecatalog176

并发安全的内存型“资源目录 176”，仅依赖 Go 标准库（Go 1.22+）。语义见 `SPEC.md`。

## 设计说明

**索引**：`Store` 以 `map[string]record` 作为主索引，键为资源名称，值为深拷贝后的
`Value` 与 `Revision`。`Get`/`Snapshot` 为 O(1) 查询与全量枚举；`Snapshot` 在读取时
收集键并排序，不维护额外的有序结构，写入路径保持 O(1) 摊销。

**候选事务**：`Apply` 分三个阶段——
1. 完整结构校验（kind、名称字符集与长度、Value 长度、Delete 不得携带 Value），
   此阶段不读取任何状态，因此非法输入优先于 `ErrNotFound` 返回；
2. 在候选副本（原索引的浅拷贝，Value 按需重新分配）上按输入顺序执行 Put/Delete，
   Put 从候选 revision 计数器连续分配，Delete 不分配；
3. 仅在批次末检查记录数与 Value 总字节容量。

任一阶段失败即丢弃候选，`records`、`generation`、`nextRevision` 全部不变，实现天然回滚；
成功时整体换入候选，非空批次 `generation` 恰好 +1，空批次不变。

**所有权**：Put 的 `Value` 在提交前深拷贝，调用方之后修改入参切片不影响存储；
`Get`/`Snapshot`/`Result.Changed` 返回的 `Value` 与记录切片均为新分配的副本，
调用方修改返回值不会污染内部状态，内部状态也不会逃逸到返回值。

**并发**：`sync.RWMutex` 保护全部状态；`Apply` 持写锁，`Get`/`Snapshot` 持读锁，
所有公开方法可安全并发调用。

**复杂度**：`Apply` 为 O(n + m)，n 为批次操作数、m 为当前记录数（候选拷贝与容量求和）；
`Get` 为 O(1)；`Snapshot` 为 O(m log m)（排序）。
