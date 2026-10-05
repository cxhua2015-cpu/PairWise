# resourcecatalog191

并发安全的内存型资源目录（Go 1.22+，仅标准库）。原子批次按输入顺序执行
Put/Delete，失败整体回滚。公开 API 与错误值见 `resourcecatalog191/servicecatalog.go`，
语义规范见 `SPEC.md`。

## 设计说明

- **索引**：`Store` 内部以 `map[string]record` 作为唯一主索引，键为资源名，
  值保存深拷贝后的 `Value` 与分配的 `Revision`；另维护 `totalValue` 运行和，
  使总字节容量检查为 O(1)。`Get`/`Snapshot`/`Apply` 均通过该索引完成，无二级索引。
- **候选事务**：`Apply` 先对整个批次做完整结构校验（kind、名称字符集与长度、
  Value 长度），不触碰任何状态；随后在互斥锁内把当前 map 复制为候选副本，
  按输入顺序在副本上应用 Put/Delete（Put 递增 revision，Delete 不分配），
  批次末才检查记录数与 Value 总字节容量。任何一步失败直接丢弃候选，
  已提交状态、generation、revision 全部不变；成功时一次性换入候选，
  非空批次 generation 恰好加一。
- **所有权**：进入 `Store` 的 `Value` 一律深拷贝，返回值（`Record`、`Changed`、
  `Snapshot.Records`）同样深拷贝，调用方后续修改其切片不影响内部状态，
  内部状态也不别名任何已返回的切片。
- **并发**：所有公开方法由单一 `sync.Mutex` 保护；结构校验在锁外完成，
  状态读写与提交在锁内完成，`-race` 下无数据竞争。
- **复杂度**：`Get` O(1)；`Snapshot` O(n log n)（按名称排序）；`Apply` 结构校验
  O(m)，候选复制 O(n)，应用 O(m)，构造按名称排序的 `Changed` O(m log m)，
  其中 n 为记录数、m 为批次内操作数；容量检查 O(1)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
