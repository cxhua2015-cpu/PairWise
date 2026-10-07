# metacatalog316

并发安全的内存型元数据目录（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

- **索引**：记录存储在 `map[string]entry` 中，按名称 O(1) 定位；`entry` 保存深拷贝的 Value 与分配时的 revision。另维护 `totalBytes` 计数器，使总字节容量检查为 O(1)。
- **候选事务**：`Apply` 先对整个批次做完整结构校验（不读状态），然后在写锁内把当前 map 克隆为候选副本，按输入顺序在其上执行 Put/Delete。Put 分配连续递增的 revision，Delete 不分配；同批次内重复 Put 覆盖候选值。记录数与 Value 总字节上限只在批次末检查，越限或中途 `ErrNotFound` 时直接丢弃候选副本——generation、revision 与记录全部自然回滚，无需逆向补偿。仅当全部成功时才用候选副本原子替换正式状态，且非空批次 generation 只加一。
- **所有权**：进入 Store 的 Value 在 Put 时深拷贝；`Get`/`Snapshot`/`Result.Changed` 返回的 Value 与切片均为独立副本，调用方对返回数据的修改不影响内部状态，反之亦然。`Snapshot.Records` 与 `Result.Changed` 均按名称排序。
- **并发**：单把 `sync.RWMutex` 保护全部状态；`Apply` 持写锁，`Get`/`Snapshot` 持读锁，可并行读取。
- **复杂度**：结构校验 O(Σ 输入字节)；候选克隆 O(n)；批次执行 O(m)；末次容量检查 O(1)。`Get` O(1)（外加返回值拷贝 O(|v|)），`Snapshot` O(n log n)（排序）。空间 O(n + 批次内变更数)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
