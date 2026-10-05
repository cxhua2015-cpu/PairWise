# resourcecatalog161

并发安全的内存型资源目录（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

**索引**：`Store` 以 `map[string]entry` 为主索引，键为资源名称，值保存深拷贝后的
`Value` 与 `Revision`。`Get` 为 O(1) 查表；`Snapshot` 将记录按键名排序后返回。
另维护单调递增的 `generation` 与 `revision` 计数器。

**候选事务**：`Apply` 分两阶段。第一阶段在不加锁的情况下做完整结构校验
（kind 合法、名称字符集与长度、Value 长度），失败即返回 `ErrInvalidInput`，
不读取任何状态。第二阶段持写锁，把当前 map 浅拷贝为候选 map，按输入顺序在
候选上执行 Put/Delete：Put 分配连续 revision，Delete 要求目标存在
（否则 `ErrNotFound`）。所有操作完成后才在候选上检查最终记录数与 Value 总字节
容量（`ErrCapacity`）。任一失败直接丢弃候选，状态、generation、revision 均不变；
成功则整体换入候选，非空批次 generation 恰好加一。

**所有权**：Put 时拷贝调用方传入的 `Value`；`Get`/`Snapshot`/`Result.Changed`
返回的切片均为新分配的深拷贝，调用方对返回值的任何修改不影响内部状态，
返回的切片与内部状态完全隔离。

**并发**：单把 `sync.RWMutex` 保护全部状态。`Apply` 取写锁，`Get`/`Snapshot`
取读锁可并行。结构校验在锁外完成，缩短临界区。

**复杂度**（n = 当前记录数，m = 批次操作数，B = 涉及的字节总量）：
- `Apply`：校验 O(m)；候选拷贝 O(n)；执行 O(m)；容量检查 O(n)；总时间
  O(n + m)，额外空间 O(n + B)。
- `Get`：O(1) 查找 + O(|Value|) 拷贝。
- `Snapshot`：O(n log n) 排序 + O(B) 拷贝。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
