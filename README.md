# locklease

并发安全的内存型“锁租约表”，使用显式非负单调时间，仅依赖标准库（Go 1.22+）。语义详见 `SPEC.md`。

## 索引

- 主索引为 `map[string]Entry`，按 key 精确查找，Put/Touch/Delete 均为 O(1) 均摊。
- 未维护按过期时间的堆/有序索引：每批次的淘汰与 `Expire` 采用全表扫描 O(n)。在“批次内先淘汰再执行”的语义下，扫描实现更简单且易于保证候选事务的一致性；若条目规模增大，可替换为最小堆而不改变公开语义。
- `Snapshot` 与 `Expire` 返回的条目按 key 排序，保证输出确定性。

## 候选事务

`Apply` 分两阶段：

1. **校验**：先对整个批次做纯结构校验（kind 合法、key 字符集与字节上限、`Now`/`ExpiresAt` 非负），不触碰状态；再检查时间单调性（`Now >= 当前时间`，否则 `ErrTime`）。
2. **候选执行**：在条目表的浅拷贝（候选状态）上先删除 `ExpiresAt <= Now` 的条目（闭区间），再顺序执行 Put/Touch/Delete；Put/Touch 从候选的 `nextRevision` 起分配 revision。任何 `ErrNotFound` 或最终容量超限（`ErrCapacity`）都会直接丢弃候选，淘汰、时间与 revision 一并回滚；只有全部成功才把候选状态、新时间和 revision 计数器原子提交。非空成功批次 `generation` 恰好加一，空批次不变。

## 所有权

- 所有公开方法通过一把 `sync.Mutex` 串行化，支持并发调用。
- `Snapshot` 与 `Expire` 返回的切片均为新分配的副本，调用方修改不会影响内部状态。
- `Entry`/`Result`/`Snapshot` 均为值类型，按值返回，无共享指针。

## 复杂度

- `Apply`：O(n + m)，n 为当前条目数（候选拷贝与淘汰扫描），m 为批内操作数。
- `Expire`：O(n + k log k)，k 为过期条目数（排序）。
- `Snapshot`：O(n log n)（拷贝并排序）。
- 空间：O(n)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
