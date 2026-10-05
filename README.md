# taskqueue150

并发安全的内存型任务优先队列（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 架构（三层联动）

- **状态引擎 `prioritybox.go`**：`Queue` 持有全部事务性数据。单把 `sync.Mutex` 串行化
  `Apply`/`Pop`/`Snapshot`，因此所有公开方法并发安全。
- **策略层 `policy.go`**：`Policy` 独立同步，不依赖队列锁。actor 白名单存放于
  `atomic.Value`，`ReplaceActors` 先完整校验再一次性原子替换，读者无锁；
  单批操作数上限 `maxOps` 不可变。`Authorize` 纯读取，拒绝时不触碰核心状态。
- **协调层 `coordinator.go`**：`Coordinator` 用自己的互斥锁串行化准入流程：
  先 `Policy.Authorize(actor, len(ops))`，通过后才委托 `Queue.Apply`；
  成功、策略拒绝、引擎失败三类结果都追加一条审计 `Decision`，序号从 1 连续递增。
  `Decisions()` 返回拷贝，调用方修改不影响内部日志。

## 索引与候选事务

- 主索引为 `map[string]Item`（按 ID），Enqueue/Cancel 查重与删除均为 O(1)。
- `Apply` 采用**候选事务**：先完整结构校验（时间非负且单调、kind 合法、ID 字符集与
  字节上限），再顺序应用并记录 undo 日志；任一步失败或最终容量超限时按逆序回滚
  状态与 revision，时间不前进。容量只在批次末尾检查，因此同批 “Cancel 再 Enqueue”
  可以成功。非空成功批次 generation 恰好 +1，revision 仅 Enqueue 时分配。
- `Pop` 在锁内过滤 `ReadyAt <= now` 的候选，按 Priority 降序、ReadyAt 升序、ID 升序
  排序后截取并原子删除。

## 所有权

`Snapshot.Items`、`Pop` 与 `Coordinator.Decisions` 均返回独立拷贝/新建切片，
不与内部存储共享底层数组；`Policy` 白名单替换时构建新 map，旧读者不受影响。

## 复杂度

- `Apply`：O(k)，k 为批内操作数（回滚同为 O(k)）。
- `Pop`：O(n log n)，n 为当前就绪候选数。
- `Snapshot`：O(n log n)（按 ID 排序保证确定性输出）。
- `Authorize`/`ReplaceActors`：O(1) / O(a)，a 为 actor 数。
- `Coordinator.Apply`：O(k) 加一次决策追加；`Decisions`：O(d) 拷贝。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
