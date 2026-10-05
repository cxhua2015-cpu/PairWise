# modelcatalog

Read `SPEC.md` and implement the package.

## 设计说明

### 索引
- 主索引为 `map[string]entry`（名称 → `{value, revision}`），按名称 O(1) 定位记录。
- `Store` 另维护单调递增的 `generation` 与 `lastRevision` 两个计数器。
- 排序视图（`Snapshot.Records`、`Result.Changed`）在读取时按名称排序生成，不维护额外有序结构。

### 候选事务（candidate transaction）
`Apply` 分三个阶段，全程持有互斥锁：
1. **结构校验**：先校验全部 Op 的 kind、名字符集/长度、Value 长度，不读取任何状态；失败返回 `ErrInvalidInput`。
2. **候选执行**：把当前记录复制到候选 map，按输入顺序在其上执行 Put/Delete。Put 使候选 revision 递增并写入，Delete 不分配 revision；删除不存在的名称返回 `ErrNotFound`。
3. **末尾容量检查**：仅在候选终态上检查记录数（`MaxRecords`）与 Value 总字节（`MaxTotalValueBytes`），超限返回 `ErrCapacity`。

任一阶段失败都直接丢弃候选，状态、`generation`、`revision` 完全不变（天然回滚）；全部通过才一次性提交候选 map 与计数器。非空成功批次 `generation` 只加一，空批次不变。

### 所有权
- 写入时复制：Put 的 `Value` 在入库前深拷贝，调用方之后修改入参不影响目录。
- 读取时复制：`Get`/`Snapshot`/`Result.Changed` 中的 `Value` 均为深拷贝，调用方修改返回值不影响内部状态；返回切片亦为新建，与内部状态隔离。
- `Changed` 按名称排序，每个被触及的名称一条：仍存在的记录带最终 Value 与 Revision；被删除的记录 `Value` 为 nil、`Revision` 为 0。

### 并发
所有公开方法通过一把 `sync.Mutex` 串行化，支持任意并发调用；`go test -race ./...` 通过。

### 复杂度
- `New`：O(1)。
- `Apply`：O(n + m + k log k)，n 为现有记录数（候选复制），m 为批内 Op 数，k 为被触及名称数（排序）；容量检查 O(n)。
- `Get`：O(1)（外加一次 Value 拷贝 O(v)）。
- `Snapshot`：O(n log n)（排序）+ O(总字节数)（深拷贝）。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
