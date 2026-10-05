# certificatelease

并发安全的内存型证书租约表（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 索引

- 主索引为 `map[string]Entry`，键即租约名，Put/Touch/Delete/查询均为 O(1) 均摊。
- 未维护堆等过期索引：过期条目在每次 `Apply`/`Expire` 时以闭区间 `ExpiresAt <= Now` 惰性扫描剔除，单次成本 O(n)。该设计在租约表规模（受 `MaxEntries` 上限约束）下足够简单且正确。

## 候选事务

`Apply` 采用候选-提交（copy-on-write）事务模型：

1. 先对整个批次做结构校验（键字符集/长度、kind 合法、时间非负），再检查单调时间。
2. 在候选副本上先淘汰 `ExpiresAt <= Now` 的条目，再顺序执行 Put/Touch/Delete；Put/Touch 各自分配递增 revision。
3. 最终容量超限或任何错误（`ErrNotFound`/`ErrCapacity` 等）发生时直接丢弃候选，淘汰、时间与 revision 一并回滚，已发布状态不受影响。
4. 全部成功后一次性提交；非空成功批次 generation 恰好加一，空批次不变。

## 所有权与并发

- 所有公开方法由单一互斥锁保护，可安全并发调用。
- `Snapshot` 与 `Expire` 返回的切片均为新分配的副本，调用方修改不会影响内部状态；内部也从不保留调用方传入的切片。
- `Entry` 为纯值类型，不存在共享指针。

## 复杂度

- `Apply`：O(n + m)，n 为当前条目数（候选复制与淘汰扫描），m 为批次内 op 数。
- `Expire`：O(n + k log k)，k 为过期条目数（按键排序返回）。
- `Snapshot`：O(n log n)（复制并按键排序）。
- 空间：O(n)，受 `MaxEntries` 约束。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
