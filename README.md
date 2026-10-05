# locklease

并发安全的内存型“锁租约表”（Go 1.22+，仅标准库）。表使用显式非负单调时间，
租约以 `ExpiresAt` 表示，过期边界为闭区间：`ExpiresAt <= Now` 即视为过期。

## 设计

- **索引**：条目存放在 `map[string]Entry` 中，按 key 直接寻址；`Snapshot`/`Expire`
  返回的切片按 key 排序，保证确定性输出。未维护额外的过期堆——容量上限较小且
  过期扫描只在批处理/过期调用时发生，线性扫描足够且实现更简单。
- **候选事务**：`Apply` 先对整个批次做纯结构校验（时间非负、kind 合法、key 字符集
  与长度、ExpiresAt 非负），再检查时间单调性；随后在候选副本上先淘汰
  `ExpiresAt <= Now` 的条目，再顺序执行 Put/Touch/Delete，Put/Touch 分配递增
  revision。全部成功且最终容量不超限时才一次性提交（时间、淘汰、revision、
  generation 一起生效）；任何错误（`ErrTime`/`ErrNotFound`/`ErrCapacity`）都完整
  回滚，状态与调用前逐字节一致。非空成功批次 generation 恰好 +1，空批次不变。
- **所有权**：`Table` 内部状态绝不外泄——`Snapshot` 与 `Expire` 返回的切片均为新
  分配的副本，调用方修改返回值不影响表；`Apply` 的入参在锁外只读校验，锁内只从
  入参拷贝标量字段。所有公开方法通过单个 `sync.Mutex` 串行化，支持并发调用。
- **复杂度**：结构校验 O(批次大小)；候选构建 O(n)；顺序执行 O(批次大小)；容量
  检查 O(1)。`Apply`/`Expire` 总体 O(n + 批次大小)，`Snapshot` O(n log n)（排序），
  空间 O(n)。n 为当前条目数。

## 错误

`ErrInvalidOptions`（非正容量/长度上限）、`ErrInvalidInput`（结构非法）、
`ErrTime`（时间回退）、`ErrNotFound`（Touch/Delete 缺失键）、`ErrCapacity`
（批次结束后条目数超限，已回滚）。

## 验证

```sh
go test ./...        # 契约 + 边界 + 并发测试
go test -race ./...
go run ./cmd/demo
```
