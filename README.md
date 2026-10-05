# servicecatalog

并发安全的内存型服务目录，实现见 `SPEC.md`，仅依赖标准库（Go 1.22+）。

## 设计说明

- **索引**：`Store` 以 `map[string]Record` 为主索引，键为服务名；另维护 `totalValue`（Value 总字节）、`generation`、`revision` 三个计数器，避免批次末为计算容量而全表扫描。
- **候选事务**：`Apply` 先在完整结构校验后克隆当前记录表为候选状态，按输入顺序在候选上执行 Put/Delete（Put 递增 revision，Delete 不分配）；批次末检查记录数与总字节容量。任一失败直接丢弃候选，已提交状态、generation、revision 均不变；成功则整体换入候选并只递增一次 generation（空批次不递增）。
- **所有权**：Put 时拷贝调用方传入的 Value；`Get`/`Snapshot`/`Result.Changed` 返回的 Value 均为深拷贝，返回切片与内部状态完全隔离，调用方修改互不影响。
- **并发**：单把 `sync.RWMutex` 保护全部状态；`Apply` 持写锁，`Get`/`Snapshot` 持读锁，可多读者并发。
- **复杂度**：结构校验 O(批次大小)；`Apply` 额外 O(n) 克隆（n 为当前记录数）加 O(ops put/delete 均摊 O(1)、末尾容量检查 O(1)；`Get` O(1)；`Snapshot` O(n log n)（按名称排序）。空间 O(n)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
