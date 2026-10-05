# resourcecatalog106

并发安全的内存型资源目录（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 实现说明

- **索引**：`Store` 以 `map[string]Record` 作为主索引，键为资源名，值为含深拷贝 `Value` 与 `Revision` 的记录。无二级索引；`Snapshot`/`Changed` 的按名排序在读取时通过 `sort.Strings` 完成。
- **候选事务**：`Apply` 在单把 `sync.Mutex` 下先对全部 Ops 做完整结构校验（kind、名称字符集与长度、Value 长度），不触碰状态；随后在克隆出的候选 map 上按输入顺序执行 Put/Delete（Put 分配连续 revision，Delete 不分配，删不存在键即 `ErrNotFound`）；批次末才检查记录数与 Value 总字节容量（`ErrCapacity`）。任一步失败直接丢弃候选，已提交状态、generation、revision 均不变；成功则整体换入候选，非空批次 generation 只增一次。
- **所有权**：写入时拷贝调用方传入的 `Value`；`Get`/`Snapshot`/`Result.Changed` 返回的记录均含新分配的 `Value` 副本，返回切片与内部状态完全隔离，调用方可自由修改。
- **复杂度**：设批次含 k 个 op、目录含 n 条记录。结构校验 O(k)；候选克隆与回放 O(n+k)；末次容量检查 O(n)；`Apply` 总体 O(n + k log k)（`Changed` 排序）。`Get` O(1)（均摊），`Snapshot` O(n log n)。空间 O(n)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
