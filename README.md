# topologygraph258

并发安全的内存型控制拓扑图（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 架构

实现刻意拆分为四个相互联动的文件：

- `trustgraph.go` — 核心事务引擎：`Graph`、`New`、`Apply`、`Reachable`、`Snapshot`。
- `validation.go` — 无副作用的批次结构预检 `ValidateBatch`，与 `Apply` 共享同一套结构语义（`validateOp` / `validName`）。
- `stats.go` — 线性一致的 `Stats` 状态统计。
- `clone.go` — 保留逻辑时钟（generation）且所有权完全隔离的深拷贝 `Clone`。

## 索引

- 节点：`map[string]struct{}`，O(1) 存在性判断。
- 边：`map[Edge]struct{}`（`Edge{From, To}` 为可比较键），O(1) 查重与删除。
- 逻辑时钟：`generation uint64`，仅非空成功批次递增一次。

## 候选事务

`Apply` 先在读锁外完成纯结构预检（不读状态），再持写锁把当前 `nodes`/`edges`
复制为候选副本，按序重放全部 op（存在性、缺失、有向环检查都在候选上进行）。
只有在全部 op 成功且批次末容量（`MaxNodes`/`MaxEdges`）达标时才整体提交：
指针交换候选副本并递增 generation。任何失败直接丢弃候选，原状态与
generation 完全不变，实现原子回滚。删除节点会在候选上级联删除关联边。

## 所有权

- 所有公开方法均可并发调用；状态由 `sync.RWMutex` 保护，写操作独占，读操作
  （`Reachable`/`Snapshot`/`Stats`/`Clone`/`ValidateBatch`）共享读锁。
- `Snapshot` 返回新建切片并对节点、边按字典序稳定排序；`Clone` 逐键复制两个
  map。返回值与内部状态零共享，调用方修改不会影响图。
- `Clone` 复制 generation，克隆体后续 `Apply` 不影响原图，反之亦然。

## 复杂度

设 N 为节点数、E 为边数、K 为批次 op 数：

- `Apply`：O(N + E) 候选复制 + 每个 AddEdge O(E) 的可达性检查，整体 O(N + E + K·E)。
- `Reachable`：BFS，O(N + E)。
- `Snapshot`：排序，O(N log N + E log E)。
- `Stats`：O(1)；`Clone`：O(N + E)；`ValidateBatch`：O(K·L)，L 为名称长度。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
