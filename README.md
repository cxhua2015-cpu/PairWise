# taskqueue145

并发安全的内存型任务优先队列（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 架构（三层联动）

- `prioritybox.go` — 状态引擎：事务化数据与快照。
- `policy.go` — 策略层：可原子替换的 actor 白名单 + 单批操作数上限。
- `coordinator.go` — 协调层：先授权、再调用引擎，并为成功/拒绝/引擎失败记录连续审计序号。

## 索引与数据结构

- 核心状态为 `map[string]Item`（按 ID 索引），由单把 `sync.Mutex` 保护。
- 不维护堆；`Pop`/`Snapshot` 时按需筛选并排序。排序键：Priority 降序、ReadyAt 升序、ID 升序。
- `Policy` 的 actor 集合存于 `atomic.Value`，`ReplaceActors` 整体原子替换，读取无锁。
- `Coordinator` 用独立互斥锁维护单调递增的审计序号与决策日志。

## 候选事务

`Apply` 先完整结构校验（时间非负、kind 合法、ID 字符集与字节上限），再检查单调时间，然后在**状态副本**上顺序执行 Enqueue/Cancel：Enqueue 分配 revision，Cancel 删除；最终容量只在末尾检查。任一步失败直接丢弃副本——时间、状态、revision 计数器全部自然回滚。成功时一次性提交副本，非空批次 generation 恰好 +1，空批次不变。

## 所有权

- `Snapshot`、`Pop`、`Decisions` 均返回新分配的切片，调用方修改不影响内部状态。
- 策略拒绝发生在引擎调用之前，不读取也不修改核心状态。
- 审计序号从 1 开始连续分配，覆盖成功、拒绝与引擎失败三种结果。

## 复杂度

- `Apply`：O(k·n) 复制 + O(k) 应用（k 为批大小，n 为当前元素数）。
- `Pop`：O(n log n) 排序；`Snapshot`：O(n log n)。
- `Authorize`/`ReplaceActors`：O(1) / O(a)（a 为 actor 数）。
- `Decisions`：O(d) 拷贝（d 为决策数）。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
