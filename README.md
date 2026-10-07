# metacatalog306

并发安全的内存型元数据目录（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

**索引**
- 主索引为 `map[string]Record`，按名称 O(1) 定位；名称本身作为键，无额外辅助索引。
- 另维护两个 O(1) 聚合计数器：`totalValue`（Value 总字节）与 `lastRevision`，避免批次末容量检查的全表扫描。
- `Snapshot`/`Changed` 在返回前按名称排序（`sort.Slice`），排序发生在拷贝出的切片上，不影响索引。

**候选事务（candidate transaction）**
- `Apply` 分两阶段：先对整个批次做完整结构校验（kind、名称字符集与长度、Value 长度），不读取任何状态；通过后再克隆一份候选 map，在候选上按输入顺序执行 Put/Delete。
- Put 分配连续 revision（Delete 不分配）；Delete 缺失键返回 `ErrNotFound`。
- 最终记录数与 Value 总字节容量只在批次末检查，越界返回 `ErrCapacity`。
- 任何失败直接丢弃候选，已提交状态、generation、revision 完全不变（天然回滚）；成功且非空批次一次性替换 map 并将 generation 加一，空批次不产生任何变化。

**所有权**
- Put 时深拷贝调用方传入的 Value；`Get`/`Snapshot`/`Result.Changed` 返回的 Value 均为新分配的副本，返回切片与内部状态完全隔离，调用方可自由修改。

**并发**
- 单把 `sync.RWMutex`：`Apply` 持写锁，`Get`/`Snapshot` 持读锁，所有公开方法可安全并发调用（`-race` 验证）。

**复杂度**
- `Apply`：O(B + N)，B 为批内 op 数，N 为当前记录数（候选克隆）；校验 O(B)。
- `Get`：O(1)（不计返回值拷贝的 O(V)）。
- `Snapshot`：O(N log N)（排序）+ O(总字节） 拷贝。
- 空间：O(N + 总字节）。

## 验证

```
go test ./...
go test -race ./...
go run ./cmd/demo
```
