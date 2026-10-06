# metacatalog216

并发安全的内存型元数据目录（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计说明

- **索引**：单一日志结构无需索引；记录存放在 `map[string]entry`（名称 → 值/修订号），
  按名称 O(1) 定位。`Snapshot`/`Result.Changed` 在返回前对名称排序，保证确定性输出。
- **候选事务**：`Apply` 先对全部操作做纯结构校验（不读状态），然后在写锁内把当前
  map 克隆为候选副本，按输入顺序在其上执行 Put/Delete。Delete 未命中、最终记录数或
  Value 总字节超限时直接丢弃候选并返回错误——原始 map、generation、revision 均未
  改动，回滚为零成本。只有全部检查通过才用候选替换正式状态，并将 generation 加一。
- **所有权**：Put 的 Value 在入库前深拷贝；`Get`/`Snapshot`/`Result.Changed` 返回的
  切片均为独立副本，调用方对返回值的修改不会污染内部状态，反之亦然。
- **并发**：`sync.RWMutex` 保护全部状态；`Apply` 取写锁，`Get`/`Snapshot` 取读锁，
  所有公开方法可安全并发调用（含 `-race` 验证）。
- **复杂度**：结构校验 O(批次总字节)；候选克隆 O(n)；执行 O(批次长度)；容量检查 O(n)；
  排序输出 O(k log k)（k 为涉及/现存记录数）。`Get` 为 O(1) 均摊 + O(值长) 拷贝，
  `Snapshot` 为 O(n log n)。空间 O(n + 单批候选副本)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
