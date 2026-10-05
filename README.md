# endpointcatalog

并发安全的内存型端点目录（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

- **索引**：`Store` 以 `map[string]Record` 作为主索引，按名称 O(1) 定位记录；另维护 `totalValue`（Value 总字节数）、`generation` 与 `nextRev` 计数器，避免批次末重新扫描求和。
- **候选事务**：`Apply` 分两阶段。第一阶段对整个批次做完整结构校验（kind、名称字符集与长度、Value 上限），不读取任何状态；第二阶段在写锁内把当前记录复制到候选 map，按输入顺序执行 Put/Delete（Put 分配连续 revision，Delete 不分配），仅在批次末检查记录数与 Value 总字节容量。任一步失败直接丢弃候选 map，已提交的记录、generation 与 revision 全部保持不变，实现天然回滚。
- **所有权**：Put 的 Value 在入库时深拷贝；`Get`/`Snapshot`/`Result.Changed` 返回的 Value 均为独立副本，调用方对返回切片的修改不影响内部状态，反之亦然。`Snapshot.Records` 与 `Result.Changed` 按名称排序。
- **并发**：单把 `sync.RWMutex` 保护全部状态；`Apply` 取写锁，`Get`/`Snapshot` 取读锁，所有公开方法可安全并发调用。
- **复杂度**：结构校验 O(批次总字节数)；候选执行 O(B + R)，其中 B 为批次数、R 为当前记录数（候选复制）；`Get` O(1)；`Snapshot` O(R log R)（排序）；结果排序 O(C log C)，C 为变更名称数。空间 O(R + B)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
