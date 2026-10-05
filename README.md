# featureflags

并发安全的内存型功能开关目录（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计说明

- **索引**：记录存储在 `map[string]entry` 中，按名称 O(1) 查找；`entry` 持有深拷贝的 `Value` 与单调递增的 `Revision`。另维护 `totalValue` 计数器用于容量核算，避免遍历求和。
- **候选事务**：`Apply` 先在无锁状态下对整个批次做完整结构校验（名称字符集/长度、Kind 合法性、Delete 不带 Value、单值长度上限）。随后持写锁，把当前 map 克隆为候选事务，按输入顺序应用 Put/Delete；Put 分配连续 revision，Delete 要求记录存在（否则 `ErrNotFound`）。记录数与 Value 总字节容量只在批次末检查，超限返回 `ErrCapacity`。任何失败直接丢弃候选，已提交状态、generation 与 revision 天然不变；成功时整体换入候选，generation 恰好加一。
- **所有权**：进入（Put 的 Value）与离开（`Get`/`Snapshot`/`Result.Changed`）的字节切片全部深拷贝，调用方后续修改不影响内部状态，返回值也不别名内部状态。`Snapshot` 与 `Changed` 均按名称排序。
- **并发**：单把 `sync.RWMutex` 保护全部状态；写路径（`Apply`）持写锁，读路径（`Get`/`Snapshot`）持读锁，可并行。
- **复杂度**：结构校验 O(批次总字节)；候选克隆 O(n)；应用 O(k)（k 为 op 数）；`Get` O(1)；`Snapshot`/`Changed` 排序 O(n log n)。空间 O(n + k)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
