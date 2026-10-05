# controlgraph178

并发安全的内存型“控制面依赖图”。仅依赖 Go 标准库（Go 1.22+）。语义详见 `SPEC.md`。

## 用法

```go
g, _ := controlgraph178.New(controlgraph178.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
r, _ := g.Apply(controlgraph178.Batch{Ops: []controlgraph178.Op{
    {Kind: controlgraph178.AddNode, From: "a"},
    {Kind: controlgraph178.AddNode, From: "b"},
    {Kind: controlgraph178.AddEdge, From: "a", To: "b"},
}})
ok, _ := g.Reachable("a", "b")
snap := g.Snapshot()
```

运行演示：`go run ./cmd/demo`。

## 设计说明

**索引**
- `nodes map[string]struct{}`：节点存在性 O(1)。
- `edges map[Edge]struct{}`：边存在性 O(1)，`Edge{From, To}` 为可比较键。
- `out map[string]map[string]struct{}`：出边邻接表，供环检测与 `Reachable` 的 DFS 使用；删除节点时扫描边集做级联删除。

**候选事务（candidate）**
`Apply` 分三个阶段：
1. 结构校验：整个批次在不读任何状态的情况下校验 kind、名称字符集（非空 ASCII 小写字母/数字/`-`/`_`）与字节上限、节点类操作不得携带 `To`；任何违规返回 `ErrInvalidInput`。
2. 在深拷贝出的候选状态上顺序应用操作：重复返回 `ErrExists`，缺失返回 `ErrNotFound`，加边前以“`To` 是否可达 `From`”检测有向环（`ErrCycle`）。
3. 仅在批次末检查最终节点/边容量（`ErrCapacity`）。任一阶段失败即丢弃候选状态，实现整体回滚；成功后一次性替换内部状态并将 `generation` 加一（空批次与失败批次不变）。

**所有权与并发**
- 所有公开方法并发安全：`Apply` 持写锁，`Reachable`/`Snapshot` 持读锁（`sync.RWMutex`），因此 `Reachable` 总是基于当前一致快照。
- `Snapshot` 返回新建的切片（节点按字典序、边按 `(From, To)` 稳定排序），调用方修改返回值不影响内部状态；候选拷贝在提交前为 `Apply` 私有，提交后归 `Graph` 独占。

**复杂度**（N 节点、E 边、K 批次操作数）
- `Apply`：结构校验 O(K·L)（L 为名称长度）；候选拷贝 O(N+E)；每加边一次 DFS O(N+E)；容量检查 O(1)。
- `Reachable`：O(N+E)。`Snapshot`：O(N log N + E log E)。
- `New`/`Apply`/`Reachable` 的名称校验均为 O(L)。
