# servicegraph

并发安全的内存型服务依赖图（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计说明

**索引**
- `nodes map[string]struct{}`：节点存在性 O(1)。
- `edges map[Edge]struct{}`：边存在性 O(1)。
- `out` / `in map[string]map[string]struct{}`：出/入邻接表，用于可达性 DFS 与删节点时的级联删边。
所有索引在一次提交内同步维护，永不出现不一致中间态。

**候选事务（candidate transaction）**
`Apply` 先做整批结构校验（kind、名称字符集与字节上限、多余字段），不读取任何状态；
随后在写锁内把当前 `state` 深拷贝为候选副本，按序在副本上应用全部操作
（`ErrExists`/`ErrNotFound`/`ErrCycle` 任一失败即丢弃副本），最后才检查节点/边容量
（`ErrCapacity`）。全部成功才原子地换入副本并将 generation 加一；空批次不改变 generation。
因此失败批次对可见状态零影响，天然整体回滚。

**所有权与并发**
- 单把 `sync.RWMutex` 保护 `state` 指针与 generation；`Apply` 持写锁，
  `Reachable`/`Snapshot` 持读锁，读到的是同一提交的一致快照。
- 已提交的 `state` 不可变（只被替换、不被原地修改），读路径无需拷贝即可安全遍历。
- `Snapshot` 返回新建切片并对节点、边分别按字典序和 `(From, To)` 稳定排序，
  调用方修改返回切片不影响内部状态。

**复杂度**（N=节点数，E=边数，B=批内操作数）
- `Apply`：结构校验 O(B·L)（L 为名称长度）；候选拷贝 O(N+E)；
  每次 `AddEdge` 的成环检查为一次 DFS，O(N+E)；容量检查 O(1)。
- `Reachable`：一次 DFS，O(N+E)。
- `Snapshot`：O(N+E) 构造，排序 O(N log N + E log E)。
- 空间：O(N+E)。

## 验证

```
go test ./...
go test -race ./...
go run ./cmd/demo
```
