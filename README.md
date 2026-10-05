# resourcecatalog176

并发安全的内存型资源目录（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计说明

**索引**：`Store` 以 `map[string]Record` 作为主索引，按名称 O(1) 定位记录；
`generation` 与 `nextRevision` 为单调计数器。Snapshot/Changed 的顺序在返回前
按名称排序生成，不维护额外有序结构。

**候选事务**：`Apply` 分三个阶段——
1. 完整结构校验（kind、名称字符集与长度、Value 长度），不做任何状态读取；
2. 在记录的浅拷贝候选 map 上按输入顺序模拟 Put/Delete：Put 分配连续 revision，
   Delete 要求目标存在（否则 `ErrNotFound`）；
3. 仅在批次末检查最终记录数与 Value 总字节容量（`ErrCapacity`）。
任一阶段失败直接返回，候选被丢弃，正式状态、generation、revision 全部不变；
成功时整体换入候选 map，非空批次 generation 恰好加一。

**所有权**：Put 时深拷贝 Value 存入；Get/Snapshot/Result.Changed 返回的记录均
深拷贝 Value，调用方对返回切片的任何修改不影响内部状态，反之亦然。

**并发**：所有公开方法共用一把 `sync.Mutex`，Apply 的校验—模拟—提交全程持锁，
因此批次之间是线性化的；Get/Snapshot 同样持锁，读到的是已提交的一致快照。

**复杂度**（n = 批次数 op 数，m = 当前记录数，k = 触及名称数）：
- `Apply`：时间 O(n + m + k log k)，空间 O(m)（候选 map 为记录的浅拷贝，
  Value 仅对 Put 的输入做深拷贝）；
- `Get`：O(1) 均摊（外加返回值拷贝）；
- `Snapshot`：O(m log m)。
