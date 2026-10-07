# metacatalog381

并发安全的内存型元数据目录（Go 1.22+，仅标准库）。实现见 `metacatalog381/servicecatalog.go`，语义以 `SPEC.md` 与契约测试为准。

## 设计说明

**索引**：`Store` 内部使用 `map[string]entry` 作为主索引，键为记录名，`entry` 保存 `Value` 副本与 `Revision`。`generation` 与 `nextRevision` 为单调计数器，revision 从 1 开始分配。

**候选事务**：`Apply` 先在持锁状态下把主索引浅拷贝为候选 map（`entry` 中的 `[]byte` 只被替换、从不原地修改，因此浅拷贝安全），再按输入顺序在候选上执行 Put/Delete：Put 分配连续 revision，Delete 不分配、且目标不存在即返回 `ErrNotFound`。记录数与 Value 总字节容量只在批次末对候选检查。任一步失败直接丢弃候选，`generation`、`nextRevision` 与主索引全部保持不变，实现回滚；成功时一次性换入候选，`generation` 仅加一（空批次不变）。

**所有权**：Put 时拷贝调用方传入的 `Value`；`Get`、`Snapshot`、`Result.Changed` 返回的 `Value` 与记录切片均为深拷贝，返回值与内部状态完全隔离，调用方可自由修改。

**并发**：所有公开方法通过单把 `sync.Mutex` 串行化，结构校验不需要状态、可在锁外完成；Get/Snapshot 在锁内完成拷贝，保证一致快照。

**复杂度**（n 为记录数，k 为批次内 op 数，B 为涉及的字节总量）：
- `Apply`：O(n + k) 时间（候选拷贝 + 顺序执行 + 末次容量扫描），O(n) 额外空间；revision 分配 O(1)/op。
- `Get`：O(1) 定位 + O(value) 拷贝。
- `Snapshot`：O(n log n) 排序 + O(Σvalue) 拷贝。
- 空间：O(n + Σvalue)。

## 验证

```
go test ./...
go test -race ./...
go run ./cmd/demo
```
