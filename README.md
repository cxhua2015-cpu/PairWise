# metacatalog336

并发安全的内存型元数据目录（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

- **索引**：`Store` 以 `map[string]Record` 作为主索引，键为记录名，直接支撑 O(1) 的
  Get 与批次内定位；`Snapshot`/`Result.Changed` 按需对键排序，不维护冗余有序结构。
- **候选事务**：`Apply` 先在当前记录的副本（候选 map）上按输入顺序执行 Put/Delete，
  任一操作失败（未知名称 Delete、末态容量超限）即整体丢弃候选，原状态、generation
  与 revision 计数器保持不变，天然实现回滚；全部通过后一次性换入并提交计数器。
- **所有权**：Put 的 Value 在写入时深拷贝，Get/Snapshot/Result 返回的 Value 均为新
  分配的副本，调用方与内部状态完全隔离，互不因对方修改而受影响。
- **并发**：单把 `sync.RWMutex` 保护全部状态；写事务持写锁，Get/Snapshot 持读锁，
  结构校验在加锁前完成，不持有锁期间不做任何分配以外的阻塞操作。
- **复杂度**：结构校验 O(批次大小)；候选事务 O(现有记录数 + 批次大小)（复制索引）；
  末态容量检查 O(记录数)；Get O(1) 均摊；Snapshot O(n log n)（排序）。
