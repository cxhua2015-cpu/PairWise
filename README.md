# resourcecatalog131

并发安全的内存型资源目录（Go 1.22+，仅标准库）。分三层实现，均在 `resourcecatalog131` 包内。

## 架构

- **状态引擎**（`servicecatalog.go`）：`Store` 持有事务性数据与快照。
- **策略层**（`policy.go`）：`Policy` 维护可原子替换的 actor 白名单与单批操作数上限。
- **协调层**（`coordinator.go`）：`Coordinator` 串行化准入，先授权再调用引擎，并为成功、拒绝、引擎失败都分配连续审计序号。

## 索引

`Store` 使用 `map[string]entry` 作为主索引，按名称 O(1) 定位记录；`entry` 保存深拷贝的 Value 与 revision。另维护 `totalBytes` 运行计数，避免每次容量检查都全表扫描。`Snapshot` 与 `Result.Changed` 在返回前按名称排序（O(n log n)）。

## 候选事务

`Apply` 分两阶段：

1. **结构校验**：在读取任何状态前校验全部 op 的 kind、名称字符集/长度、Value 长度；失败返回 `ErrInvalidInput`，不加锁、不读状态。
2. **候选执行**：在互斥锁内把当前记录复制到候选 map，按输入顺序应用 Put/Delete（Put 分配连续 revision，Delete 不分配、不存在返回 `ErrNotFound`）。记录数与 Value 总字节容量只在批次末检查（`ErrCapacity`）。任何失败直接丢弃候选，状态、generation、revision 全部不变；成功时整体换入候选，非空批次 generation 恰好加一。

## 所有权

- Put 的 Value 在写入前深拷贝，调用方后续修改不影响内部状态。
- `Get`/`Snapshot`/`Result.Changed` 返回的 Value 均为深拷贝。
- `Coordinator.Decisions` 返回内部审计日志的副本切片，调用方修改不影响内部。
- `Policy.ReplaceActors` 将白名单整体拷贝后原子换入。

## 并发与复杂度

三层各自使用独立互斥锁（`Policy` 用 `RWMutex`），所有公开方法可并发调用。`Coordinator` 在单把锁内完成「授权 → 引擎调用 → 追加审计」，保证审计序号连续无空洞。

- `Apply`：O(k·n) 候选复制 + O(k) 应用 + O(c log c) 排序（k=批大小，n=记录数，c=变更数）。
- `Get`：O(1) 查找 + O(v) 拷贝。
- `Snapshot`：O(n·v + n log n)。
- `Authorize`/`ReplaceActors`：O(1) / O(a)。
- `Decisions`：O(d) 拷贝。

## 验证

```
go test ./...
go test -race ./...
go run ./cmd/demo
```
