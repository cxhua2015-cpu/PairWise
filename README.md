# resourcecatalog191

并发安全的内存型资源目录（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计说明

- **索引**：`Store` 内部使用 `map[string]entry` 作为唯一主索引，键为资源名称；`entry` 保存 Value 副本与 revision。另维护 `totalVal`（Value 总字节数）与 `nextRev`（下一个待分配 revision）两个计数器，避免每次容量检查都全表扫描。
- **候选事务**：`Apply` 先对整个批次做纯结构校验（kind、名称字符集与长度、Value 长度、Delete 不得携带 Value），不触碰任何状态；随后在写锁内把当前 map 浅拷贝为候选表，按输入顺序在候选表上执行 Put/Delete。Put 从局部 `nextRev` 连续分配 revision，Delete 不分配。批次末才检查最终记录数与 Value 总字节容量；任何失败直接丢弃候选表，状态、generation、revision 全部自然回滚。仅当全部成功时才用候选表整体替换主索引，且非空成功批次 generation 只增加一次。
- **所有权**：Put 时深拷贝调用方传入的 Value；`Get`/`Snapshot`/`Result.Changed` 返回的 Value 均为新分配的副本，返回切片与内部状态完全隔离，调用方对返回数据的修改不会影响目录，反之亦然。
- **并发**：单把 `sync.RWMutex` 保护全部内部状态。`Apply` 持写锁，`Get`/`Snapshot` 持读锁，可并发执行。
- **复杂度**：结构校验 O(批次大小)；`Apply` 为 O(当前记录数 + 批次大小)（候选表拷贝），容量检查 O(1)；`Get` O(1)；`Snapshot` 为 O(n log n)（按名称排序），n 为当前记录数。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
