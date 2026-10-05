# resourcecatalog151

并发安全的内存型资源目录，实现见 `SPEC.md`。仅依赖标准库，需 Go 1.22+。

## 设计说明

- **索引**：`Store` 内部以 `map[string]Record` 作为主索引，按名称 O(1) 定位记录；另维护 `totalValue`（Value 总字节数）、`generation` 与 `nextRevision` 计数器。记录数与总字节数均通过该索引 O(1) 得出，无需遍历。
- **候选事务**：`Apply` 先在无锁状态下对整个批次做完整结构校验（kind、名称字符集与长度、Value 长度），再在互斥锁内把当前索引浅拷贝为候选 map，按输入顺序应用 Put/Delete。Put 深拷贝 Value 并分配连续 revision，Delete 不分配 revision。记录数与 Value 总字节容量只在批次末检查；任何失败（`ErrNotFound`/`ErrCapacity`）直接丢弃候选 map 与候选计数器，已提交状态、generation、revision 全部不变，天然回滚。
- **所有权**：存入的 Value 在 Put 时深拷贝，`Get`/`Snapshot`/`Result.Changed` 返回的 Value 均为深拷贝副本，调用方对返回切片的修改不会影响内部状态，反之亦然。`Snapshot` 与 `Changed` 均按名称排序。
- **并发**：所有公开方法通过一把 `sync.Mutex` 串行化内部状态访问；结构校验在锁外完成，无共享状态读取，全部方法可安全并发调用（`go test -race` 通过）。
- **复杂度**：设批次含 k 个 op、目录含 n 条记录。`Apply` 结构校验 O(k)，候选拷贝 O(n)，应用 O(k)，结果排序 O(k log k)，总计 O(n + k log k)。`Get` 为 O(1)（外加 O(value) 拷贝），`Snapshot` 为 O(n log n)（排序）外加 O(总字节数) 拷贝。空间 O(n + k)。

## 验证

```
go test ./...
go test -race ./...
go run ./cmd/demo
```
