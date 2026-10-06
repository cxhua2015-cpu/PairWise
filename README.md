# expirytable214

并发安全的内存型“到期状态表”，使用显式非负单调时间，仅依赖标准库（Go 1.22+）。

## 语义

- `New(Options)` 校验容量与键长上限必须为正，否则返回 `ErrInvalidOptions`。
- `Apply(Batch)` 先对整个批次做结构校验（未知 kind、非法键、负 ExpiresAt → `ErrInvalidInput`），再检查时间单调性（`Now < 当前 Now` → `ErrTime`）。
- 通过校验后在候选状态上先淘汰 `ExpiresAt <= Now` 的条目（闭区间），再顺序执行 Put/Touch/Delete；Put/Touch 各分配一个递增 revision，Touch/Delete 缺失键返回 `ErrNotFound`。
- 批次结束后条目数超过 `MaxEntries` 返回 `ErrCapacity`。任何错误都会把淘汰、时间、revision、generation 一起回滚。
- 非空成功批次 generation 只增加一次；空批次不改变 generation。
- `Expire(now)` 使用相同闭区间边界淘汰并返回被删条目，同时推进时间；时间倒退返回 `ErrTime`。
- 键仅允许非空 ASCII 小写字母、数字、连字符、下划线，且不超过 `MaxKeyBytes`。

## 索引

内部使用 `map[string]Entry` 作为唯一索引，按键精确查找 O(1)。不维护额外的按过期时间排序的索引——淘汰为全表扫描，换取写入路径的简单与无锁竞争的单互斥量设计。

## 候选事务

`Apply` 在持有互斥量期间把存活条目克隆到候选 map，在其上执行淘汰与全部操作，最后做容量检查；全部成功才一次性替换内部状态并提交时间/revision/generation。任何一步失败直接返回，内部状态保持原样，实现原子回滚。

## 所有权与并发

所有公开方法由单个 `sync.Mutex` 保护，可并发调用。`Snapshot` 与 `Expire` 返回的切片均为新分配的副本，调用方修改不会影响内部状态；`Entry` 为纯值类型，无共享指针。

## 复杂度

- `Apply`：O(n + m)，n 为存活条目数（克隆与淘汰扫描），m 为批次操作数。
- `Expire`：O(n + k log k)，k 为被淘汰条目数（结果按键排序）。
- `Snapshot`：O(n log n)（返回按键排序的副本）。
- 空间：O(n)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
