# artifactindex

并发安全的内存型制品索引，面向分布式控制面。仅依赖 Go 标准库（Go 1.22+）。语义以 `SPEC.md` 与 `artifactindex/contract_test.go` 为准。

## 索引

`Store` 内部以 `map[string]entry` 保存名称到 `{value, revision}` 的映射，并维护三个单调/派生量：`generation`（每个非空成功批次 +1）、`revision`（仅 Put 分配，连续递增）、`totalBytes`（所有 Value 字节总数）。一把 `sync.Mutex` 保护全部状态，所有公开方法（`Apply`/`Get`/`Snapshot`）可并发调用。`Snapshot` 在锁内按名称排序后拷贝返回。

## 候选事务

`Apply` 分三个阶段：

1. **结构校验**：对整个批次先做纯结构校验（kind 合法、名称非空且仅含 `[a-z0-9-_]`、名称/Value 字节上限），此阶段不读取任何状态，失败即返回 `ErrInvalidInput`。
2. **候选执行**：按输入顺序在“候选事务”上应用 Put/Delete。首次触碰某名称前备份其旧条目；Put 分配连续 revision 并深拷贝 Value，Delete 不分配 revision、删除不存在的名称返回 `ErrNotFound`。
3. **容量检查**：记录数与 Value 总字节容量只在批次末检查，超出返回 `ErrCapacity`。

任一阶段失败都会用备份回滚全部记录、`totalBytes` 与 `revision`，`generation` 不增加——批次对外表现为全有或全无。成功时 `generation` 只增加一次，`Result.Changed` 包含本批次触及且最终仍存在的记录（按名称排序，深拷贝）。

## 所有权

- **存入即拷贝**：`Apply` 中的 `Op.Value` 在写入前深拷贝，调用方之后修改原切片不影响索引。
- **取出即拷贝**：`Get` 与 `Snapshot` 返回的 `Record.Value` 均为深拷贝，调用方修改返回值不会污染内部状态；返回切片与内部状态完全隔离。
- 索引从不保留调用方传入的切片，也从不把内部切片暴露给调用方。

## 复杂度

设批次含 `b` 个 op、触及 `u` 个不同名称、索引共 `n` 条记录、Value 平均长 `m`：

- `Apply`：时间 `O(b·m + u log u)`（校验与执行线性，Changed 排序 `u log u`），额外空间 `O(u·m)`（备份与 Changed）。
- `Get`：时间 `O(m)`（一次 map 查找加拷贝），额外空间 `O(m)`。
- `Snapshot`：时间 `O(n·m + n log n)`，额外空间 `O(n·m)`。
- 内存总量受 `Options.MaxRecords` 与 `Options.MaxTotalValueBytes` 约束。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
