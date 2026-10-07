# metacatalog336

并发安全的内存型元数据目录（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

- **索引**：`Store` 内部使用 `map[string]Record` 作为主索引，按名称 O(1) 定位记录；名称即键，无二级索引。`Snapshot` 与 `Result.Changed` 在返回前按键名排序（`sort.Strings`），保证输出确定性。
- **候选事务**：`Apply` 分两阶段。第一阶段在加锁前对整批做完整结构校验（kind、名称字符集与长度、Value 长度），不读取任何状态。第二阶段在互斥锁内把当前索引浅克隆为候选 map，按输入顺序在其上执行 Put/Delete：Put 递增并分配连续 revision，Delete 不分配 revision 且要求键存在（否则 `ErrNotFound`）。记录数与 Value 总字节容量只在批次末对候选整体检查（`ErrCapacity`）。任一失败直接丢弃候选，`generation`/`revision` 与索引保持原样，实现天然回滚；全部通过才用候选替换索引，且非空批次 `generation` 只增一次。
- **所有权**：Put 时拷贝调用方传入的 Value；`Get`/`Snapshot`/`Result.Changed` 返回的 Value 均为深拷贝，返回切片为新建，调用方对返回值的任何修改不影响内部状态，反之亦然。
- **并发**：所有公开方法由单个 `sync.Mutex` 保护；结构校验只依赖不可变的 `Options`，可在锁外执行。
- **复杂度**：结构校验 O(批次总字节)；候选克隆 O(n)（n 为当前记录数）；批次应用 O(m)（m 为 op 数）；容量检查 O(n)；`Get` O(1)；`Snapshot` O(n log n)（排序）。除候选克隆与返回拷贝外无额外分配热点。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
