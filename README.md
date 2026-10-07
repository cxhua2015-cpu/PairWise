# metacatalog356

并发安全的内存型元数据目录（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

- **索引**：`Store` 内部使用 `map[string]Record` 作为主索引，按名称 O(1) 定位记录；另维护 `totalValue`、`generation`、`revision` 三个标量计数器，避免容量检查与快照时重复聚合。
- **候选事务**：`Apply` 先对全部 Op 做纯结构校验（不触碰状态），再在索引的副本（候选事务）上按输入顺序执行 Put/Delete。Put 分配连续 revision，Delete 不分配。记录数与 Value 总字节上限只在批次末检查；任何失败直接丢弃候选，已提交的状态、generation 与 revision 完全不受影响，实现原子回滚。
- **所有权**：Put 时深拷贝 Value 存入，调用方之后修改入参切片不影响目录；`Get`/`Snapshot`/`Result.Changed` 均返回深拷贝，调用方修改返回值不影响内部状态。`Snapshot` 的记录按名称排序。
- **并发**：所有公开方法通过一把 `sync.Mutex` 串行化，结构校验在加锁前完成（只读 `Options`），无数据竞争；空批次成功但不增加 generation。
- **复杂度**：结构校验 O(批次总字节)；候选事务复制 O(n)（n 为当前记录数），每个 Op O(1) 均摊；批次末容量检查 O(1)；`Get` O(1) 均摊；`Snapshot` O(n log n)（排序）。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
