# metacatalog356

并发安全的内存型元数据目录（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计说明

**索引**：`Store` 内部以 `map[string]entry` 作为主索引，键为记录名，`entry` 保存
深拷贝后的 Value 与分配的 revision；另维护 `totalBytes`（Value 总字节）、
`generation` 与 `revision` 计数器。单把 `sync.Mutex` 保护全部状态，所有公开方法
（`Apply`/`Get`/`Snapshot`）均可并发调用。

**候选事务**：`Apply` 分三个阶段——
1. 结构校验：对整个批次先做完整校验（kind、名称字符集与长度、Value 长度），
   不读取任何状态，任一失败即返回 `ErrInvalidInput`；
2. 候选执行：在记录的副本（候选事务）上按输入顺序执行 Put/Delete，Put 分配
   连续递增的 revision，Delete 不分配；Delete 缺失名称返回 `ErrNotFound`；
3. 末尾容量检查与提交：仅在批次末检查记录数与 Value 总字节上限，超限返回
   `ErrCapacity`。任何失败都直接丢弃候选，已提交状态、generation 与 revision
   完全不变（天然回滚）；成功时一次性换入候选，非空批次 generation 只加一。

**所有权**：Put 时拷贝调用方传入的 Value；`Get`/`Snapshot`/`Result.Changed`
返回的 Value 均为新分配的深拷贝，调用方对返回切片的修改不会影响目录内部状态，
反之亦然。`Snapshot` 的记录按名称排序，`NextRevision` 为下一个将分配的 revision。

**复杂度**（n = 当前记录数，k = 批次操作数，B = 涉及 Value 总字节）：
- `Apply`：校验 O(k)，候选复制 O(n)，执行 O(k·B)，排序 Changed O(k log k)；
- `Get`：O(|value|)（深拷贝）；
- `Snapshot`：O(n·B + n log n)（深拷贝 + 按名称排序）。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
