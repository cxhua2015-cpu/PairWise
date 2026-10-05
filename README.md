# controlgraph193

Read `SPEC.md` and implement the package.

## 实现说明

### 索引结构
`Graph` 内部维护四份冗余索引，均以哈希表实现：
- `nodes map[string]struct{}`：节点集合，O(1) 存在性判断。
- `edges map[Edge]struct{}`：边集合，O(1) 判重与删除定位。
- `out map[string]map[string]struct{}`：出边邻接表，用于可达性遍历与环检测。
- `in map[string]map[string]struct{}`：入边邻接表，使 `DeleteNode` 能 O(关联边数) 级联删除入边，无需全图扫描。

### 候选事务（原子批次）
`Apply` 先在持锁状态下对整个批次做纯结构校验（kind 合法、字段无多余、名称合法），不读取图状态；随后把当前图**深拷贝**为候选副本，在副本上顺序应用全部操作（语义错误如 `ErrExists`/`ErrNotFound`/`ErrCycle` 直接返回），最后才在副本上检查节点/边容量。任一失败即丢弃副本，原图与 generation 完全不变（整体回滚）；全部成功则一次性用候选副本替换内部状态并将 generation 加一。空批次为成功空操作，不增加 generation。

### 所有权与并发
所有公开方法并发安全：`Apply` 持 `sync.Mutex` 写锁，`Reachable`/`Snapshot` 持 `RWMutex` 读锁，因此 `Reachable` 总是基于某一一致快照。内部 map 绝不暴露给调用方：`Snapshot` 返回新建且稳定排序（节点按字典序、边按 `(From, To)` 字典序）的切片，调用方修改返回值不影响内部状态。候选副本在 `Apply` 返回前为局部所有，不与共享状态混叠。

### 复杂度
设 N 为节点数、E 为边数、B 为批次操作数：
- `New`：O(1)。
- `Apply`：结构校验 O(B)；候选拷贝 O(N+E)；每条 `AddEdge` 的环检测为一次 DFS，O(N+E)；容量检查 O(1)。整体 O(N+E+B·(N+E))，失败零副作用。
- `Reachable`：一次 DFS，O(N+E)。
- `Snapshot`：收集 O(N+E)，排序 O(N log N + E log E)。
- 空间：O(N+E)。
