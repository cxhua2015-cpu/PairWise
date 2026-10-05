# resourceledger142

并发安全的内存型资源计量账本（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 架构

包内三个生产文件协同工作：

- `creditpool.go` — 状态引擎 `Ledger`：原子批次 Apply、Top、Snapshot。
- `policy.go` — 准入策略 `Policy`：actor 白名单 + 单批操作数上限。
- `coordinator.go` — 协调层 `Coordinator`：先授权再调用引擎，并记录单调递增的审计序号。

## 索引与数据结构

- 账户存储为 `map[string]Account`（按名 O(1) 存取），不维护有序索引。
- `Top` 每次调用对当前账户做全量排序（值降序、名升序），`Snapshot` 按名升序排序；
  两者都在读锁内拷贝后返回，调用方拿到的切片与内部状态完全隔离。
- revision 由单调计数器 `nextRevision`（从 1 开始）分配，Add/Set 在批次内按输入顺序连续取号。

## 候选事务（candidate transaction）

`Apply` 分两阶段：

1. **结构校验**：先完整校验所有 Op 的 kind 与名称合法性，不读取任何状态；
   未知 kind 或非法名称返回 `ErrInvalidInput`。
2. **候选执行**：在账户 map 的副本上按顺序执行 Add/Set/Delete。算术前先检测
   int64 溢出并执行 `MaxAbsValue` 绝对值上限（`ErrValue`）；Delete 缺失账户返回
   `ErrNotFound`；最终账户容量仅在批次末检查（`ErrCapacity`）。任何失败直接丢弃
   副本——账户、revision 计数器、generation 全部回滚。成功时一次性提交副本，
   非空批次 generation 恰好加一，空批次不变。

## 所有权

- `Result.Changed`、`Top`、`Snapshot`、`Coordinator.Decisions` 返回的切片均为新分配
  或拷贝，绝不别名内部存储，调用方可自由修改。
- `Policy` 的配置（白名单 + 操作数上限）是不可变结构体，`ReplaceActors` 通过
  `atomic.Pointer` 单次交换整体替换，读者无需加锁即可看到一致快照。
- `Coordinator` 用互斥锁串行化「授权 → 引擎调用 → 追加审计」，保证成功、拒绝与
  引擎失败都获得连续无间隙的审计序号（从 1 开始）。

## 并发安全

- `Ledger`：`sync.RWMutex`，Apply 写锁，Top/Snapshot 读锁。
- `Policy`：原子指针，无锁读。
- `Coordinator`：`sync.Mutex` 串行化准入与审计。
- 三层均可被任意数量的 goroutine 并发调用（`go test -race ./...` 验证）。

## 复杂度

| 操作 | 时间 | 额外空间 |
| --- | --- | --- |
| `Apply`（k 个 Op，n 个账户） | O(n + k)（复制候选 map） | O(n) |
| `Top(m)` | O(n log n) | O(n) |
| `Snapshot` | O(n log n) | O(n) |
| `Authorize` | O(1) | O(1) |
| `ReplaceActors`（a 个 actor） | O(a) | O(a) |
| `Coordinator.Apply` | 授权 O(1) + 引擎 Apply | O(1) 审计摊还 |
| `Decisions`（d 条） | O(d) | O(d) |

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
