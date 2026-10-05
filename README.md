# resourcecatalog131

并发安全的内存型资源目录（Go 1.22+，仅标准库）。原子批次按输入顺序执行 Put/Delete，Put 分配连续 revision，Delete 不分配；失败回滚全部状态、generation 与 revision。

## 架构（三层联动）

- `servicecatalog.go` — 状态引擎 `Store`：持有事务性数据与快照。
- `policy.go` — 准入策略 `Policy`：独立同步、可原子替换的 actor 白名单 + 单批操作数上限。
- `coordinator.go` — 协调层 `Coordinator`：先授权再调用引擎，为成功/拒绝/引擎失败分配连续审计序号。

## 索引

`Store` 使用 `map[string]Record` 作为主索引（按名称 O(1) 查找），另维护 `totalBytes` 运行合计用于容量判定；无次级排序索引，`Get`/`Snapshot` 时按需排序输出。

## 候选事务（candidate transaction）

`Apply` 分两阶段：

1. **完整结构校验**（不加锁、不读状态）：kind 合法、名称字符集/长度、Value 长度上限。
2. **候选应用**：在互斥锁内把整批操作应用到临时 `touched` 增量表与临时计数器上；Delete 未知名称立即返回 `ErrNotFound`。记录数与 Value 总字节容量只在批次末检查，超限返回 `ErrCapacity`。任何失败都不触碰真实状态，天然回滚；全部通过后才一次性提交并推进 `generation`（非空批次 +1）与 `revision`。

## 所有权

- 写入时深拷贝 `Value`；`Get`/`Snapshot`/`Result.Changed` 返回的切片均为新分配副本，调用方修改不影响内部状态。
- `Snapshot.Records` 与 `Result.Changed` 按名称排序。
- `Coordinator.Decisions()` 返回内部审计日志的拷贝切片，不别名内部存储。
- `Policy.ReplaceActors` 先构建新白名单再在锁内整体替换，授权读取与替换互不影响。

## 并发与复杂度

三层各自持有独立互斥锁（`Store` 用 `sync.Mutex`，`Policy` 用 `sync.RWMutex`，`Coordinator` 审计日志用 `sync.Mutex`），所有公开方法可并发调用。

- `Apply`：O(n) 校验 + O(n) 候选应用 + O(k log k) 排序变更集（n 为批内操作数，k 为涉及的不同名称数）。
- `Get`：O(1) 平均 + O(v) 拷贝（v 为 Value 长度）。
- `Snapshot`：O(m log m)（m 为记录数）。
- `Authorize`：O(1)；`Decisions`：O(d)（d 为审计条数）。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
