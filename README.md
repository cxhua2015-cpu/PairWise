# metacatalog301

并发安全的内存型元数据目录（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计说明

**索引**：`Store` 内部使用 `map[string]entry` 作为主索引，键为记录名，`entry` 保存深拷贝后的 Value 与该记录的 revision。`Snapshot`/`Result.Changed` 在返回前对键排序（`sort.Strings`），保证按名称有序输出。

**候选事务**：`Apply` 先做整批结构校验（kind、名称字符集与长度、Value 长度），不触碰任何状态；随后在互斥锁内把当前 map 克隆为候选事务，按输入顺序执行 Put/Delete——Put 递增并分配连续 revision，Delete 不分配。记录数与 Value 总字节容量只在批次末对候选状态检查。任一步失败（`ErrNotFound`/`ErrCapacity`）直接丢弃候选，状态、generation、revision 全部不变；成功时一次性换入候选 map，非空批次 generation 只加一。

**所有权**：写入时拷贝调用方传入的 Value；`Get`/`Snapshot`/`Result.Changed` 返回的 Value 均为新分配的副本，返回切片与内部状态完全隔离，调用方可自由修改。

**并发**：所有公开方法通过单个 `sync.Mutex` 串行化状态访问；结构校验在锁外完成。已用 `go test -race` 验证。

**复杂度**：设批次含 k 个 op、当前 n 条记录。`Apply` 为 O(n + k) 状态克隆加 O(k) 执行，末尾容量检查 O(n)，排序 Changed O(k log k)；`Get` 为 O(1)（外加 O(|Value|) 拷贝）；`Snapshot` 为 O(n log n)。空间 O(n + k)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
