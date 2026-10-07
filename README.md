# metacatalog311

并发安全的内存型“元数据目录 311”，仅依赖 Go 标准库（Go 1.22+）。语义详见 `SPEC.md`。

## 设计说明

**索引**：`Store` 内部使用 `map[string]entry` 作为主索引，键为记录名，值保存深拷贝后的 Value 与分配的 revision。单把 `sync.RWMutex` 保护全部状态（索引、generation、revision）：`Apply` 持写锁，`Get`/`Snapshot` 持读锁，因此所有公开方法可并发调用。

**候选事务**：`Apply` 先对整个批次做完整结构校验（kind 合法、名称字符集与长度、Value 长度），通过后才读取状态。随后在索引的克隆（候选事务）上按输入顺序执行 Put/Delete：Put 递增并分配连续 revision，Delete 不分配 revision 且目标必须存在（否则 `ErrNotFound`）。记录数与 Value 总字节容量只在批次末检查（`ErrCapacity`）。任一步失败直接丢弃候选，已提交状态、generation、revision 全部不变；成功时整体换入候选，非空批次 generation 恰好加一，空批次不改变任何状态。

**所有权**：Put 时拷贝调用方传入的 Value；`Get`、`Snapshot`、`Result.Changed` 返回的 Value 均为新分配的副本，返回切片与内部状态完全隔离，调用方随后修改互不影响。`Snapshot` 的记录按名称排序，`NextRevision` 为下一个将分配的 revision。

**复杂度**（n = 当前记录数，b = 批次内 op 数，v = Value 总字节数）：
- `Apply`：校验 O(b)；候选克隆 O(n + v)；执行 O(b)；末次容量检查 O(n)；构造 `Changed` O(b log b)。总计 O(n + v + b log b)，额外空间 O(n + v)。
- `Get`：O(1) 平均（外加 O(value) 拷贝）。
- `Snapshot`：O(n log n + v)。
- `New`：O(1)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
