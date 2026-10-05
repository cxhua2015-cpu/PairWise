# resourceledger137

并发安全的内存型资源计量账本（Go 1.22+，仅标准库）。规则详见 `SPEC.md`。

## 架构

三个生产文件协同工作：

- `resourceledger137/creditpool.go` — 状态引擎：原子批次、revision/generation、Top/Snapshot。
- `resourceledger137/policy.go` — 准入策略：可原子替换的 actor 白名单 + 单批操作数上限。
- `resourceledger137/coordinator.go` — 协调层：先授权再调用引擎，为成功/拒绝/引擎失败分配连续审计序号。

## 设计要点

**索引**：核心状态为 `map[string]Account`，按名称 O(1) 定位；`Top` 与 `Snapshot` 在读取时拷贝后排序（Top 按值降序、名称升序；Snapshot 按名称升序），不维护持久有序结构。

**候选事务**：`Apply` 先完整结构校验（kind、名称字符集与字节上限），再在账户 map 的副本上按输入顺序执行 Add/Set/Delete；int64 溢出在算术前检测，绝对值上限逐操作执行，账户容量仅在批次末检查。任一步失败直接丢弃副本实现整体回滚；成功才一次性替换 map、generation +1、推进连续 revision。空批次不改变 generation。

**所有权**：`Top`/`Snapshot`/`Result.Changed`/`Coordinator.Decisions` 均返回新建切片，调用方无法别名内部状态；`Policy.ReplaceActors` 整体替换白名单 map，读写各自持锁。

**并发**：引擎用 `sync.RWMutex`（写 Apply、读 Top/Snapshot）；Policy 独立 `RWMutex`；Coordinator 用 `Mutex` 串行化“授权→引擎→记账”，保证审计序号连续无空洞。策略拒绝发生在引擎之前，不读取也不修改核心状态。

**复杂度**：结构校验 O(批次数)；Apply 额外 O(账户总数) 用于候选副本；Top/Snapshot O(n log n)；Authorize O(1)；Decisions O(决策数) 拷贝。

## 验证

```
go test ./...
go test -race ./...
go run ./cmd/demo
```
