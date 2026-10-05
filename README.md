# controlgraph123

并发安全的内存型控制面依赖图（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计说明

### 索引

图状态由四份互为冗余的索引组成，全部归属单个 `state` 结构：

- `nodes map[string]struct{}`：节点集合，O(1) 存在性判断。
- `edges map[Edge]struct{}`：边集合，O(1) 判重。
- `out map[string]map[string]struct{}`：正向邻接表，用于可达性 DFS 与删除节点时清理出边。
- `in  map[string]map[string]struct{}`：反向邻接表，用于删除节点时清理入边。

`DeleteNode` 借助 `out`/`in` 两个邻接表级联删除关联边，无需扫描全图。

### 候选事务（candidate transaction）

`Apply` 分三个阶段：

1. **结构校验**：在读取任何状态之前校验全部 op 的 kind、名称字符集（非空 ASCII 小写字母/数字/`-`/`_`）、字节上限及多余字段，失败返回 `ErrInvalidInput`。
2. **候选执行**：把当前 `state` 深拷贝为候选副本，在副本上顺序应用所有 op（存在性、未找到、环检测均针对候选状态）。`AddEdge` 通过在候选图上做 `To → From` 可达性搜索阻止有向环，自环直接判 `ErrCycle`。
3. **提交**：仅在批次末尾检查最终节点/边容量（`ErrCapacity`）。任何失败都直接丢弃候选副本，原状态零改动，实现整体回滚；成功则原子换入候选副本，`generation` 恰好加一（空批次不变）。

### 所有权与并发

- `Graph` 内嵌 `sync.RWMutex`：`Apply` 取写锁，`Reachable`/`Snapshot` 取读锁，所有公开方法可并发调用。
- `state` 及其所有 map 绝不共享给调用方；`Snapshot` 返回新建切片（节点按字典序、边按 `(From, To)` 稳定排序），调用方修改返回值不影响内部状态。
- 不可配置项（容量上限）在 `New` 时校验（必须为正，否则 `ErrInvalidOptions`）后只读。

### 复杂度

设 N 为节点数、E 为边数、K 为批次内 op 数：

- `Apply`：候选拷贝 O(N + E)；每个 `AddEdge` 的环检测为一次 DFS，O(N + E)；总复杂度 O(K·(N + E))，空间 O(N + E)。
- `Reachable`：一次 DFS，O(N + E)。
- `Snapshot`：O(N log N + E log E)（排序）。
- `AddNode`/`DeleteNode`/`DeleteEdge`：均摊 O(1)（`DeleteNode` 另加其关联边数）。

## 使用

```sh
go test ./...
go run ./cmd/demo
```
