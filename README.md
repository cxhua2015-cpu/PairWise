# metacatalog391

并发安全的内存型元数据目录（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

**索引**：`Store` 以 `map[string]entry` 作为主索引，键为记录名，值持有深拷贝后的
`Value` 与 `Revision`。另维护 `totalValue`（Value 总字节）与 `nextRevision` 计数器，
使容量检查与 revision 分配均为 O(1)。`Snapshot`/`Changed` 在返回前按名称排序，
不为排序维护额外索引。

**候选事务**：`Apply` 分四个阶段——(1) 完整结构校验（kind、名称字符集与长度、
Value 长度），不读任何状态；(2) 状态读取：按批次顺序模拟存在性，Delete 目标必须
在已提交状态或本批次此前的 Put 中存在，否则 `ErrNotFound`；(3) 在候选 map 副本上
按输入顺序应用 Put/Delete，Put 分配连续 revision，Delete 不分配；(4) 仅对候选最终
状态检查记录数与 Value 总字节上限。任一阶段失败即丢弃候选，已提交状态、
generation 与 revision 完全不变（天然回滚，无需补偿日志）；全部通过才一次性提交，
非空成功批次 generation 只加一。

**所有权**：进入 `Store` 的 Value 在 Put 时深拷贝；`Get`/`Snapshot`/`Result.Changed`
返回的 Value 与切片均为新分配的副本，调用方对返回值的修改不会影响内部状态，
内部状态的后续变化也不影响已返回的值。

**并发**：所有公开方法由同一把 `sync.Mutex` 保护。结构校验在锁外完成（只读
`Options`，不可变），缩短临界区；批次整体在锁内原子提交，因此并发批次之间的
revision 严格单调且不重复。

**复杂度**：结构校验与候选应用为 O(批次操作数)；`Get` 为 O(1) 均摊（外加返回值
拷贝 O(len(Value))）；`Snapshot` 与 `Changed` 为 O(R log R)（R 为返回记录数，
源于按名称排序）；候选构建为 O(当前记录数 + 批次操作数)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
