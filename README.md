# endpointcatalog

并发安全的内存型端点目录（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

**索引**：`Store` 以 `map[string]entry` 为主索引，键为端点名，值为 `{value, revision}`；
另维护 `totalBytes` 运行合计，使总字节容量检查为 O(1)。记录数即 `len(map)`。
`Snapshot`/`Changed` 在返回前对键排序（按名称字典序）。

**候选事务**：`Apply` 分三个阶段——
1. 结构校验：在不读取任何状态的前提下校验全部 op 的 kind、名称字符集/长度、Value 长度；
2. 候选执行：在互斥锁内把当前 map 浅拷贝为候选副本，按输入顺序重放 Put/Delete
   （Put 分配连续 revision，Delete 不分配，Delete 缺失立即返回 `ErrNotFound`）；
3. 末尾容量检查：仅对最终候选状态检查记录数与 Value 总字节数，超限返回 `ErrCapacity`。
任何失败都直接丢弃候选副本，状态、generation、revision 完全不变；成功才整体提交，
非空批次 generation 恰好 +1，空批次不变。

**所有权**：Put 的 Value 在入库前深拷贝；`Get`/`Snapshot`/`Result.Changed` 返回的
Value 均为新分配的副本，调用方对返回切片的修改不会影响内部状态，反之亦然。

**并发**：单把 `sync.RWMutex` 保护全部状态；`Apply` 持写锁，`Get`/`Snapshot` 持读锁，
所有公开方法可安全并发调用（`go test -race` 验证）。

**复杂度**（n = 记录数，b = 批内 op 数，v = 单条 Value 字节数）：
- `Apply`：校验 O(b·名称长)，候选拷贝 O(n)，重放 O(b·v)，排序 Changed O(b log b)；
- `Get`：平均 O(1)（外加返回值拷贝 O(v)）；
- `Snapshot`：O(n log n) 排序 + O(总字节数) 深拷贝；
- 空间：O(n·v)，候选事务临时多用一份同样大小的副本。
