# resourcecatalog096

并发安全的内存型资源目录（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

**索引**
- 主索引为 `map[string]Record`，按名称 O(1) 定位；名称即键，无次级索引。
- `Store` 另维护单调递增的 `generation`（每个非空成功批次 +1）与 `nextRevision`（仅 Put 消耗，从 1 开始连续分配）。
- `Snapshot`/`Result.Changed` 在返回前按名称排序，排序在拷贝出的切片上进行，不影响索引。

**候选事务（candidate transaction）**
- `Apply` 分三阶段：
  1. 完整结构校验（kind 合法、名称字符集与长度、Value 长度上限），此阶段不读取任何状态；
  2. 在克隆出的候选 map 上按输入顺序执行 Put/Delete，Put 分配连续 revision，Delete 要求目标存在（否则 `ErrNotFound`）；
  3. 仅在批次末检查记录数与 Value 总字节容量（`ErrCapacity`）。
- 任何失败直接丢弃候选 map，`generation`/`nextRevision` 不落盘，回滚零成本；成功则整体替换索引并各推进一次计数器。

**所有权**
- Put 的 Value 在写入前深拷贝，调用方之后修改入参不影响目录。
- `Get`/`Snapshot`/`Result.Changed` 返回的 Value 均为新分配的副本，返回切片与内部状态完全隔离。

**并发**
- 所有公开方法共用一把 `sync.RWMutex`：`Apply` 取写锁，`Get`/`Snapshot` 取读锁，读写可并行、写互斥。

**复杂度**（n = 当前记录数，k = 批次内 op 数，m = 触及的不同名称数）
- `Apply`：时间 O(n + k + m log m)（克隆索引 + 顺序执行 + Changed 排序），空间 O(n)。
- `Get`：O(1) 加一次 Value 拷贝。
- `Snapshot`：O(n log n)（排序），空间 O(n)。
