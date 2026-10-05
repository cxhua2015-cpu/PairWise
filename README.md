# taskqueue135

并发安全的内存型任务优先队列（Go 1.22+，仅标准库）。规范见 `SPEC.md`。

## 架构（三层联动）

- **状态引擎 `prioritybox.go`**：`Queue` 持有事务性数据与快照。`Apply` 先做整批结构校验（不读状态），再顺序执行 Enqueue/Cancel，末尾统一检查容量；任何失败通过 undo 日志回滚时间、状态与 revision。`Pop` 原子选择并删除就绪任务。单把 `sync.Mutex` 串行化所有读写。
- **策略层 `policy.go`**：`Policy` 独立持锁（`sync.RWMutex`），维护可原子替换的 actor 白名单与单批操作数上限。`ReplaceActors` 先完整校验再整体换入，失败保留旧名单；`Authorize` 只读。
- **协调层 `coordinator.go`**：`Coordinator` 先 `Authorize`（拒绝时不触碰核心状态），再委托 `Queue.Apply`，并为成功、拒绝、引擎失败三类结果分配连续审计序号。`Decisions()` 返回拷贝，不别名内部存储。

## 索引与候选事务

- 主索引为 `map[string]Item`（按 ID），Enqueue/Cancel 为 O(1) 查找。
- `Pop` 全量扫描过滤 `ReadyAt <= now` 的候选，按 Priority 降序、ReadyAt 升序、ID 升序排序后截取 limit 并删除：O(n log n)。
- `Apply` 的候选事务在 undo 日志上执行：Enqueue 记录待删 ID，Cancel 记录待恢复的 `Item` 副本，revision 起点单独保存，回滚为 O(ops)。

## 所有权

- `Snapshot`、`Pop`、`Decisions` 均返回新分配的切片，调用方修改不影响内部状态。
- 白名单替换采用“构建新 map → 加锁整体换入”，读路径无部分可见状态。

## 复杂度

- `Apply`：O(ops) + 末尾容量检查 O(1)。
- `Pop`：O(n log n)（n 为当前任务数）。
- `Snapshot`：O(n log n)（按 ID 排序保证确定性输出）。
- `Authorize`：O(1)；`ReplaceActors`：O(actors)；`Decisions`：O(d)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
