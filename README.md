# resourceledger132

并发安全的内存型“资源计量账本”，Go 1.22+，仅依赖标准库。语义详见 `SPEC.md`。

## 架构（三层联动）

- `creditpool.go`（状态引擎）：持有事务性账户数据、generation 与 revision 分配，提供 `Apply` / `Top` / `Snapshot`。
- `policy.go`（策略层）：独立同步的准入配置——可原子替换的 actor 白名单与单批操作数上限。
- `coordinator.go`（协调层）：先 `Authorize` 再委托状态引擎，为成功、拒绝、引擎失败三类结果分配连续审计序号。

## 索引

状态引擎使用 `map[string]Account` 作为主索引（按名称 O(1) 定位）。`Top` 与 `Snapshot` 在读取时即时排序，不维护额外的有序索引，以换取写入路径的简单与线性。

## 候选事务

`Apply` 先做完整结构校验（kind、名称字符集与字节上限、绝对值上限），不读取状态；随后在账户映射的**候选副本**上按输入顺序执行 Add/Set/Delete，revision 在候选上连续预分配。算术前先检测 int64 溢出（含 `MinInt64` 取绝对值的退化情形），账户容量上限仅在批次末检查。任一步失败直接丢弃候选，实现零成本整体回滚；成功则一次性替换内部映射，非空批次 generation 恰好 +1。

## 所有权

所有公开方法返回的切片（`Result.Changed`、`Top`、`Snapshot.Accounts`、`Coordinator.Decisions`）均为新分配的副本，不与内部存储共享底层数组；调用方修改返回值不会影响账本或审计日志。`Policy.ReplaceActors` 同样复制白名单后再原子替换。

## 并发与复杂度

- 状态引擎：`sync.RWMutex`；`Apply` 持写锁，`Top`/`Snapshot` 持读锁。
- 策略层：独立 `RWMutex`，`Authorize` 读锁，`ReplaceActors` 写锁整体换 map。
- 协调层：独立 `Mutex` 仅保护审计序号与日志追加；策略拒绝发生在任何核心状态读写之前。
- 复杂度：`Apply` O(B·A)（B 为批操作数，A 为当前账户数，候选复制）；`Top`/`Snapshot` O(A log A)；`Authorize` O(1)；`Decisions` O(D)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
