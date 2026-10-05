# taskqueue145

并发安全的内存型任务优先队列（Go 1.22+，仅标准库）。规范见 `SPEC.md`。

## 架构（三层联动）

- `taskqueue145/prioritybox.go` — 状态引擎：事务化 Apply/Pop、快照与回滚。
- `taskqueue145/policy.go` — 策略层：独立互斥锁保护的可原子替换 actor 白名单与单批操作数上限，`Authorize` 不触碰核心状态。
- `taskqueue145/coordinator.go` — 协调层：先授权再委托状态引擎，为成功、拒绝与引擎失败分配连续审计序号；`Decisions()` 返回拷贝，与内部存储隔离。

## 索引与候选事务

- 核心状态为 `map[string]Item`（按 ID 索引，Enqueue/Cancel 为 O(1)）。
- Apply 先完整结构校验（kind、ID 字符集与字节上限、ReadyAt 非负），再按序执行；每个 op 记录一条 undo（插入记删除、删除记原值），任一失败或最终容量超限即逆序回滚，同时恢复 `now`、`nextRevision`，generation 不变。
- Pop/快照时对候选集（ReadyAt <= now）按 Priority 降序、ReadyAt 升序、ID 升序排序；Pop 原子删除所选项并推进时间。

## 所有权

- `Snapshot().Items`、`Pop` 结果与 `Decisions()` 均为新建切片，调用方修改不影响内部状态。
- 策略白名单通过整体替换 map 实现原子更新，读写各自持锁。

## 复杂度

- `Apply`：O(k)，k 为批内操作数（不含回滚外的排序）。
- `Pop`：O(n log n)，n 为就绪候选数；删除 O(k)。
- `Snapshot`：O(n log n)。
- `Authorize` / `ReplaceActors`：O(1) / O(a)，a 为 actor 数。

## 验证

```
go test ./...
go test -race ./...
go run ./cmd/demo
```
