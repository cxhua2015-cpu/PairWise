# topologygraph238

并发安全的内存型控制拓扑图（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 架构

实现按职责拆分为四个联动文件，共享同一套结构与状态语义：

- `trustgraph.go` — 核心事务引擎：`New` / `Apply` / `Reachable` / `Snapshot`。
- `validation.go` — 无副作用的批次结构预检 `ValidateBatch`；`Apply` 复用同一套校验，保证“先完整结构校验、再读取状态”。
- `stats.go` — 线性一致的 `Stats`（在读锁内一次取齐 generation/节点数/边数）。
- `clone.go` — 保留逻辑时钟（generation）的深拷贝 `Clone`，与原图完全隔离所有权。

## 索引

- `nodes map[string]struct{}`：节点集合，O(1) 存在性判断。
- `edges map[Edge]struct{}`：边集合，O(1) 去重与删除。
- `out map[string]map[string]struct{}`：出边邻接表，用于环检测与可达性 DFS，以及删除节点时的级联清理。

## 候选事务（candidate transaction）

`Apply` 在写锁内先把 `nodes`/`edges`/`out` 复制为候选事务 `txn`，按序施加全部操作；任一操作失败或批次末容量（`MaxNodes`/`MaxEdges`）超限，直接丢弃候选，已提交状态零改动（整体回滚）。成功才一次性换入候选状态并将 generation 加一；空批次不改变 generation。因此“容量只在批次末检查”与“失败整体回滚”天然成立。

## 所有权

- 所有公开方法在 `sync.RWMutex` 保护下执行，写操作独占，读操作（`Reachable`/`Snapshot`/`Stats`/`Clone`/`ValidateBatch`）共享读锁，可并发调用。
- `Snapshot` 返回新建并排序的切片（节点按字典序、边按 `(From, To)` 稳定排序），调用方修改返回值不影响内部状态。
- `Clone` 逐键复制所有 map，克隆体与原图不共享任何可写内存，generation 各自独立演进。

## 复杂度

设 N 为节点数、E 为边数、B 为批次大小：

- `Apply`：结构校验 O(B·L)（L 为名称长度）；候选复制 O(N+E)；单条 AddEdge 的环检测为一次 DFS，O(N+E)；整体 O(N+E+B·(N+E))。
- `Reachable`：一次 DFS，O(N+E)。
- `Snapshot`：O(N log N + E log E)（排序主导）。
- `Stats`：O(1)。`Clone`：O(N+E)。`ValidateBatch`：O(B·L)，不读图状态。
