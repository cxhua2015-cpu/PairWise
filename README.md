# controlgraph083

并发安全的内存型控制面依赖图（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

### 索引
- `nodes map[string]struct{}`：节点存在性 O(1)。
- `edges map[Edge]struct{}`：边存在性 O(1)。
- `out` / `in map[string]map[string]struct{}`：出/入邻接表，用于环检测、
  `Reachable` 的 DFS，以及 `DeleteNode` 时 O(度数) 级联删除关联边。

### 候选事务（candidate transaction）
`Apply` 先对整个批次做纯结构校验（kind、名称字符集与字节上限、多余字段），
不读取任何状态；随后在写锁内把四份索引浅拷贝为候选状态，逐条在候选上执行
操作。任一步失败（`ErrExists`/`ErrNotFound`/`ErrCycle`）直接丢弃候选，原状态
不受影响。节点/边容量只在批次末尾对候选检查，超限返回 `ErrCapacity` 并整体
回滚。全部成功才一次性替换内部状态并将 `generation` 加一（空批次不增加）。

### 所有权与并发
- 单个 `sync.RWMutex` 保护全部状态：`Apply` 取写锁，`Reachable`/`Snapshot`
  取读锁，读到的是同一 `generation` 下的一致快照。
- `Snapshot` 返回新建切片（节点按字典序、边按 `(From, To)` 稳定排序），
  调用方修改返回值不影响内部状态；批次与选项按值传入，无共享引用。

### 复杂度
- `Apply`：结构校验 O(Σ名称长度)；候选拷贝 O(N+E)；每条 `AddEdge` 的环检测
  为一次 DFS，O(N+E)；容量检查 O(1)。
- `Reachable`：O(N+E)。
- `Snapshot`：O(N log N + E log E)（排序）。
- 空间：O(N+E)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
