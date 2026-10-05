# taskqueue140

并发安全的内存型任务优先队列（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 三层架构

- **状态引擎（`taskqueue140/prioritybox.go`）**：`Queue` 持有全部事务性数据。
  单把 `sync.Mutex` 串行化所有公开方法；`Apply` 先在无锁状态下做完整结构校验
  （kind、ID 字符集与字节上限、非负时间），再在锁内做单调时间检查，随后在
  **候选事务**（克隆的 items map 与本地 revision 计数）上顺序执行 Enqueue/Cancel，
  最终容量只在末尾检查；任何失败直接丢弃候选，时间、状态、revision 全部回滚，
  成功才一次性提交。非空成功批次 generation 只增一次，空批次不变。
- **策略层（`taskqueue140/policy.go`）**：`Policy` 用独立的 `sync.RWMutex` 维护
  actor 白名单（map 索引，O(1) 查找）与单批操作数上限。`ReplaceActors` 先完整
  校验再整体换入新 set，实现原子替换；`Authorize` 只读策略，绝不触碰核心状态。
- **协调层（`taskqueue140/coordinator.go`）**：`Coordinator` 用自己的 mutex 串行
  准入：先 `Authorize`，拒绝则直接记录审计并返回 `ErrDenied`（不读不写核心状态）；
  通过则委托 `Queue.Apply`。无论成功、拒绝还是引擎失败，都追加一条审计
  `Decision`，序号从 1 开始连续单调递增。

## 索引与复杂度

- 核心索引：`map[string]Item`，Enqueue/Cancel/查找均摊 O(1)。
- `Apply`：O(k·n) 克隆（k 为当前任务数，n 为批大小）+ O(n) 应用。
- `Pop`：O(m log m)，m 为就绪任务数；按 Priority 降序、ReadyAt 升序、ID 升序
  排序后取前 limit 个并原子删除。
- `Snapshot`：O(k log k)，按 ID 排序输出，保证确定性。
- `Authorize`：O(1)；`Decisions`：O(d) 拷贝。

## 所有权隔离

`Snapshot`、`Pop`、`Decisions` 均返回新分配的切片（元素为值类型），调用方修改
返回值不会影响内部状态；`ReplaceActors` 校验失败时保留旧白名单。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
