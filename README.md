# expirytable354

并发安全的内存型“到期状态表”，仅依赖 Go 标准库（Go 1.22+）。语义详见 `SPEC.md`。

## 索引

- 主索引为 `map[string]Entry`（按 Key 哈希），Put/Touch/Delete 与过期扫描均直接作用于该映射。
- 不维护额外的堆或有序索引；过期淘汰采用全表扫描，按 `ExpiresAt <= Now` 闭区间过滤。
- `Snapshot` 与 `Expire` 返回的条目按 Key 排序，保证输出确定性。

## 候选事务

`Apply` 采用候选状态（copy-on-write）事务模型：

1. 先对整个批次做结构校验（kind、键字符集与长度、非负 ExpiresAt），再检查时间单调性。
2. 在真实状态的副本上先淘汰 `ExpiresAt <= Now` 的条目，再顺序执行 Put/Touch/Delete；Put/Touch 从候选 revision 计数器分配版本号。
3. 最终容量超限或任一操作失败（如 `ErrNotFound`）时直接丢弃候选，淘汰、时间与 revision 一并回滚，真实状态零副作用。
4. 全部成功才一次性提交：替换映射、推进 `now` 与 `nextRevision`，非空批次 `generation` 恰好加一，空批次不变。

## 所有权

- 所有公开方法通过单一 `sync.Mutex` 串行化，支持并发调用。
- 表独占内部映射；`Snapshot` 与 `Expire` 返回的切片均为新建副本，调用方修改返回值不影响内部状态。
- 时间为显式非负单调值：小于当前 `now`（或为负）返回 `ErrTime`，相等允许。

## 复杂度

设 `n` 为当前条目数、`m` 为批次操作数：

- `Apply`：结构校验 O(m)，候选复制 O(n)，执行 O(m)，提交 O(1)；总 O(n + m)，额外空间 O(n)。
- `Expire`：O(n) 扫描 + O(k log k) 排序（k 为到期条目数）。
- `Snapshot`：O(n log n)（复制并排序）。
- `New`：O(1)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
