# resourcecatalog166

并发安全的内存型资源目录（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

**索引**
- 主索引为 `map[string]Record`，按名称 O(1) 定位；另维护 `totalValues` 运行计数，
  使批次末的总字节容量检查为 O(1)，无需遍历。
- `Snapshot` 与 `Result.Changed` 在读取时按名称排序输出，不为排序维护额外结构。

**候选事务（candidate transaction）**
- `Apply` 先做整批结构校验（不读任何状态），再在写锁内把当前 map 克隆为候选状态，
  按输入顺序在其上执行 Put/Delete：Put 分配连续 revision，Delete 不分配。
- 记录数与 Value 总字节容量只在批次末检查；任何失败直接丢弃候选 map，
  generation、nextRevision 与记录集天然回滚，无需反向补偿。
- 成功时一次性换入候选 map；非空批次 generation 只加一，空批次不变。

**所有权**
- Put 时深拷贝 `Value`；`Get`/`Snapshot`/`Result.Changed` 返回的 Value 均为新切片，
  调用方对返回切片的修改不影响内部状态，反之亦然。

**并发**
- 单把 `sync.RWMutex`：`Apply` 取写锁，`Get`/`Snapshot` 取读锁。
  所有公开方法可安全并发调用，已通过 `go test -race` 验证。

**复杂度**
- 结构校验 O(Σ(名字长度))；执行 O(批次数 + 记录数)（克隆 map）；
  `Get` O(1)；`Snapshot` O(n log n)（排序）；空间 O(记录数 + 总字节数)。
