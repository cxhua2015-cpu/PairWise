# resourceledger137

并发安全的内存型资源计量账本（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 架构

三层协作，均并发安全：

- `creditpool.go`（状态引擎）：`Ledger` 以 `map[string]Account` 持有账户，单把 `sync.Mutex` 串行化 `Apply`/`Top`/`Snapshot`。
- `policy.go`（策略层）：`Policy` 通过 `atomic.Pointer` 持有不可变配置（actor 白名单 + 单批操作数上限），`ReplaceActors` 原子整体替换，读取无锁。
- `coordinator.go`（协调层）：`Coordinator` 先 `Authorize` 再委托引擎；审计日志由独立互斥锁保护，成功、拒绝、引擎失败都分配连续序号（从 1 开始）。

## 索引与候选事务

- 主索引为 `map[string]Account`，按名称 O(1) 定位；无有序索引，`Top`/`Snapshot` 现取现排。
- `Apply` 先在候选副本（clone-on-write 的 map）上按输入顺序执行全部操作，仅在批次末检查账户容量；全部成功才整体替换主索引，任一步失败直接丢弃候选，实现整体回滚，无需反向补偿。
- Add/Set 在候选上分配连续 revision；溢出在加法前用 `math.MaxInt64/MinInt64` 边界检测，绝对值上限逐操作检查。

## 所有权

- 返回的 `Result.Changed`、`Top`、`Snapshot.Accounts`、`Coordinator.Decisions` 均为新分配切片，`Account`/`Decision` 为纯值类型，调用方修改不影响内部状态。
- 策略配置不可变，替换即换指针，旧配置不被改写。

## 复杂度

- `Apply`：O(k·n) 候选克隆 + O(k) 执行（k 为批内操作数，n 为账户数）。
- `Top`：O(n log n)；`Snapshot`：O(n log n)（按名称排序）。
- `Authorize`/`ReplaceActors`：O(1) / O(白名单大小)。
- `Decisions`：O(d) 拷贝（d 为审计条数）。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
