# workerlease

并发安全的内存型工作节点租约表（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 索引

- 主索引为 `map[string]Entry`（按 key 哈希），Put/Touch/Delete 均为 O(1) 定位。
- 无堆/有序索引：`Apply` 与 `Expire` 的淘汰采用全表扫描，按 `ExpiresAt <= Now` 闭区间判定。
- `Snapshot` 与 `Expire` 返回的条目按 key 字典序排列（规范顺序），保证输出确定性。

## 候选事务

`Apply` 分两阶段执行：

1. **校验**：先对整个批次做结构校验（kind、键字符集与字节上限、ExpiresAt 非负），再检查时间单调性（`Now < 当前时间` 返回 `ErrTime`）。任一失败不触碰状态。
2. **候选提交**：在候选副本上先删除 `ExpiresAt <= Now` 的条目，再顺序执行 Put/Touch/Delete（Put/Touch 各分配一个递增 revision），最后做容量检查。任何错误（`ErrNotFound`/`ErrCapacity`）直接丢弃候选，淘汰、时间与 revision 一并回滚；成功才整体提交，非空批次 generation 恰好加一，空批次完全不变。

## 所有权

- 表内部只持有不可变语义的 `Entry` 值；`Snapshot`/`Expire` 返回的切片均为新建副本，调用方修改不影响表内状态。
- 所有公开方法通过单一 `sync.Mutex` 串行化，支持任意并发调用；无锁外共享状态。

## 复杂度

设 n 为表内条目数，b 为批次操作数：

- `Apply`：O(n + b)（候选复制 + 顺序执行），校验 O(b·k)，k 为键长。
- `Expire`：O(n + e·log e)，e 为过期条目数（排序输出）。
- `Snapshot`：O(n log n)（规范顺序排序）。
- 空间：O(n)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
