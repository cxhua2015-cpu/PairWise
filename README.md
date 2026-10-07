# metacatalog351

并发安全的内存型元数据目录（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计说明

### 索引
- 主索引为 `map[string]Record`，按名称 O(1) 定位；另维护 `totalValue`（Value 总字节）、`generation` 与 `nextRevision` 计数器。
- `Get`/`Snapshot` 在 `sync.RWMutex` 读锁下执行；`Snapshot` 输出前按名称排序，排序只作用于拷贝出的切片，不维护额外有序结构。

### 候选事务（candidate transaction）
- `Apply` 分三阶段：
  1. **结构校验**：无锁状态下校验全部 op 的 kind、名称字符集/长度、Value 长度、Delete 不得携带 Value；任何错误直接返回 `ErrInvalidInput`，不触碰状态。
  2. **候选执行**：持写锁，在索引的克隆（candidate map）上按输入顺序执行 Put/Delete；Put 从候选 `nextRevision` 起连续分配 revision，Delete 不分配；Delete 缺失键返回 `ErrNotFound`。
  3. **批次末容量检查**：仅校验最终记录数与 Value 总字节，超限返回 `ErrCapacity`。
- 任一失败直接丢弃候选 map 与候选计数器——状态、generation、revision 天然全部回滚，无需补偿日志。成功时整体换入候选并一次性 `generation++`（空批次不加）。

### 所有权
- Put 的 Value 在提交前深拷贝；`Get`/`Snapshot`/`Result.Changed` 返回的 Value 均为新切片，调用方对返回值的修改不影响内部状态，反之亦然。返回切片与内部状态完全隔离。

### 复杂度
- `Apply`：校验 O(B)，候选克隆 O(N)，执行 O(B)，容量检查 O(1)，Changed 排序 O(B log B)；总体 O(N + B log B)，B 为批次大小、N 为记录数。
- `Get`：O(1)。`Snapshot`：O(N log N)（拷贝 + 排序）。
- 空间：O(N + 批次内 Value 字节)。

## 验证
- `go test ./...`
- `go test -race ./...`
- `go run ./cmd/demo`
