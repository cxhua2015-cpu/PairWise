# controlgraph138

并发安全的内存型控制面依赖图（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 架构

三个生产文件协同工作：

- `controlgraph138/trustgraph.go` — 状态引擎：原子批次、事务数据与快照。
- `controlgraph138/policy.go` — 策略层：独立同步、可原子替换的 actor 白名单与单批操作数上限。
- `controlgraph138/coordinator.go` — 协调层：先授权再调用引擎，并为成功、拒绝、引擎失败分配连续审计序号。

## 索引

引擎维护三类索引：`nodes`（节点集合）、`edges`（`Edge{From,To}` 集合）以及 `out`（出边邻接表 `map[string]map[string]struct{}`）。环检测与 `Reachable` 只遍历 `out`，删除节点时借助 `edges` 全量扫描清除关联边并同步修剪 `out`。

## 候选事务

`Apply` 先做整批结构校验（未知 kind、非法名称、多余字段返回 `ErrInvalidInput`），再在写锁内把 `nodes`/`edges`/`out` 深拷贝为候选状态，顺序应用全部操作。任何一步失败（`ErrExists`/`ErrNotFound`/`ErrCycle`）或批次末容量检查失败（`ErrCapacity`）都直接丢弃候选，原状态零改动；成功时一次性换入候选并将 `generation` 加一。空批次不改变 generation。

## 所有权

所有公开方法返回的数据（`Snapshot`、`Decisions`）都是新分配的切片，不与内部存储共享底层数组；调用方修改返回值不影响图或审计日志。策略层 `ReplaceActors` 整体替换白名单 map，旧 map 不再被引用。

## 并发与复杂度

- 引擎：`sync.RWMutex`；`Apply` 持写锁，`Reachable`/`Snapshot` 持读锁。策略与协调层各自独立加锁，三层均可并发调用。
- 结构校验 O(ops · 名称长度)；候选拷贝 O(N+E)；`AddEdge` 环检测为一次 DFS，O(N+E)；`DeleteNode` 关联边清理 O(E)；`Snapshot` 排序 O(N log N + E log E)；`Reachable` O(N+E)。
- 协调层 `Apply` 串行化审计记录，序号为单调递增的连续整数。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
