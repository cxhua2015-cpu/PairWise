# resourcelease109

并发安全的内存型“资源租约表”，Go 1.22+，仅依赖标准库。语义详见 `SPEC.md`。

## 索引

- 主索引为 `map[string]Entry`，键即租约键，查找 / 插入 / 删除均为 O(1) 均摊。
- 不维护额外的堆或按过期时间排序的索引；过期淘汰采用全量扫描（见下）。

## 候选事务

`Apply` 采用候选状态（copy-on-write）事务模型：

1. 先对整个批次做结构校验（kind、键字符集与字节上限、非负 ExpiresAt），再检查时间单调性。
2. 在候选副本上先删除 `ExpiresAt <= Now` 的条目（闭区间），再顺序执行 Put/Touch/Delete；Put/Touch 各分配一个递增 revision。
3. 任何错误（`ErrNotFound`、最终 `ErrCapacity` 等）直接丢弃候选，淘汰、时间与 revision 一并回滚，已持有状态不受影响。
4. 全部成功才一次性提交：替换索引、推进 `Now`、revision，并将 generation 加一（空批次完全不变）。

`Expire(now)` 使用相同的闭区间边界删除并返回被淘汰条目，同时推进单调时间。

## 所有权

- 所有公开方法通过单一互斥锁串行化，可任意并发调用。
- `Snapshot` 与 `Expire` 返回的切片均为新分配的副本，调用方可自由修改，不影响表内状态。

## 复杂度

- `Apply`：O(n + m)，n 为当前条目数（候选复制 + 淘汰扫描），m 为批次内操作数。
- `Expire`：O(n + k log k)，k 为被淘汰条目数（结果按键排序）。
- `Snapshot`：O(n log n)（返回按键排序的规范顺序）。
- 空间：O(n)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
