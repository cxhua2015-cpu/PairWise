# metacatalog386

并发安全的内存型元数据目录（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

**索引**：`Store` 以 `map[string]entry` 作为主索引，名称为键，值为 `{value, revision}`；另维护 `totalBytes`、`generation`、`revision` 三个计数器。所有记录按名称无序存储，排序只在读取侧进行。

**候选事务**：`Apply` 先对整个批次做完整结构校验（kind、名称字符集与长度、Value 长度、Delete 不带 Value），不触碰任何状态。随后按输入顺序执行：Put 使 revision 单调递增并写入深拷贝的 Value，Delete 不分配 revision。执行期间为每个被触及的名称保存一份旧值备份（clone-on-write）；任一步失败（`ErrNotFound`）或批次末容量检查（记录数 / Value 总字节，`ErrCapacity`）失败时，用备份回滚全部记录，且 `totalBytes`、`revision`、`generation` 均不落盘，状态与失败前完全一致。非空成功批次 `generation` 恰好加一，空批次不变。

**所有权**：所有进入（Put 的 Value）与离开（`Result.Changed`、`Get`、`Snapshot`）的字节切片都做深拷贝，调用方对返回切片的修改不会影响内部状态，反之亦然。`Snapshot` 与 `Changed` 按名称排序，返回切片与内部状态完全隔离。

**并发**：单把 `sync.RWMutex` 保护全部状态；`Apply` 持写锁，`Get`/`Snapshot` 持读锁，可并行读取。

**复杂度**：批次结构校验 O(B)，B 为批次总字节数；执行 O(k)，k 为 op 数；回滚 O(t)，t 为被触及名称数；`Changed`/`Snapshot` 排序 O(n log n)，n 为（变更/全部）记录数；`Get` 为 O(1) 均摊。额外空间 O(t + v)，v 为变更记录的字节数。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
