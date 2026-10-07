# metacatalog301

并发安全的内存型元数据目录（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计说明

**索引**：`Store` 内部以 `map[string]entry` 作为主索引，键为记录名，`entry` 保存 Value 副本与 revision。单把 `sync.Mutex` 保护全部状态（records、generation、nextRevision），所有公开方法（`Apply`/`Get`/`Snapshot`）均加锁，可并发调用。`Get`/`Snapshot` 在锁内按名称排序后返回深拷贝。

**候选事务**：`Apply` 先在无锁状态下做完整结构校验（kind、名称字符集与长度、Value 长度、Delete 不得携带 Value），不读取任何状态。随后加锁，将当前 map 浅拷贝为候选事务（entry 含不可共享处理的 Value 切片，Put 时总是复制新 Value，因此候选与已提交状态不共享可写内存），按输入顺序在候选上执行 Put/Delete：Put 分配连续 revision，Delete 要求记录存在（否则 `ErrNotFound`）且不分配 revision。批次末才检查最终记录数与 Value 总字节容量，超限返回 `ErrCapacity`。任一步失败直接丢弃候选，已提交状态、generation、revision 均不变；成功则整体换入候选，generation 只增加一次。`Changed` 按名称排序，记录每个名字最后一次操作的结果（Delete 无 Value/Revision）。

**所有权**：Put 的 Value 在写入时复制，调用方之后修改入参不影响目录；`Get`/`Snapshot`/`Result.Changed` 返回的 Value 均为深拷贝，调用方修改返回值不影响内部状态，返回切片与内部状态完全隔离。

**复杂度**（n = 当前记录数，m = 批次 op 数，B = 涉及 Value 总字节数）：
- `Apply`：校验 O(m)；候选拷贝 O(n)；执行 O(m + B)；末检 O(n)；提交后排序 Changed O(m log m)。总计 O(n + m log m + B)。
- `Get`：O(|value|)（深拷贝）。
- `Snapshot`：O(n log n + B)（排序 + 深拷贝）。
- 空间：O(n + B)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
