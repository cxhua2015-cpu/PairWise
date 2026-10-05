# dedupcache

`dedupcache` 是一个面向支付入口的并发安全、显式时间驱动的内存幂等结果缓存，仅依赖 Go 标准库（Go 1.22+）。核心语义见 `SPEC.md`。

## 事务（原子批量 Apply）

- `Apply` 以批为单位执行：先做整批结构校验（`Now >= 0`、键/令牌字符集与长度、Put/Delete 字段约束、未知 Kind），再检查时间单调性（`Now` 不得回退），然后在克隆出的候选状态上先淘汰 `ExpiresAt <= Now` 的记录，再按输入顺序执行 Put/Delete，最后检查条目数与 Value 总字节容量。
- 任一步失败（`ErrInvalidInput` / `ErrTime` / `ErrConflict` / `ErrNotFound` / `ErrCapacity`）整体回滚：淘汰、操作、时间、revision、generation 均不推进。
- 成功的批次必然推进时间；仅当淘汰或操作真正改变了状态，generation 才加一（纯重放或空批次不加）。revision 只在新建记录时分配一个，`Result.Revision` 为提交后的最新 revision（无则为 0）。

## 到期边界

记录存活区间为 `Now < ExpiresAt`：当 `ExpiresAt <= Now` 时记录被全局淘汰（`Apply` 与 `Get` 都会触发）。因此 `ExpiresAt == Now` 的 Put 属于非法输入，而 `Get(ExpiresAt, key)` 已查不到该记录。

## 重放与冲突

- 同键同令牌的 Put 是重放：原样返回已存记录，提交的 Value/ExpiresAt 不生效，也不分配 revision。
- 同键不同令牌的 Put 返回 `ErrConflict`，整批回滚。
- Delete 要求记录当前存在且令牌、Value、ExpiresAt 字段为空，删除不分配 revision。

## 所有权

所有字节输入（Put 的 Value）在写入时深拷贝；所有输出（`Outcome.Record`、`Get`、`Snapshot`）同样深拷贝返回。调用方修改自己的切片不会影响缓存，反之亦然。`Snapshot` 的记录按 Key 排序。

## 并发与复杂度

所有公开方法通过单个互斥锁串行化，可安全并发调用。设批大小为 B、记录数为 N：

- `Apply`：校验 O(B)，克隆与淘汰 O(N)，执行 O(B)，总体 O(N + B)。
- `Get`：O(N)（全局淘汰扫描）。
- `Snapshot`：O(N log N)（排序）。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
