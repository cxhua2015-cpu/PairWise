# controlgraph153

并发安全的内存型“控制面依赖图”。原子批次支持 `AddNode` / `DeleteNode` /
`AddEdge` / `DeleteEdge`，加边阻止有向环，删除节点级联删除关联边，容量只在
批次末检查，失败整体回滚。仅依赖标准库，Go 1.22+。

## 索引结构

`Graph` 内部维护四份冗余索引，均以节点名为键：

- `nodes map[string]struct{}`：节点集合，O(1) 存在性判断。
- `out map[string]map[string]struct{}`：正向邻接表（from → to 集合），用于
  可达性遍历与环检测。
- `in map[string]map[string]struct{}`：反向邻接表（to → from 集合），使
  `DeleteNode` 能 O(关联边数) 级联删除入边，无需全图扫描。
- `edges int`：边计数，避免容量检查时遍历邻接表。

## 候选事务（candidate）

`Apply` 分两阶段：

1. **结构校验**：先对整个批次做纯结构校验（kind 合法、字段不多余、名称符合
   字符集与长度上限），不读取任何状态；任何失败返回 `ErrInvalidInput`。
2. **候选执行**：在写锁内把当前状态浅复制为 `candidate`（节点集 + 正/反邻接
   表 + 边计数），在候选上顺序应用全部操作；语义错误（`ErrExists` /
   `ErrNotFound` / `ErrCycle`）或批次末容量超限（`ErrCapacity`）时直接丢弃
   候选，整体回滚。全部成功才用候选替换正式状态，并将 `generation` 加一。
   空批次成功且不改变 generation。

环检测在候选的正向邻接表上做 DFS：新增 `from→to` 前检查 `to` 是否已可达
`from`（含自环 `from == to`）。

## 所有权与并发

- 所有公开方法可并发调用：`Apply` 取写锁，`Reachable` / `Snapshot` 取读锁。
- `Reachable` 在读锁保护的一致快照上直接遍历，不复制图。
- `Snapshot` 在读锁内构造结果，节点按字典序、边按 `(From, To)` 稳定排序；
  返回的切片为新分配的副本，调用方修改不影响内部状态。
- `Apply` 成功后内部 map 整体替换为候选副本，旧 map 不再被引用，不存在
  读写别名。

## 复杂度

设 N 为节点数、E 为边数、B 为批次操作数、D 为被删节点的关联边数：

- `Apply`：结构校验 O(B·名称长度)；候选复制 O(N+E)；每操作 O(1) 均摊，
  `AddEdge` 的环检测 O(N+E)，`DeleteNode` 级联 O(D)；容量检查 O(1)。
- `Reachable`：O(N+E) DFS。
- `Snapshot`：O(N+E) 构造，排序 O(N log N + E log E)。
- 空间：O(N+E)。

## 使用

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
