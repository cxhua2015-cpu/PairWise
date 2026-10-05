# resourcelease139

并发安全的内存型资源租约表（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 架构

三个生产文件协同工作：

- `heartbeat.go`（状态引擎）：持有条目、单调时间、generation 与 revision 计数器，提供事务性 `Apply`、闭区间 `Expire` 与 `Snapshot`。
- `policy.go`（准入策略）：独立同步的 actor 白名单与单批操作数上限；`ReplaceActors` 原子整体替换白名单，`Authorize` 只做只读检查。
- `coordinator.go`（协调层）：串行化准入流程——先 `Authorize`（拒绝时不触碰核心状态），再委托状态引擎，并为成功、拒绝与引擎失败分配连续递增的审计序号。

## 索引

条目存储为 `map[string]Entry` 哈希索引，按键 O(1) 定位。`Snapshot`/`Expire` 输出按 key 排序，保证确定性且便于 `reflect.DeepEqual` 比较。

## 候选事务

`Apply` 先在无锁状态下完成整批结构校验，再持锁检查时间单调性；随后在候选副本（map 拷贝）上先淘汰 `ExpiresAt <= Now` 的条目，再顺序执行 Put/Touch/Delete 并分配 revision。最终容量检查或任何中途错误都会丢弃候选副本，淘汰、时间与 revision 一并回滚；只有全部成功才一次性提交并令 generation 加一（空批次不变）。

## 所有权

所有返回的切片（`Snapshot.Entries`、`Expire` 结果、`Coordinator.Decisions`）都是新分配的拷贝，不与内部状态共享内存；调用方修改返回值不影响表或审计日志。`Policy` 内部的白名单 map 在替换时整体重建，旧读操作通过 RWMutex 看到的是一致快照。

## 并发与复杂度

- 三层各自使用 `sync.Mutex`/`sync.RWMutex`，所有公开方法可并发调用；协调层互斥保证审计序号连续无空洞。
- `Apply`：O(B + N)，B 为批大小、N 为当前条目数（候选拷贝）。
- `Expire`：O(N + K log K)，K 为到期条目数。
- `Snapshot`：O(N log N)（排序）。
- `Authorize`：O(1)；`ReplaceActors`：O(A)，A 为 actor 数。
- `Decisions`：O(D)，D 为审计条数。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
