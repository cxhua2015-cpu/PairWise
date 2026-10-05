# metacatalog201

并发安全的内存型元数据目录（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计说明

**索引**：`Store` 使用单个 `map[string]entry` 作为主索引（名称 → 值/revision），
配合 `sync.RWMutex`：`Apply` 持写锁，`Get`/`Snapshot` 持读锁，因此读写可并行、写写互斥。
Snapshot 的排序视图在读取时按需构建，不维护冗余有序结构。

**候选事务**：`Apply` 先在完整结构校验（kind、名称字符集与长度、Value 长度）通过后，
把当前 map 浅拷贝为候选状态，按输入顺序在候选上执行 Put/Delete：Put 分配连续
revision，Delete 要求存在否则 `ErrNotFound`。记录数与 Value 总字节容量只在批次末
检查；任一步失败直接丢弃候选，已提交状态、generation 与 revision 完全不变。
成功时非空批次 generation 只加一，空批次不变。`Result.Changed` 按名称排序，包含本批
触及名称的最终状态（被删除的名称以 nil Value 表示）。

**所有权**：Put 时深拷贝调用方传入的 Value；`Get`/`Snapshot` 返回深拷贝的 Value 与
独立切片，返回值与内部状态完全隔离，调用方可自由修改。

**复杂度**：设批次长度 B、当前记录数 N。`Apply` 为 O(N + B + C log C)
（候选拷贝 + 顺序执行 + Changed 排序，C 为触及名称数）；`Get` 为 O(1) 均摊；
`Snapshot` 为 O(N log N)。空间 O(N)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
