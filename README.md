# topologygraph313

并发安全的内存型有向控制拓扑图（Go 1.22+，仅标准库）。原子批次支持
`AddNode` / `DeleteNode` / `AddEdge` / `DeleteEdge`，加边阻止有向环，
删除节点级联删除关联边，容量在批次末统一检查，失败整体回滚。

## 索引

- `nodes map[string]struct{}`：节点存在性集合，O(1) 查找。
- `edges map[Edge]struct{}`：边集合，键为 `{From, To}`，O(1) 判重/删除。
- `adj map[string]map[string]struct{}`：出边邻接表，与 `edges` 同步维护，
  供环检测与 `Reachable` 做图遍历，无需每次从边集重建。

## 候选事务（candidate transaction）

`Apply` 分两阶段：

1. **结构校验**：在加锁前校验全部 op 的 kind、名称字符集与长度、多余字段，
   任何非法输入直接返回 `ErrInvalidInput`，不触碰状态。
2. **候选执行**：在写锁内把 `nodes` / `edges` / `adj` 深拷贝为候选副本，
   按序应用全部 op（存在性、环检测等错误立即返回，副本被丢弃即回滚）；
   最后统一检查 `MaxNodes` / `MaxEdges` 容量，超限返回 `ErrCapacity` 并丢弃副本。
   全部成功才用候选副本原子替换内部状态；非空成功批次 `generation` 恰好 +1，
   空批次与失败批次不变。

## 所有权与并发

- 单把 `sync.RWMutex` 保护全部状态：`Apply` 持写锁，`Reachable` / `Snapshot`
  持读锁，读者之间可并行，读写互斥，因此 `Reachable` 看到的始终是一致的当前快照。
- `Snapshot` 返回新建切片（节点按字典序、边按 `(From, To)` 稳定排序），
  调用方修改返回值不影响内部状态；`Apply` 完成后旧副本整体被替换，
  不存在与读者共享的可变内存。

## 复杂度

设批次含 k 个 op，图有 n 个节点、m 条边：

- `Apply`：结构校验 O(k·L)（L 为名称长度）；候选拷贝 O(n+m)；
  每个 `AddEdge` 的环检测为一次 DFS，O(n+m)；容量检查 O(1)。
- `Reachable`：一次 DFS，O(n+m)。
- `Snapshot`：收集 O(n+m)，排序 O(n log n + m log m)。
- `DeleteNode`：扫描边集级联删除，O(m)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
