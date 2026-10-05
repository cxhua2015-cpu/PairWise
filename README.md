# resourcelease179

并发安全的内存型资源租约表（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 索引

表内部使用 `map[string]Entry` 作为唯一主索引，键为租约名称，值为 `{Key, ExpiresAt, Revision}`。
不维护额外的过期堆或有序索引：`Apply` 的预淘汰与 `Expire` 均为全表扫描，按 `ExpiresAt <= now`
闭区间边界删除。`Snapshot` 与 `Expire` 返回的条目按键字典序排序，保证规范顺序。

## 候选事务

`Apply` 分两个阶段：

1. **结构校验**：在读取任何状态之前完整校验整个批次（`Now >= 0`、kind 合法、键字符集与
   字节上限、`ExpiresAt >= 0`），任何违规返回 `ErrInvalidInput`。
2. **候选执行**：加锁后检查时间单调性（`Now < now` 返回 `ErrTime`），随后把当前条目复制到
   候选 map，先删除 `ExpiresAt <= Now` 的条目，再顺序执行 Put/Touch/Delete；Put/Touch 从
   候选 `nextRev` 分配递增 revision。只有全部操作成功且最终容量不超过 `MaxEntries` 时，
   才把候选条目、时间、revision 计数器和 `generation+1` 一次性提交；任何错误
   （`ErrNotFound`/`ErrCapacity`）都直接丢弃候选状态，实现完整回滚。空批次不改变任何状态，
   generation 不变。

## 所有权

所有公开方法（`Apply`/`Expire`/`Snapshot`）通过单个 `sync.Mutex` 串行化，支持并发调用。
返回的 `[]Entry` 与 `Snapshot.Entries` 均为新分配的切片和值拷贝，调用方修改不会影响内部状态；
内部也绝不保留调用方传入的切片。

## 复杂度

- `Apply`：O(n + m)，n 为当前条目数（候选复制与淘汰扫描），m 为批内操作数。
- `Expire`：O(n + k log k)，k 为过期条目数（排序）。
- `Snapshot`：O(n log n)（排序）；空间 O(n)。
- 单操作（Put/Touch/Delete）：均摊 O(1)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
