# metacatalog311

并发安全的内存型元数据目录（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

**索引**：`Store` 以 `map[string]Record` 作为主索引，按名称 O(1) 定位记录；`generation` 与 `revision` 为两个单调计数器。一把 `sync.Mutex` 保护全部状态，所有公开方法（`Apply`/`Get`/`Snapshot`）均可并发调用。

**候选事务**：`Apply` 分三个阶段——
1. 完整结构校验（kind 合法、名称字符集与长度、Put 的 Value 上限、Delete 不得携带 Value），此阶段不读取任何状态，因此结构错误优先于 `ErrNotFound` 等状态错误；
2. 在整表复制的候选 map 上按输入顺序执行 Put/Delete，Put 分配连续 revision，Delete 不分配；
3. 仅在批次末检查最终记录数与 Value 总字节容量。

任一阶段失败直接丢弃候选，状态、generation、revision 全部不变（天然回滚）；成功时整体换入候选并仅将 generation 加一。空批次成功但不改变 generation。

**所有权**：Put 时深拷贝 Value 存入；`Get`/`Snapshot`/`Result.Changed` 返回的记录均含独立 Value 副本，调用方对返回切片的任何修改都不会影响内部状态，反之亦然。`Snapshot.Records` 与 `Result.Changed` 均按名称排序。

**复杂度**：设批次含 k 个 op、表内 n 条记录、单条 Value 平均 b 字节。
- `Apply`：校验 O(k·(名称+Value 长度))，候选复制 O(n·b)，执行 O(k·b)，末检 O(n)；总体 O((n+k)·b)，空间 O(n·b)。
- `Get`：O(b)（拷贝返回的 Value）。
- `Snapshot`：O(n·b + n log n)（深拷贝 + 排序）。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
