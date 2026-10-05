# resourcelease194

并发安全的内存型资源租约表（Go 1.22+，仅标准库）。表使用显式非负单调时间，
通过 `Apply` 批量提交 Put/Touch/Delete，通过 `Expire` 主动驱逐到期租约。

## 设计说明

- **索引**：主索引为 `map[string]Entry`（键 → 租约），按键 O(1) 定位；
  淘汰与快照按键排序后输出，保证确定性。未维护额外的堆索引，因为容量上限
  通常较小，且闭区间淘汰只在批次边界发生。
- **候选事务**：`Apply` 先对整个批次做结构校验（未知 kind、非法键直接返回
  `ErrInvalidInput`），再检查时间单调性（`ErrTime`）。随后在候选状态（主索引
  的副本）上先删除 `ExpiresAt <= Now` 的条目，再顺序执行 Put/Touch/Delete，
  Put/Touch 各分配一个递增 revision。任何错误（`ErrNotFound`、最终容量
  `ErrCapacity`）都会丢弃候选，连同淘汰、时间和 revision 一起回滚，原表不变。
  非空成功批次 generation 只增加一次，空批次不变。
- **所有权**：`Snapshot` 与 `Expire` 返回的切片均为新建副本，调用方修改不会
  影响内部状态；`Entry` 为纯值类型，无共享指针。
- **并发**：所有公开方法由单把 `sync.Mutex` 串行化，可安全并发调用。
- **复杂度**：结构校验 O(批次键长总和)；`Apply` 为 O(N + B)，其中 N 为当前
  条目数（候选复制与淘汰扫描）、B 为批次数；`Expire` O(N)；`Snapshot`
  O(N log N)（排序）；空间 O(N)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
