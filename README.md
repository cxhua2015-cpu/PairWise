# expirytable329

并发安全的内存型“到期状态表 329”（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

- **索引**：条目主存于 `map[string]entry`（O(1) 按键查找）；另维护一个按
  `(ExpiresAt, Key)` 排序的最小堆作为到期索引。Put/Touch 会向堆中追加新版本的
  索引项，旧版本成为惰性垃圾，弹出时按 revision 与主表比对并丢弃，无需原地更新堆。
- **候选事务**：`Apply` 先在持锁状态下对整个批次做完整结构校验（kind、键字符集与
  字节上限、非负 ExpiresAt），再检查时间单调性（`Now < 0` 或回退即 `ErrTime`）。
  随后在候选状态（主表与堆索引的副本）上先淘汰 `ExpiresAt <= Now` 的条目，再顺序
  执行 Put/Touch/Delete，最后做容量判定。任一步失败（`ErrNotFound`/`ErrCapacity`）
  直接丢弃候选，淘汰、时间、revision 与 generation 一并回滚；成功才整体提交。
  非空成功批次 generation 恰好加一，空批次不变。
- **所有权**：`Snapshot` 与 `Expire` 返回的切片均为新分配的副本，调用方修改不会影响
  表内状态；表也不保留调用方传入的切片。所有公开方法由单一互斥锁保护，可并发调用。
- **复杂度**：设批次含 m 个操作、淘汰 k 条、表内 n 条。结构校验 O(m·L)（L 为键长）；
  候选复制 O(n)；每个 Put/Touch/Delete 为 O(log n)（堆操作）或 O(1)（Delete）；
  淘汰 O(k log n)；容量判定 O(1)。`Expire` 为 O(k log n)，`Snapshot` 为 O(n log n)
  （按键排序输出）。空间 O(n + 堆中惰性项)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
