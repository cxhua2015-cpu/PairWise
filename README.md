# resourcecatalog126

并发安全的内存型资源目录，仅依赖 Go 标准库（Go 1.22+）。语义详见 `SPEC.md`。

## 设计说明

**索引**
- 主索引为 `map[string]Record`，按名称 O(1) 定位记录。
- 另维护 `totalValue`（Value 总字节数）与 `revision`（已分配的最大 revision）两个计数器，避免批次末为容量检查而全表扫描。
- 有序视图（`Snapshot`、`Result.Changed`）在读取时按需排序，不为有序性维护额外索引结构。

**候选事务（Apply 三阶段）**
1. 结构校验：在不持有锁、不读取任何状态的情况下校验全部 Op（kind 合法、名称字符集与长度、Value 长度），失败返回 `ErrInvalidInput`。
2. 候选执行：在锁内把当前记录复制到候选 map，按输入顺序执行 Put/Delete；Put 在候选 revision 计数器上分配连续 revision，Delete 不分配；Delete 缺失记录返回 `ErrNotFound`。
3. 批次末容量检查：仅此时校验记录数与 Value 总字节上限，超限返回 `ErrCapacity`。
- 任何失败都直接丢弃候选状态，已提交状态、generation、revision 完全不变（天然回滚）；成功才整体提交，非空批次 generation 恰好加一。

**所有权**
- Put 的 Value 在提交前深拷贝，调用方之后修改入参切片不影响目录。
- `Get`/`Snapshot`/`Result.Changed` 返回的 Value 均为深拷贝，返回切片与内部状态完全隔离。

**并发**
- 所有公开方法通过一把 `sync.Mutex` 串行化状态访问；结构校验在锁外完成。`Store` 可安全地被多个 goroutine 并发调用。

**复杂度**（n = 当前记录数，b = 批次内 Op 数）
- `Apply`：时间 O(n + b·log b)（候选复制 O(n)，Changed 排序 O(b·log b)），额外空间 O(n)。
- `Get`：O(1)（不计返回值拷贝）。
- `Snapshot`：O(n·log n)。
- 单条 Put/Delete 摊还 O(1)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
