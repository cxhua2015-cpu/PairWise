# metacatalog331

并发安全的内存型元数据目录（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

**索引**
- 主索引为 `map[string]Record`，按名称 O(1) 定位记录；另维护 `totalValue`（Value 总字节数）、`generation`、`revision` 三个计数器，避免容量检查时的全表扫描。
- `Snapshot` 与 `Result.Changed` 在返回前按名称排序，内部不维护有序结构。

**候选事务（candidate transaction）**
- `Apply` 分两阶段：先对整个批次做完整结构校验（kind、名称字符集与长度、Value 长度），不读取任何状态；随后在互斥锁内把当前记录表浅拷贝为候选表，按输入顺序应用 Put/Delete。
- Put 在候选表上分配连续 revision 并深拷贝 Value；Delete 不分配 revision，删除不存在的名称即返回 `ErrNotFound`。
- 记录数与 Value 总字节容量只在批次末检查，超出返回 `ErrCapacity`。
- 任一步失败直接丢弃候选表，状态、generation、revision 全部不变；成功时整体换入候选表，非空批次 generation 恰好加一，空批次不产生任何变化。

**所有权**
- 写入时深拷贝 `Op.Value`；`Get`、`Snapshot`、`Result.Changed` 返回的 Value 均为独立副本，调用方对返回切片的修改不会影响内部状态，反之亦然。

**并发**
- 所有公开方法共用一把 `sync.RWMutex`：`Apply` 取写锁，`Get`/`Snapshot` 取读锁，可安全并发调用。

**复杂度**
- `Apply`：O(n + m)，n 为批次操作数，m 为当前记录数（候选表拷贝），另加 Changed 排序 O(c log c)。
- `Get`：O(1)（含返回值拷贝 O(v)）。
- `Snapshot`：O(m log m)，主要来自按名称排序。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
