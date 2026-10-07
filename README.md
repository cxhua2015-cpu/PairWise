# topologygraph328

并发安全的内存型有向控制拓扑图（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计

### 索引
图状态由四份冗余索引组成，全部随事务一起克隆与提交：
- `nodes map[string]struct{}`：节点存在性，O(1) 查询。
- `edges map[Edge]struct{}`：边存在性，O(1) 查询。
- `out map[string]map[string]struct{}`：出边邻接表，用于可达性 DFS 与删节点级联。
- `in  map[string]map[string]struct{}`：入边邻接表，使删节点时反向级联也是 O(关联边数)。

### 候选事务（candidate transaction）
`Apply` 先对整个批次做纯结构校验（kind 合法、节点操作无多余 `To`、名称字符集与字节上限），不读取任何状态；随后在写锁内把当前状态**整体克隆**为候选状态，在候选上顺序执行全部操作。任一步失败（`ErrExists`/`ErrNotFound`/`ErrCycle`）或批次末容量检查（`ErrCapacity`）失败时直接丢弃候选，已提交状态零改动，实现原子回滚。成功时原子地交换状态指针并将 `generation` 加一；空批次成功但 `generation` 不变。

### 所有权与并发
- 单把 `sync.RWMutex` 保护状态指针与 `generation`：`Apply` 取写锁，`Reachable`/`Snapshot` 取读锁，读者看到的是同一份不可变快照状态。
- 状态一旦提交就不再被原地修改（所有变更都发生在私有候选克隆上），因此读路径无需拷贝即可安全遍历。
- `Snapshot` 返回新建切片（节点按字典序、边按 `(From,To)` 稳定排序），调用方对返回切片的修改不影响内部状态。

### 复杂度（N=节点数，E=边数，B=批内操作数）
- `Apply`：克隆 O(N+E)；每条边操作的环检测为一次 DFS，O(N+E)；整体 O(B·(N+E))，空间 O(N+E)。
- `Reachable`：O(N+E) DFS，读锁下的一致快照。
- `Snapshot`：O(N+E) 收集 + O(N log N + E log E) 排序。
- `New`/`Apply` 的名称与结构校验：O(名称字节数)。

## 验证
```
go test ./...
go test -race ./...
go run ./cmd/demo
```
