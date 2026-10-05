# resourcecatalog166

并发安全的内存型资源目录（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

**索引**
- 核心索引为 `map[string]Record`，按名称 O(1) 定位记录；`MaxTotalValueBytes` 由单独维护的 `totalBytes` 计数器 O(1) 统计，避免每次遍历求和。
- `Snapshot` 与 `Result.Changed` 在返回前按名称排序（`sort.Slice`），保证输出确定性。

**候选事务（candidate transaction）**
- `Apply` 分三个阶段：
  1. **结构校验**：在加锁读取任何状态之前，完整校验整个批次（kind 合法、名称字符集与长度、Value 长度），失败返回 `ErrInvalidInput`。
  2. **候选执行**：在锁内把当前记录浅拷贝到候选 map，按输入顺序执行 Put/Delete；Put 递增共享 revision 计数器并深拷贝 Value，Delete 不分配 revision，缺失返回 `ErrNotFound`。
  3. **容量检查**：仅在批次末检查记录数与 Value 总字节，超限返回 `ErrCapacity`。
- 任一步失败直接丢弃候选 map 与局部 revision/total 计数器，已提交状态、generation、revision 完全不变（回滚无补偿成本）；成功则整体替换内部 map，非空批次 generation 恰好 +1。

**所有权**
- Put 时拷贝调用方 Value；`Get`/`Snapshot`/`Result.Changed` 返回的 Value 均为新分配的深拷贝，返回切片与内部状态完全隔离，调用方可自由修改。

**并发**
- 所有公开方法通过单个 `sync.Mutex` 串行化状态访问；结构校验在锁外完成，缩短临界区。`Get` 对非法名称返回 `ErrInvalidInput`，未找到返回 `ok=false, err=nil`。

**复杂度**
- `Apply`：O(n + m log m)，n 为批次操作数，m 为触及的不同名称数（排序）；候选拷贝 O(当前记录数)。
- `Get`：O(1)（外加 Value 拷贝）。`Snapshot`：O(r log r)，r 为记录数。
- 空间：O(记录数 + Value 总字节)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
