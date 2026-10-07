# metacatalog351

并发安全的内存型元数据目录（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

**索引**
- 主索引为 `map[string]Record`，按名称 O(1) 定位记录。
- 另维护 `totalBytes` 计数器，随批次增量更新，使 Value 总字节容量检查为 O(1)，无需遍历。
- `Snapshot` 与 `Result.Changed` 在返回前对名称排序（O(n log n)），保证确定性输出。

**候选事务（candidate transaction）**
- `Apply` 分三阶段：
  1. **结构校验**：对全部 Op 做纯结构检查（kind 合法、名称字符集与长度、Value 长度、Delete 不得携带 Value），不读取任何状态；任一失败即返回 `ErrInvalidInput`。
  2. **候选执行**：在索引的私有副本上按输入顺序执行 Put/Delete；Put 分配连续 revision，Delete 不分配；Delete 缺失名称返回 `ErrNotFound`。
  3. **容量检查**：仅在批次末对最终状态检查记录数与 Value 总字节上限，超限返回 `ErrCapacity`。
- 任一步失败直接丢弃候选副本，内部状态、generation 与 revision 完全不变，天然实现回滚，无需撤销日志。
- 非空成功批次 generation 恰好加一；空批次成功且不改变任何计数器。

**所有权**
- Put 的 Value 在写入时深拷贝，调用方之后修改入参切片不影响目录。
- `Get`/`Snapshot`/`Result.Changed` 返回的 Value 均为深拷贝，返回切片与内部状态完全隔离。

**并发**
- 单把 `sync.RWMutex` 保护全部状态：`Apply` 持写锁，`Get`/`Snapshot` 持读锁。所有公开方法可安全并发调用。

**复杂度**
- `Apply`：O(k·v + n)，k 为批内 Op 数，v 为 Value 拷贝开销，n 为候选副本克隆开销。
- `Get`：O(v)；`Snapshot`：O(n log n + 总字节数)。
- 空间：O(记录数 + Value 总字节)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
