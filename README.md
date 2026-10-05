# resourcelease084

并发安全的内存型资源租约表（Go 1.22+，仅标准库）。表使用显式非负单调时间：
`Apply`/`Expire` 携带调用方提供的时间戳，任何小于当前表时间的调用都会以
`ErrTime` 失败且不产生任何副作用。

## 索引

- 主索引为 `map[string]Entry`，键即租约名，查找 / 插入 / 删除均为 O(1)。
- 不维护额外的按过期时间排序的索引；淘汰（`Apply` 前置淘汰与 `Expire`）
  通过一次全表扫描完成，代价见下文复杂度。
- 所有公开方法共用一把 `sync.Mutex`，整个批次在临界区内原子完成，
  因此并发调用者永远观察不到中间状态。

## 候选事务

`Apply` 采用“候选状态 + 提交”的事务模型：

1. 先对整批 Op 做完整结构校验（kind 合法、键为非空 ASCII 小写字母 / 数字 /
   `-` / `_` 且不超过 `MaxKeyBytes`、`ExpiresAt` 非负），再检查时间单调性；
   任一失败直接返回，不触碰状态。
2. 将当前条目克隆到候选 map，并在克隆时丢弃 `ExpiresAt <= Now` 的条目
   （闭区间边界，`Expire` 使用同一边界）。
3. 在候选上顺序执行 Put / Touch / Delete；Put 与 Touch 各自分配一个
   单调递增的 revision，Touch / Delete 命中不存在的键返回 `ErrNotFound`。
4. 全部执行完后检查最终容量 `<= MaxEntries`，超限返回 `ErrCapacity`。
5. 只有全部成功才把候选 map、时间、revision 计数器一次性提交，
   且非空成功批次的 generation 恰好加一；空批次不改变任何状态。

由于所有变更都发生在候选副本上，任何错误（含容量失败）天然连同淘汰、
时间和 revision 一起回滚，无需显式补偿。

## 所有权

- `Snapshot` 与 `Expire` 返回的切片及其中的 `Entry` 都是新分配的副本，
  调用方可自由修改，不影响表内状态；条目按键排序以保证确定性输出。
- 传入的 `Batch`/`Op` 仅按值读取，实现不保留任何调用方切片的引用。

## 复杂度

设批次含 `b` 个操作、表内 `n` 个条目：

- `Apply`：校验 O(b)，克隆 + 淘汰 O(n)，执行 O(b)，合计 O(n + b) 时间、
  O(n) 额外空间。
- `Expire`：O(n) 时间，O(k) 额外空间（k 为被淘汰条目数）。
- `Snapshot`：O(n log n)（排序），O(n) 额外空间。
- `New`：O(1)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
