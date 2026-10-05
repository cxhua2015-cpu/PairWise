# resourceledger147

并发安全的内存型资源计量账本（Go 1.22+，仅标准库）。实现见 `SPEC.md`。

## 架构

- `creditpool.go`（状态引擎）：持有账户表、generation 与 revision 计数器，执行原子批次并产出快照。
- `policy.go`（准入策略）：独立同步的 actor 白名单与单批操作数上限，白名单可原子整体替换。
- `coordinator.go`（协调层）：先授权再调用状态引擎，为成功、拒绝与引擎失败分配连续审计序号。

## 索引

状态引擎使用 `map[string]Account` 作为主索引，按名称 O(1) 定位账户。`Top` 与 `Snapshot` 在读取时物化并排序，不维护辅助有序结构，以换取写入路径的常数复杂度。

## 候选事务

`Apply` 先对整批做纯结构校验（kind、名称字符集与字节上限），不触碰状态；随后在写锁内把当前账户表复制为候选副本，按输入顺序在副本上执行 Add/Set/Delete：Add/Set 消耗连续 revision，加法前用边界比较检测 int64 溢出，结果执行 `MaxAbsValue` 绝对值上限，批次末才检查最终账户容量。任一步失败直接丢弃候选副本，实现整体回滚；全部成功才一次性换入候选副本，非空批次 generation 恰好加一。

## 所有权

所有公开方法返回的切片（`Result.Changed`、`Top`、`Snapshot.Accounts`、`Coordinator.Decisions`）都是新分配的副本，不与内部存储共享底层数组；调用方修改返回值不影响账本。`Policy.ReplaceActors` 将白名单复制进新 map 后原子换入，调用方后续修改入参切片不影响策略。

## 并发与复杂度

- 状态引擎：`sync.RWMutex`；`Apply` 为 O(k + n)（k 为操作数，n 为账户数，候选复制），`Top`/`Snapshot` 为 O(n log n)。
- 策略层：独立 `sync.RWMutex`，`Authorize` O(1)，`ReplaceActors` O(a)（a 为 actor 数）。
- 协调层：独立 `sync.Mutex` 仅保护审计序号与日志追加，`Decisions` O(d) 复制（d 为日志条数）；策略拒绝发生在读取核心状态之前。

## 验证

```
go test ./...
go test -race ./...
go run ./cmd/demo
```
