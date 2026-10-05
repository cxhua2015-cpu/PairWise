# resourcecatalog081

并发安全的内存型资源目录（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计说明

**索引**
- 主索引为 `map[string]Record`，按名称 O(1) 定位记录。
- 另维护 `totalBytes`（Value 总字节数）与单调递增的 `revision`、`generation` 计数器，避免每次容量检查时全表扫描。
- `Snapshot` 与 `Result.Changed` 按需收集名称后排序，不维护持久有序结构。

**候选事务**
- `Apply` 分两阶段：先对整个批次做纯结构校验（kind、名称字符集与长度、Value 长度、Delete 不得携带 Value），不读取任何状态。
- 随后在写锁内将主索引浅拷贝为候选 map，按输入顺序在候选上执行 Put/Delete：Put 分配连续 revision，Delete 不分配；记录数与 Value 总字节容量只在批次末检查。
- 任何失败（`ErrNotFound` / `ErrCapacity`）直接丢弃候选 map，revision、generation 与索引全部保持原状，天然回滚；成功则整体换入候选并提交计数器，非空批次 generation 只增一次。

**所有权**
- Put 时深拷贝 `Value` 存入；`Get`/`Snapshot`/`Result.Changed` 返回的 `Value` 均为独立副本，调用方对返回切片的修改不会影响内部状态，反之亦然。

**并发**
- 单把 `sync.RWMutex` 保护全部状态：`Apply` 持写锁，`Get`/`Snapshot` 持读锁，所有公开方法可安全并发调用。

**复杂度**
- `Apply`：O(n + m)，n 为批次数、m 为当前记录数（候选拷贝）；校验 O(n)，提交 O(m)。
- `Get`：O(1) 均摊（外加返回值拷贝 O(|Value|)）。
- `Snapshot`：O(m log m)，由按名称排序主导。
