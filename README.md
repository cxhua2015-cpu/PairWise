# metacatalog391

并发安全的内存型元数据目录（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

### 索引
- 主索引为 `map[string]entry`（名称 → 值/修订号），Put/Delete/Get 均为 O(1) 均摊查找。
- 另维护 `totalBytes`（Value 总字节）与 `revision`/`generation` 计数器，避免容量检查时全表扫描。
- `Snapshot` 与 `Result.Changed` 在返回前按名称排序，排序成本 O(n log n)，n 为相关记录数。

### 候选事务（candidate transaction）
`Apply` 分四个阶段：
1. **结构校验**：在加锁、读取任何状态之前，校验全部 op 的 kind、名称字符集与长度、Value 长度；失败返回 `ErrInvalidInput`。
2. **候选执行**：在仅含本批次触及键的候选 map 上按输入顺序执行 Put/Delete（未触及的键回退读主索引），同时维护候选记录数与候选总字节。Delete 不存在的键返回 `ErrNotFound`。
3. **批次末容量检查**：最终记录数与 Value 总字节超限返回 `ErrCapacity`；中途临时超限不失败。
4. **提交**：将候选写回主索引并更新计数器。任何失败发生在提交前，因此状态、generation、revision 天然全部回滚，无需显式撤销。

Put 每执行一次分配一个连续 revision；Delete 不分配。非空成功批次 generation 恰好加一，空批次不变。

### 所有权
- Put 时深拷贝调用方传入的 Value；Get/Snapshot/Result 返回的 Value 与切片均为新分配的副本，与内部状态完全隔离，调用方可自由修改。
- 并发安全由单一 `sync.RWMutex` 保证：Apply 持写锁，Get/Snapshot 持读锁。

### 复杂度
- `Apply`：O(k + m log m)，k 为 op 数，m 为批次触及且最终仍存在的键数。
- `Get`：O(1) 均摊（外加 O(|value|) 拷贝）。
- `Snapshot`：O(n log n)，n 为记录总数。
- 空间：O(n) 记录 + 每批次 O(k) 候选。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
