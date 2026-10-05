# resourcecatalog121

并发安全的内存型资源目录（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计要点

- **索引**：`Store` 以 `map[string]Record` 作为主索引，按名称 O(1) 定位记录；`Snapshot` 与 `Result.Changed` 在返回前对名称排序，保证确定性输出。
- **候选事务**：`Apply` 分两阶段。阶段一仅做完整结构校验（kind、名称字符集与长度、Value 长度），不读取任何状态；阶段二将当前 map 克隆为候选副本，按输入顺序在其上执行 Put/Delete，Put 递增本地 revision 计数，Delete 不分配。仅在批次末对候选副本检查记录数与 Value 总字节容量；任一失败直接丢弃候选副本，主状态、generation 与 revision 完全不变（天然回滚）。全部成功才整体提交，generation 恰好加一。
- **所有权**：Put 时深拷贝调用方传入的 Value；`Get`/`Snapshot`/`Result.Changed` 返回的 Value 均为新分配的副本，返回切片与内部状态完全隔离，调用方修改互不影响。
- **并发**：所有公开方法由一把 `sync.Mutex` 保护，批次整体串行化，保证原子性与线性一致性。

## 复杂度

- `New`：O(1)。
- `Get`：O(L)，L 为 Value 长度（深拷贝）。
- `Snapshot`：O(N log N + B)，N 为记录数，B 为 Value 总字节数。
- `Apply`：O(M + N + B)，M 为批次操作数（校验与克隆候选副本），排序 Changed 为 O(M log M)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
