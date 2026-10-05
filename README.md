# controlgraph138

并发安全的内存型“控制面依赖图 138”，Go 1.22+，仅标准库。实现见 `SPEC.md`，生产代码分为三个协同层。

## 三层架构

- `controlgraph138/trustgraph.go` — 状态引擎：原子批次事务、快照、可达性。
- `controlgraph138/policy.go` — 策略层：独立同步、可原子替换的 actor 白名单与单批操作数上限。
- `controlgraph138/coordinator.go` — 协调层：先授权再调用引擎，为成功/拒绝/引擎失败分配连续审计序号。

## 索引

状态引擎维护四类内存索引，全部由一把 `sync.RWMutex` 保护：

- `nodes map[string]struct{}` — 节点存在性，O(1) 查询。
- `edges map[Edge]struct{}` — 边集合，O(1) 去重与删除。
- `out map[string]map[string]struct{}` — 出边邻接表，用于环检测与 `Reachable` 的 BFS。
- `in map[string]map[string]struct{}` — 入边邻接表，使 `DeleteNode` 能 O(关联边数) 清理关联边。

## 候选事务

`Apply` 先对全部操作做纯结构校验（kind、名称字符集与字节上限、多余字段），不读取任何状态；随后在**候选副本**（clone 的 nodes/edges/out/in）上顺序执行操作。任一步失败（ErrExists/ErrNotFound/ErrCycle）或批次末容量检查（ErrCapacity）失败时直接丢弃候选，已提交状态零改动——天然回滚。成功则整体换入候选并将 `generation` 加一；空批次成功但不改变 generation。

## 所有权

- `Snapshot()` 返回新建并稳定排序（节点字典序；边按 From 再 To）的切片，调用方修改不影响内部状态。
- `Coordinator.Decisions()` 返回内部审计日志的独立副本，不别名内部存储。
- `Policy.ReplaceActors` 将白名单整体拷贝后原子换入，`Authorize` 只读当前快照。
- 策略拒绝发生在访问核心状态之前，不读取也不修改引擎。

## 复杂度

设批次数为 k，节点数 V，边数 E：

- `Apply`：结构校验 O(k·名称长)；候选克隆 O(V+E)；每个 AddEdge 的环检测为一次 BFS，O(V+E)；容量检查 O(1)。整体 O(V+E+k·(V+E))，写路径持写锁串行化。
- `Reachable`：BFS，O(V+E)，读锁。
- `Snapshot`：O(V log V + E log E)，读锁。
- `Authorize` / `ReplaceActors`：O(1) / O(白名单大小)。
- `Coordinator.Apply`：授权 + 一次引擎 `Apply` + O(1) 审计追加。

## 验证

```
go test ./...
go test -race ./...
go run ./cmd/demo
```
