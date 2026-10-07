# metacatalog326

并发安全的内存型元数据目录（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

**索引**
- 主索引为 `map[string]Record`，按名称 O(1) 定位记录。
- 全量状态由一把 `sync.RWMutex` 保护：`Apply` 取写锁，`Get`/`Snapshot` 取读锁，允许并发读。
- `Snapshot` 与 `Result.Changed` 在锁内拷贝后按名称排序输出，不维护额外有序结构。

**候选事务（candidate transaction）**
- `Apply` 分三阶段：
  1. **结构校验**：在读取任何状态前校验全部 Op（kind 合法、名称字符集与长度、Value 长度、Delete 不带 Value），失败返回 `ErrInvalidInput`。
  2. **候选执行**：在索引的拷贝上按输入顺序应用 Put/Delete；Put 分配连续 revision（覆盖同名也分配），Delete 不分配且要求目标存在（否则 `ErrNotFound`）。
  3. **容量终审**：仅在批次末检查记录数与 Value 总字节，超限返回 `ErrCapacity`。
- 任何失败都直接丢弃候选副本，已提交状态、generation 与 revision 完全不变，天然回滚。非空成功批次 generation 只 +1，空批次不变。

**所有权**
- 写入时深拷贝 `Op.Value`，返回的 `Record`/`Snapshot` 同样深拷贝，调用方与 store 互不共享内存；返回值切片可安全修改。

**复杂度**（n = 批次数，m = 记录数）
- `Apply`：校验 O(n)，候选拷贝 O(m)，执行 O(n)，结果排序 O(n log n)。
- `Get`：O(1)。`Snapshot`：O(m log m)。
- 空间：O(m) 主索引 + Apply 期间 O(m) 候选副本。

## 验证

```
go test ./...
go test -race ./...
go run ./cmd/demo
```
